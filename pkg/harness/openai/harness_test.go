package openai

import (
	"context"
	"fmt"
	"testing"

	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func TestNew_Defaults(t *testing.T) {
	h := New(Config{})
	if h.Name() != "openai" {
		t.Errorf("expected 'openai', got %q", h.Name())
	}
	if h.defaultModel != "gpt-4o" {
		t.Errorf("expected default model gpt-4o, got %q", h.defaultModel)
	}
}

func TestNew_CustomModel(t *testing.T) {
	h := New(Config{DefaultModel: "gemini-pro"})
	if h.defaultModel != "gemini-pro" {
		t.Errorf("expected gemini-pro, got %q", h.defaultModel)
	}
}

func TestStreamTurn_NoClient(t *testing.T) {
	h := New(Config{})
	err := h.StreamTurn(context.Background(), &harness.Turn{}, func(harness.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error with no client")
	}
}

func TestListModels_NoClient(t *testing.T) {
	h := New(Config{})
	models, err := h.ListModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected no models, got %d", len(models))
	}
}

// mockStreamClient implements streamClient for testing.
type mockStreamClient struct {
	events []protocol.StreamEvent
	models []harness.ModelInfo
	err    error
}

func (m *mockStreamClient) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	if m.err != nil {
		return m.err
	}
	for _, ev := range m.events {
		if err := onEvent(sse.Event{Value: ev}); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockStreamClient) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	return m.models, nil
}

func TestStreamTurn_TextDelta(t *testing.T) {
	h := &Harness{
		client: &mockStreamClient{
			events: []protocol.StreamEvent{
				{Type: "response.output_text.delta", Delta: "Hello "},
				{Type: "response.output_text.delta", Delta: "world"},
				{Type: "response.completed", Response: &protocol.ResponseRef{
					Usage: &protocol.Usage{InputTokens: 10, OutputTokens: 5},
				}},
			},
		},
		defaultModel: "gpt-4o",
	}

	var events []harness.Event
	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2 text deltas + 1 usage + 1 done
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventText || events[0].Text.Delta != "Hello " {
		t.Errorf("unexpected first event: %+v", events[0])
	}
	if events[1].Kind != harness.EventText || events[1].Text.Delta != "world" {
		t.Errorf("unexpected second event: %+v", events[1])
	}
	if events[2].Kind != harness.EventUsage {
		t.Errorf("expected usage, got %s", events[2].Kind)
	}
	if events[3].Kind != harness.EventDone {
		t.Errorf("expected done, got %s", events[3].Kind)
	}
}

func TestStreamTurn_ToolCall(t *testing.T) {
	h := &Harness{
		client: &mockStreamClient{
			events: []protocol.StreamEvent{
				{
					Type: "response.output_item.done",
					Item: &protocol.OutputItem{
						Type:      "function_call",
						CallID:    "call_123",
						Name:      "shell",
						Arguments: `{"command":"ls"}`,
					},
				},
			},
		},
		defaultModel: "gpt-4o",
	}

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

	// tool_call + done
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventToolCall {
		t.Errorf("expected tool_call, got %s", events[0].Kind)
	}
	if events[0].ToolCall.Name != "shell" {
		t.Errorf("expected 'shell', got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.CallID != "call_123" {
		t.Errorf("unexpected call ID: %s", events[0].ToolCall.CallID)
	}
}

func TestStreamTurn_Error(t *testing.T) {
	h := &Harness{
		client: &mockStreamClient{
			events: []protocol.StreamEvent{
				{Type: "error", Message: "rate limit exceeded"},
			},
		},
		defaultModel: "gpt-4o",
	}

	var events []harness.Event
	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventError {
		t.Errorf("expected error, got %s", events[0].Kind)
	}
	if events[0].Error.Message != "rate limit exceeded" {
		t.Errorf("unexpected error msg: %s", events[0].Error.Message)
	}
}

func TestStreamTurn_StreamError(t *testing.T) {
	h := &Harness{
		client: &mockStreamClient{
			err: fmt.Errorf("connection refused"),
		},
		defaultModel: "gpt-4o",
	}

	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}, func(ev harness.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamAndCollect(t *testing.T) {
	h := &Harness{
		client: &mockStreamClient{
			events: []protocol.StreamEvent{
				{Type: "response.output_text.delta", Delta: "Hello"},
				{Type: "response.completed", Response: &protocol.ResponseRef{
					Usage: &protocol.Usage{InputTokens: 100, OutputTokens: 20},
				}},
			},
		},
		defaultModel: "gpt-4o",
	}

	result, err := h.StreamAndCollect(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Hello" {
		t.Errorf("expected 'Hello', got %q", result.FinalText)
	}
	if result.Usage == nil {
		t.Fatal("expected usage")
	}
	if result.Usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", result.Usage.InputTokens)
	}
}

func TestBuildRequest_Basic(t *testing.T) {
	h := New(Config{DefaultModel: "gpt-4o"})
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gpt-4o" {
		t.Errorf("unexpected model: %s", req.Model)
	}
	if req.Instructions == "" {
		t.Error("expected non-empty instructions")
	}
	if len(req.Input) != 1 {
		t.Fatalf("expected 1 input, got %d", len(req.Input))
	}
	if !req.Stream {
		t.Error("expected stream=true")
	}
}

