package harness

import "testing"

func TestEventKindString(t *testing.T) {
	tests := []struct {
		kind EventKind
		want string
	}{
		{EventText, "text"},
		{EventThinking, "thinking"},
		{EventToolCall, "tool_call"},
		{EventToolResult, "tool_result"},
		{EventPlanUpdate, "plan_update"},
		{EventPreamble, "preamble"},
		{EventUsage, "usage"},
		{EventError, "error"},
		{EventDone, "done"},
		{EventKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestEventConstructors(t *testing.T) {
	ev := NewTextEvent("hi")
	if ev.Kind != EventText || ev.Text.Delta != "hi" {
		t.Error("NewTextEvent failed")
	}

	ev = NewThinkingEvent("hmm")
	if ev.Kind != EventThinking || ev.Thinking.Delta != "hmm" {
		t.Error("NewThinkingEvent failed")
	}

	ev = NewToolCallEvent("c1", "shell", "{}")
	if ev.Kind != EventToolCall || ev.ToolCall.Name != "shell" {
		t.Error("NewToolCallEvent failed")
	}

	ev = NewToolResultEvent("c1", "output", false)
	if ev.Kind != EventToolResult || ev.ToolResult.Output != "output" {
		t.Error("NewToolResultEvent failed")
	}

	ev = NewPlanEvent("step 1", "pending")
	if ev.Kind != EventPlanUpdate || ev.Plan.Title != "step 1" {
		t.Error("NewPlanEvent failed")
	}

	ev = NewPreambleEvent("checking...")
	if ev.Kind != EventPreamble || ev.Preamble.Text != "checking..." {
		t.Error("NewPreambleEvent failed")
	}

	ev = NewUsageEvent(100, 50)
	if ev.Kind != EventUsage || ev.Usage.TotalTokens != 150 {
		t.Error("NewUsageEvent failed")
	}

	ev = NewErrorEvent("oops")
	if ev.Kind != EventError || ev.Error.Message != "oops" {
		t.Error("NewErrorEvent failed")
	}

	ev = NewDoneEvent()
	if ev.Kind != EventDone {
		t.Error("NewDoneEvent failed")
	}
}
