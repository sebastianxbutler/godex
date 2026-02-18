package openai

import (
	"testing"

	"godex/pkg/harness"
	"godex/pkg/protocol"
)

func TestTranslateEvent_TextDelta(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: "hello"}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventText {
		t.Fatalf("expected text event, got %v", events)
	}
}

func TestTranslateEvent_EmptyDelta(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: ""}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
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

func TestTranslateEvent_FunctionCallArgsDone(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			CallID:    "c1",
			Name:      "shell",
			Arguments: `{"cmd":"ls"}`,
		},
	}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
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
}

func TestTranslateEvent_FunctionCallArgsDone_NilItem(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "response.function_call_arguments.done"}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events for nil item")
	}
}

func TestTranslateEvent_OutputItemDone_NotFunctionCall(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{
		Type: "response.output_item.done",
		Item: &protocol.OutputItem{Type: "message"},
	}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events for non-function_call item")
	}
}

func TestTranslateEvent_OutputItemAdded(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "response.output_item.added"}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events on output_item.added")
	}
}

func TestTranslateEvent_ResponseDone(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{
		Type: "response.done",
		Response: &protocol.ResponseRef{
			Usage: &protocol.Usage{InputTokens: 200, OutputTokens: 80},
		},
	}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != harness.EventUsage {
		t.Fatalf("expected usage, got %v", events)
	}
}

func TestTranslateEvent_ResponseDone_NoUsage(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "response.done", Response: &protocol.ResponseRef{}}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events when no usage")
	}
}

func TestTranslateEvent_Error(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "error", Message: "bad request"}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Error.Message != "bad request" {
		t.Error("expected error event")
	}
}

func TestTranslateEvent_ErrorEmpty(t *testing.T) {
	h := &Harness{}
	ev := protocol.StreamEvent{Type: "error"}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
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
	ev := protocol.StreamEvent{Type: "something.random"}
	var events []harness.Event
	err := h.translateEvent(ev, func(e harness.Event) error {
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