func TestBuildRequest_ModelOverride(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{Model: "gemini-2.0-flash"}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "gemini-2.0-flash" {
		t.Errorf("expected gemini-2.0-flash, got %s", req.Model)
	}
}

func TestBuildRequest_WithTools(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "do it"}},
		Tools: []harness.ToolSpec{
			{
				Name:        "shell",
				Description: "Run a shell command",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(req.Tools))
	}
	if req.Tools[0].Name != "shell" {
		t.Errorf("expected 'shell', got %q", req.Tools[0].Name)
	}
	if req.ToolChoice != "auto" {
		t.Errorf("expected tool_choice=auto, got %q", req.ToolChoice)
	}
}

func TestBuildRequest_NoToolsNoToolChoice(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if req.ToolChoice != "" {
		t.Errorf("expected empty tool_choice, got %q", req.ToolChoice)
	}
}

func TestBuildRequest_MessageTypes(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "do it"},
			{Role: "assistant", Content: `{"command":"ls"}`, Name: "shell", ToolID: "call_01"},
			{Role: "tool", Content: "file1.go", ToolID: "call_01"},
			{Role: "assistant", Content: "Done!"},
		},
	}
	req, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Input) != 4 {
		t.Fatalf("expected 4 inputs, got %d", len(req.Input))
	}
}

func TestListModels(t *testing.T) {
	h := &Harness{
		client: &mockStreamClient{
			models: []harness.ModelInfo{
				{ID: "gpt-4o", Provider: "openai"},
			},
		},
		defaultModel: "gpt-4o",
	}
	models, err := h.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" {
		t.Errorf("unexpected model: %s", models[0].ID)
	}
}

func TestRunToolLoop(t *testing.T) {
	callCount := 0
	h := &Harness{
		client: &mockStreamClient{
			events: []protocol.StreamEvent{
				{Type: "response.output_text.delta", Delta: "All done."},
			},
		},
		defaultModel: "gpt-4o",
	}

	handler := &testToolHandler{
		fn: func(call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
			callCount++
			return &harness.ToolResultEvent{CallID: call.CallID, Output: "ok"}, nil
		},
	}

	result, err := h.RunToolLoop(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}, handler, harness.LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "All done." {
		t.Errorf("unexpected final text: %q", result.FinalText)
	}
	// No tool calls in the stream, so handler shouldn't be called
	if callCount != 0 {
		t.Errorf("expected 0 tool calls, got %d", callCount)
	}
}

func TestRunToolLoop_WithToolCall(t *testing.T) {
	// We need a client that returns different events on successive calls
	client := &multiTurnClient{
		turns: [][]protocol.StreamEvent{
			// Turn 1: tool call
			{
				{
					Type: "response.output_item.done",
					Item: &protocol.OutputItem{
						Type:      "function_call",
						CallID:    "call_01",
						Name:      "shell",
						Arguments: `{"command":"ls"}`,
					},
				},
			},
			// Turn 2: text response
			{
				{Type: "response.output_text.delta", Delta: "Found files."},
				{Type: "response.completed", Response: &protocol.ResponseRef{
					Usage: &protocol.Usage{InputTokens: 200, OutputTokens: 30},
				}},
			},
		},
	}

	h := &Harness{client: client, defaultModel: "gpt-4o"}

	handler := &testToolHandler{
		fn: func(call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
			return &harness.ToolResultEvent{CallID: call.CallID, Output: "file1.go\nfile2.go"}, nil
		},
	}

	result, err := h.RunToolLoop(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "list files"}},
	}, handler, harness.LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Found files." {
		t.Errorf("unexpected final text: %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].Name != "shell" {
		t.Errorf("unexpected tool name: %s", result.ToolCalls[0].Name)
	}
}

// Test helpers

type testToolHandler struct {
	fn func(harness.ToolCallEvent) (*harness.ToolResultEvent, error)
}

func (h *testToolHandler) Handle(_ context.Context, call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
	return h.fn(call)
}

func (h *testToolHandler) Available() []harness.ToolSpec { return nil }

// multiTurnClient returns different events per call.
type multiTurnClient struct {
	turnIndex int
	turns     [][]protocol.StreamEvent
}

func (m *multiTurnClient) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	if m.turnIndex >= len(m.turns) {
		return fmt.Errorf("no more turns")
	}
	events := m.turns[m.turnIndex]
	m.turnIndex++
	for _, ev := range events {
		if err := onEvent(sse.Event{Value: ev}); err != nil {
			return err
		}
	}
	return nil
}

func (m *multiTurnClient) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	return nil, nil
}
