package codex

import (
	"errors"
	"testing"

	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func TestTranslateEvent_FunctionCallArgsDone_Shell(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_456",
			Name:      "shell",
			Arguments: `{"command":["echo","hi"]}`,
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
}

func TestTranslateEvent_FunctionCallArgsDone_UpdatePlan(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	args := `{"steps":[{"title":"Read","status":"done"}]}`
	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
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
}

func TestTranslateEvent_FunctionCallArgsDone_NilItem(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.function_call_arguments.done"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
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

func TestTranslateEvent_FunctionCallArgsDone_NoItemUsesCollectorState(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()
	collector.Observe(protocol.StreamEvent{
		Type: "response.output_item.added",
		Item: &protocol.OutputItem{
			ID:     "item_exec",
			Type:   "function_call",
			CallID: "call_exec",
			Name:   "exec",
		},
	})
	collector.Observe(protocol.StreamEvent{
		Type:   "response.function_call_arguments.delta",
		ItemID: "item_exec",
		Delta:  `{"command":"ls"}`,
	})

	ev := protocol.StreamEvent{
		Type:   "response.function_call_arguments.done",
		ItemID: "item_exec",
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
	if events[0].ToolCall.Name != "exec" {
		t.Fatalf("expected exec, got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.Arguments != `{"command":"ls"}` {
		t.Fatalf("unexpected args: %q", events[0].ToolCall.Arguments)
	}
}

func TestTranslateEvent_DedupesDuplicateToolCallByCallID(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()
	ev1 := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_dup",
			Name:      "exec",
			Arguments: `{"command":"ls"}`,
		},
	}
	ev2 := protocol.StreamEvent{
		Type: "response.output_item.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_dup",
			Name:      "exec",
			Arguments: `{"command":"ls"}`,
		},
	}
	var events []harness.Event
	emit := func(e harness.Event) error {
		events = append(events, e)
		return nil
	}
	if err := h.translateEvent(ev1, collector, emit); err != nil {
		t.Fatal(err)
	}
	if err := h.translateEvent(ev2, collector, emit); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(events))
	}
}

func TestTranslateEvent_NormalizesConcatenatedJSONArgs(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()
	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_concat",
			Name:      "exec",
			Arguments: `{"command":"ls"}{"command":"ls"}`,
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
	if len(events) != 1 || events[0].ToolCall == nil {
		t.Fatalf("expected one tool_call event, got %v", events)
	}
	if events[0].ToolCall.Arguments != `{"command":"ls"}` {
		t.Fatalf("unexpected args: %q", events[0].ToolCall.Arguments)
	}
}

func TestTranslateEvent_NormalizesConcatenatedJSONArgs_UsesLastValue(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()
	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_concat_last",
			Name:      "exec",
			Arguments: `{}{"command":"ls"}`,
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
	if len(events) != 1 || events[0].ToolCall == nil {
		t.Fatalf("expected one tool_call event, got %v", events)
	}
	if events[0].ToolCall.Arguments != `{"command":"ls"}` {
		t.Fatalf("unexpected args: %q", events[0].ToolCall.Arguments)
	}
}

func TestTranslateEvent_PrefersDoneSnapshotOverCollectedPlaceholder(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	collector.Observe(protocol.StreamEvent{
		Type: "response.output_item.added",
		Item: &protocol.OutputItem{
			ID:        "item_exec",
			Type:      "function_call",
			CallID:    "call_exec",
			Name:      "exec",
			Arguments: "{}",
		},
	})

	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			Type:      "function_call",
			CallID:    "call_exec",
			Name:      "exec",
			Arguments: `{"command":"ls","workdir":"/home/cmd/clawd"}`,
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
	if len(events) != 1 || events[0].ToolCall == nil {
		t.Fatalf("expected one tool_call event, got %v", events)
	}
	if events[0].ToolCall.Arguments != `{"command":"ls","workdir":"/home/cmd/clawd"}` {
		t.Fatalf("unexpected args: %q", events[0].ToolCall.Arguments)
	}
}

func TestTranslateEvent_StripsNullOptionalArgs(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()
	ev := protocol.StreamEvent{
		Type: "response.function_call_arguments.done",
		Item: &protocol.OutputItem{
			Type:   "function_call",
			CallID: "call_nulls",
			Name:   "exec",
			Arguments: `{"command":"ls","workdir":null,"yieldMs":null,` +
				`"env":{"FOO":null,"BAR":"x"}}`,
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
	if len(events) != 1 || events[0].ToolCall == nil {
		t.Fatalf("expected one tool_call event, got %v", events)
	}
	if events[0].ToolCall.Arguments != `{"command":"ls","env":{"BAR":"x"}}` {
		t.Fatalf("unexpected args: %q", events[0].ToolCall.Arguments)
	}
}

func TestTranslateEvent_ResponseDone_NoUsage(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.completed", Response: &protocol.ResponseRef{}}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events when no usage data")
	}
}

func TestTranslateEvent_ResponseDone_NilResponse(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.completed"}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events")
	}
}

func TestTranslateEvent_OutputItemDone_NonFunctionCall(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{
		Type: "response.output_item.done",
		Item: &protocol.OutputItem{Type: "message"},
	}
	var events []harness.Event
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Error("expected no events for non-function_call done")
	}
}

func TestTranslateEvent_EmitError(t *testing.T) {
	h := &Harness{}
	collector := sse.NewCollector()

	ev := protocol.StreamEvent{Type: "response.output_text.delta", Delta: "text"}
	emitErr := errors.New("emit failed")
	err := h.translateEvent(ev, collector, func(e harness.Event) error {
		return emitErr
	})
	if !errors.Is(err, emitErr) {
		t.Errorf("expected emit error, got %v", err)
	}
}
