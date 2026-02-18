package codex

import (
	"testing"

	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func TestTranslateEvent_TextDelta(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: "hello"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventText {
		t.Fatalf("expected text event, got %v", events)
	}
	if events[0].Text.Delta != "hello" {
		t.Errorf("expected 'hello', got %q", events[0].Text.Delta)
	}
}

func TestTranslateEvent_EmptyDelta(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: ""}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events for empty delta")
	}
}

func TestTranslateEvent_FunctionCallDone(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{
		Type: "response.output_item.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_123",
			Name:      "shell",
			Arguments: `{"command":["ls"]}`,
		},
	}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventToolCall {
		t.Fatalf("expected tool_call, got %v", events)
	}
	if events[0].ToolCall.Name != "shell" {
		t.Errorf("expected 'shell', got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.CallID != "call_123" {
		t.Errorf("expected call_123, got %q", events[0].ToolCall.CallID)
	}
}

func TestTranslateEvent_UpdatePlanDone(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	args := `{"steps":[{"title":"Do thing","status":"pending"}]}`
	ev := protocol.StreamEvent{
		Type: "response.output_item.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_plan",
			Name:      "update_plan",
			Arguments: args,
		},
	}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventPlanUpdate {
		t.Fatalf("expected plan_update, got %v", events)
	}
	if events[0].Plan.Title != "Do thing" {
		t.Errorf("unexpected title: %q", events[0].Plan.Title)
	}
}

func TestTranslateEvent_ResponseDone(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{
		Type: "response.completed",
		Response: &protocol.ResponseRef{
			Usage: &protocol.Usage{InputTokens: 100, OutputTokens: 50},
		},
	}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventUsage {
		t.Fatalf("expected usage, got %v", events)
	}
	if events[0].Usage.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", events[0].Usage.InputTokens)
	}
}

func TestTranslateEvent_Error(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "error", Message: "rate limited"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventError {
		t.Fatalf("expected error event, got %v", events)
	}
	if events[0].Error.Message != "rate limited" {
		t.Errorf("unexpected message: %q", events[0].Error.Message)
	}
}

func TestTranslateEvent_ErrorEmpty(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "error"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Error.Message != "unknown error" {
		t.Error("expected 'unknown error' fallback")
	}
}

func TestTranslateEvent_UnknownType(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.some_random_thing"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events for unknown type")
	}
}

func TestTranslateEvent_OutputItemAdded(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{
		Type: "response.output_item.added",
		Item: &protocol.OutputItem{Type: "function_call", CallID: "c1", Name: "shell"},
	}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should not emit an event yet (waits for done)
	if len(events) != 0 {
		t.Error("expected no events on output_item.added")
	}
}

func TestTranslateEvent_OutputTextDone(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.output_text.done"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events on text done")
	}
}

func TestNew(t *testing.T) {
	h := New(Config{DefaultModel: "o3"})
	if h.Name() != "codex" {
		t.Errorf("expected 'codex', got %q", h.Name())
	}
	if h.defaultModel != "o3" {
		t.Errorf("expected 'o3', got %q", h.defaultModel)
	}
}

func TestNew_DefaultModel(t *testing.T) {
	h := New(Config{})
	if h.defaultModel != "gpt-5.2-codex" {
		t.Errorf("expected default model, got %q", h.defaultModel)
	}
}

func TestDefaultHarnessTools(t *testing.T) {
	tools := DefaultHarnessTools()
	if len(tools) != 3 {
		t.Fatalf("expected 3, got %d", len(tools))
	}
}

func TestNewClientWrapper(t *testing.T) {
	w := NewClientWrapper(nil)
	if w == nil {
		t.Fatal("expected non-nil wrapper")
	}
}
