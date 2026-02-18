package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"godex/pkg/auth"
	backendCodex "godex/pkg/backend/codex"
	"godex/pkg/harness"
)

func newTestHarness(handler http.HandlerFunc) (*Harness, *httptest.Server) {
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}
	}
	server := httptest.NewServer(handler)

	// Create a temp auth file with an API key
	tmpDir, _ := os.MkdirTemp("", "codex-test")
	authPath := filepath.Join(tmpDir, "auth.json")
	authData, _ := json.Marshal(auth.File{
		AuthMode: auth.ModeAPIKey,
		APIKey:   "test-key",
	})
	os.WriteFile(authPath, authData, 0o644)
	store, _ := auth.Load(authPath)

	client := backendCodex.New(nil, store, backendCodex.Config{
		BaseURL: server.URL,
	})
	h := New(Config{Client: client, DefaultModel: "test-model"})
	return h, server
}

func sseResponse(events ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range events {
			fmt.Fprintf(w, "data: %s\n\n", ev)
		}
	}
}

func TestStreamTurn_Integration(t *testing.T) {
	h, server := newTestHarness(sseResponse(
		`{"type":"response.output_text.delta","delta":"Hello "}`,
		`{"type":"response.output_text.delta","delta":"world"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
	))
	defer server.Close()

	turn := &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}

	var events []harness.Event
	err := h.StreamTurn(context.Background(), turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// text + text + usage + done
	kinds := make([]harness.EventKind, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(events), kinds)
	}
	if events[0].Kind != harness.EventText || events[0].Text.Delta != "Hello " {
		t.Errorf("first event: %v", events[0])
	}
}

func TestStreamAndCollect_Integration(t *testing.T) {
	h, server := newTestHarness(sseResponse(
		`{"type":"response.output_text.delta","delta":"result"}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":20,"output_tokens":10}}}`,
	))
	defer server.Close()

	result, err := h.StreamAndCollect(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "result" {
		t.Errorf("expected 'result', got %q", result.FinalText)
	}
	if result.Usage == nil || result.Usage.InputTokens != 20 {
		t.Error("expected usage info")
	}
}

func TestListModels_Integration(t *testing.T) {
	h, server := newTestHarness(nil)
	defer server.Close()

	models, err := h.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Falls back to static list
	if len(models) == 0 {
		t.Error("expected some models")
	}
	if models[0].Provider != "codex" {
		t.Errorf("expected provider 'codex', got %q", models[0].Provider)
	}
}

func TestStreamTurn_ToolCall(t *testing.T) {
	h, server := newTestHarness(sseResponse(
		`{"type":"response.output_item.added","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"shell"}}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"{\"command\":"}`,
		`{"type":"response.function_call_arguments.delta","item_id":"item_1","delta":"[\"ls\"]}"}`,
		`{"type":"response.output_item.done","item":{"id":"item_1","type":"function_call","call_id":"call_1","name":"shell","arguments":"{\"command\":[\"ls\"]}"}}`,
		`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
	))
	defer server.Close()

	var events []harness.Event
	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "list files"}},
	}, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var toolCalls int
	for _, ev := range events {
		if ev.Kind == harness.EventToolCall {
			toolCalls++
			if ev.ToolCall.Name != "shell" {
				t.Errorf("expected 'shell', got %q", ev.ToolCall.Name)
			}
		}
	}
	if toolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCalls)
	}
}

func TestStreamTurn_PlanUpdate(t *testing.T) {
	args := `{"steps":[{"title":"Read","status":"completed"},{"title":"Write","status":"in_progress"}]}`
	h, server := newTestHarness(sseResponse(
		fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_plan","name":"update_plan","arguments":%q}}`, args),
		`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`,
	))
	defer server.Close()

	var planEvents int
	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "plan it"}},
	}, func(ev harness.Event) error {
		if ev.Kind == harness.EventPlanUpdate {
			planEvents++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if planEvents != 2 {
		t.Errorf("expected 2 plan events, got %d", planEvents)
	}
}

func TestRunToolLoop_Integration(t *testing.T) {
	callCount := 0
	h, server := newTestHarness(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount == 1 {
			// First call: emit a tool call
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.output_item.done","item":{"type":"function_call","call_id":"c1","name":"shell","arguments":"{\"command\":[\"echo\",\"hi\"]}"}}`)
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":5}}}`)
		} else {
			// Second call: emit text
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.output_text.delta","delta":"done"}`)
			fmt.Fprintf(w, "data: %s\n\n", `{"type":"response.completed","response":{"usage":{"input_tokens":15,"output_tokens":3}}}`)
		}
	})
	defer server.Close()

	handler := &testToolHandler{output: "hi\n"}
	result, err := h.RunToolLoop(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "say hi"}},
	}, handler, harness.LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "done" {
		t.Errorf("expected 'done', got %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

type testToolHandler struct {
	output string
}

func (h *testToolHandler) Handle(_ context.Context, call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
	return &harness.ToolResultEvent{CallID: call.CallID, Output: h.output}, nil
}

func (h *testToolHandler) Available() []harness.ToolSpec { return nil }

func TestStreamTurn_Error(t *testing.T) {
	h, server := newTestHarness(sseResponse(
		`{"type":"error","message":"rate limit exceeded"}`,
	))
	defer server.Close()

	var errorEvents int
	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "test"}},
	}, func(ev harness.Event) error {
		if ev.Kind == harness.EventError {
			errorEvents++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if errorEvents != 1 {
		t.Errorf("expected 1 error event, got %d", errorEvents)
	}
}
