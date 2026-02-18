package claude

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"godex/pkg/harness"
)

// testClientWrapper is a mock replacement for ClientWrapper that replays
// scripted Anthropic events without making API calls.
type testClientWrapper struct {
	events []anthropic.MessageStreamEventUnion
	models []harness.ModelInfo
}

func newTestClient(events ...string) *testClientWrapper {
	tc := &testClientWrapper{
		models: []harness.ModelInfo{{ID: "test-model", Provider: "claude"}},
	}
	for _, e := range events {
		var ev anthropic.MessageStreamEventUnion
		json.Unmarshal([]byte(e), &ev)
		tc.events = append(tc.events, ev)
	}
	return tc
}

func (tc *testClientWrapper) StreamMessages(_ context.Context, _ anthropic.MessageNewParams, onEvent func(anthropic.MessageStreamEventUnion) error) error {
	for _, ev := range tc.events {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

func (tc *testClientWrapper) ListModels(_ context.Context) ([]harness.ModelInfo, error) {
	return tc.models, nil
}

// streamClient is the interface that Harness.client needs to satisfy.
// We use this to verify our test mock matches the real client.
type streamClient interface {
	StreamMessages(ctx context.Context, params anthropic.MessageNewParams, onEvent func(anthropic.MessageStreamEventUnion) error) error
	ListModels(ctx context.Context) ([]harness.ModelInfo, error)
}

var _ streamClient = (*ClientWrapper)(nil)
var _ streamClient = (*testClientWrapper)(nil)

// newTestHarness creates a harness backed by scripted events.
func newTestHarness(events ...string) *Harness {
	tc := newTestClient(events...)
	return &Harness{
		client:       (*ClientWrapper)(nil), // won't be used
		defaultModel: "test-model",
		maxTokens:    4096,
		testClient:   tc,
	}
}

func TestStreamTurn_TextResponse(t *testing.T) {
	h := newTestHarness(
		`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":50,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world!"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
		`{"type":"message_stop"}`,
	)

	var events []harness.Event
	err := h.StreamTurn(context.Background(), &harness.Turn{Messages: []harness.Message{{Role: "user", Content: "hi"}}}, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// text("Hello ") + text("world!") + usage + done = 4 events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Text.Delta != "Hello " {
		t.Errorf("unexpected first delta: %q", events[0].Text.Delta)
	}
	if events[1].Text.Delta != "world!" {
		t.Errorf("unexpected second delta: %q", events[1].Text.Delta)
	}
	if events[2].Kind != harness.EventUsage {
		t.Errorf("expected usage, got %s", events[2].Kind)
	}
	if events[3].Kind != harness.EventDone {
		t.Errorf("expected done, got %s", events[3].Kind)
	}
}

func TestStreamAndCollect_TextResponse(t *testing.T) {
	h := newTestHarness(
		`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":50,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello!"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
		`{"type":"message_stop"}`,
	)

	result, err := h.StreamAndCollect(context.Background(), &harness.Turn{Messages: []harness.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", result.FinalText)
	}
	if result.Usage == nil || result.Usage.InputTokens != 50 {
		t.Errorf("unexpected usage: %+v", result.Usage)
	}
}

func TestStreamTurn_ThinkingAndText(t *testing.T) {
	h := newTestHarness(
		`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":100,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me analyze..."}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is 42."}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`,
		`{"type":"message_stop"}`,
	)

	var events []harness.Event
	err := h.StreamTurn(context.Background(), &harness.Turn{Messages: []harness.Message{{Role: "user", Content: "think"}}}, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	kinds := make([]harness.EventKind, len(events))
	for i, e := range events {
		kinds[i] = e.Kind
	}

	expected := []harness.EventKind{
		harness.EventThinking,
		harness.EventText,
		harness.EventUsage,
		harness.EventDone,
	}
	if len(kinds) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(kinds), kinds)
	}
	for i, k := range kinds {
		if k != expected[i] {
			t.Errorf("event %d: expected %s, got %s", i, expected[i], k)
		}
	}
}

func TestStreamTurn_ToolUse(t *testing.T) {
	h := newTestHarness(
		`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":80,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"shell","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30}}`,
		`{"type":"message_stop"}`,
	)

	result, err := h.StreamAndCollect(context.Background(), &harness.Turn{Messages: []harness.Message{{Role: "user", Content: "list files"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Name != "shell" {
		t.Errorf("expected 'shell', got %q", tc.Name)
	}
	if tc.Arguments != `{"command":"ls"}` {
		t.Errorf("unexpected args: %q", tc.Arguments)
	}
}

// multiTurnTestClient supports multiple rounds of events for tool loop testing.
type multiTurnTestClient struct {
	rounds [][]anthropic.MessageStreamEventUnion
	call   int
}

func (tc *multiTurnTestClient) StreamMessages(_ context.Context, _ anthropic.MessageNewParams, onEvent func(anthropic.MessageStreamEventUnion) error) error {
	if tc.call >= len(tc.rounds) {
		return nil
	}
	events := tc.rounds[tc.call]
	tc.call++
	for _, ev := range events {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

func (tc *multiTurnTestClient) ListModels(_ context.Context) ([]harness.ModelInfo, error) {
	return nil, nil
}

func parseEvents(t *testing.T, jsons ...string) []anthropic.MessageStreamEventUnion {
	t.Helper()
	var events []anthropic.MessageStreamEventUnion
	for _, j := range jsons {
		var ev anthropic.MessageStreamEventUnion
		if err := json.Unmarshal([]byte(j), &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

func TestRunToolLoop_Integration(t *testing.T) {
	tc := &multiTurnTestClient{
		rounds: [][]anthropic.MessageStreamEventUnion{
			// Round 1: tool call
			parseEvents(t,
				`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":80,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01","name":"shell","input":{}}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":20}}`,
				`{"type":"message_stop"}`,
			),
			// Round 2: text response
			parseEvents(t,
				`{"type":"message_start","message":{"id":"msg_02","type":"message","role":"assistant","content":[],"model":"test","usage":{"input_tokens":150,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Found 2 files."}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
				`{"type":"message_stop"}`,
			),
		},
	}

	h := &Harness{
		defaultModel: "test",
		maxTokens:    4096,
		testClient:   tc,
	}

	handler := &testToolHandler{
		result: &harness.ToolResultEvent{
			CallID: "toolu_01",
			Output: "file1.go\nfile2.go",
		},
	}

	var eventLog []harness.EventKind
	result, err := h.RunToolLoop(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "list files"}},
	}, handler, harness.LoopOptions{
		MaxTurns: 5,
		OnEvent: func(ev harness.Event) error {
			eventLog = append(eventLog, ev.Kind)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Found 2 files." {
		t.Errorf("expected 'Found 2 files.', got %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.Usage == nil {
		t.Error("expected usage")
	}
}

func TestRunToolLoop_MockBased(t *testing.T) {
	mock := NewMock(WithToolUseFlow("shell", `{"command":"ls"}`, "Found files."))

	handler := &testToolHandler{
		result: &harness.ToolResultEvent{
			CallID: "toolu_01",
			Output: "file1.go\nfile2.go",
		},
	}

	result, err := mock.RunToolLoop(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "list"}},
	}, handler, harness.LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Found files." {
		t.Errorf("expected 'Found files.', got %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

type testToolHandler struct {
	result *harness.ToolResultEvent
}

func (h *testToolHandler) Handle(_ context.Context, call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
	r := *h.result
	r.CallID = call.CallID
	return &r, nil
}

func (h *testToolHandler) Available() []harness.ToolSpec {
	return []harness.ToolSpec{{Name: "shell", Description: "Run shell command"}}
}

func TestListModels(t *testing.T) {
	mock := NewMock()
	models, err := mock.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0].Provider != "claude" {
		t.Errorf("expected provider 'claude', got %q", models[0].Provider)
	}
}
