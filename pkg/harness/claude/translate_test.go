package claude

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"godex/pkg/harness"
)

// makeEvent constructs an anthropic.MessageStreamEventUnion from JSON.
func makeEvent(t *testing.T, jsonStr string) anthropic.MessageStreamEventUnion {
	t.Helper()
	var ev anthropic.MessageStreamEventUnion
	if err := json.Unmarshal([]byte(jsonStr), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	return ev
}

func TestTranslateEvent_TextDelta(t *testing.T) {
	h := New(Config{})
	state := &streamState{currentBlockType: "text"}

	ev := makeEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`)

	var events []harness.Event
	err := h.translateEvent(ev, state, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != harness.EventText {
		t.Errorf("expected text, got %s", events[0].Kind)
	}
	if events[0].Text.Delta != "Hello" {
		t.Errorf("expected 'Hello', got %q", events[0].Text.Delta)
	}
}

func TestTranslateEvent_ThinkingDelta(t *testing.T) {
	h := New(Config{})
	state := &streamState{currentBlockType: "thinking"}

	ev := makeEvent(t, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`)

	var events []harness.Event
	err := h.translateEvent(ev, state, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != harness.EventThinking {
		t.Errorf("expected thinking, got %s", events[0].Kind)
	}
	if events[0].Thinking.Delta != "Let me think..." {
		t.Errorf("unexpected thinking: %q", events[0].Thinking.Delta)
	}
	if state.thinkingText != "Let me think..." {
		t.Errorf("state not updated: %q", state.thinkingText)
	}
}

func TestTranslateEvent_InputJSONDelta(t *testing.T) {
	h := New(Config{})
	state := &streamState{
		currentBlockType: "tool_use",
		currentToolID:    "toolu_01",
		currentToolName:  "shell",
	}

	ev := makeEvent(t, `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"command\":"}}`)

	var events []harness.Event
	err := h.translateEvent(ev, state, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// input_json_delta doesn't emit events, just accumulates
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
	if state.toolArgsJSON != `{"command":` {
		t.Errorf("unexpected args: %q", state.toolArgsJSON)
	}
}

func TestTranslateEvent_ContentBlockStop_ToolUse(t *testing.T) {
	h := New(Config{})
	state := &streamState{
		currentBlockType: "tool_use",
		currentToolID:    "toolu_01",
		currentToolName:  "shell",
		toolArgsJSON:     `{"command":"ls"}`,
	}

	ev := makeEvent(t, `{"type":"content_block_stop","index":1}`)

	var events []harness.Event
	err := h.translateEvent(ev, state, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != harness.EventToolCall {
		t.Errorf("expected tool_call, got %s", events[0].Kind)
	}
	tc := events[0].ToolCall
	if tc.CallID != "toolu_01" || tc.Name != "shell" || tc.Arguments != `{"command":"ls"}` {
		t.Errorf("unexpected tool call: %+v", tc)
	}
	if state.currentBlockType != "" {
		t.Error("block type should be reset")
	}
}

func TestTranslateEvent_ContentBlockStart_Text(t *testing.T) {
	h := New(Config{})
	state := &streamState{}

	ev := makeEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)

	err := h.translateEvent(ev, state, func(e harness.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state.currentBlockType != "text" {
		t.Errorf("expected block type 'text', got %q", state.currentBlockType)
	}
}

func TestTranslateEvent_ContentBlockStart_Thinking(t *testing.T) {
	h := New(Config{})
	state := &streamState{}

	ev := makeEvent(t, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)

	err := h.translateEvent(ev, state, func(e harness.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state.currentBlockType != "thinking" {
		t.Errorf("expected block type 'thinking', got %q", state.currentBlockType)
	}
}

func TestTranslateEvent_ContentBlockStart_ToolUse(t *testing.T) {
	h := New(Config{})
	state := &streamState{}

	ev := makeEvent(t, `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_01","name":"shell","input":{}}}`)

	err := h.translateEvent(ev, state, func(e harness.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state.currentBlockType != "tool_use" {
		t.Errorf("expected block type 'tool_use', got %q", state.currentBlockType)
	}
	if state.currentToolID != "toolu_01" {
		t.Errorf("expected tool ID 'toolu_01', got %q", state.currentToolID)
	}
	if state.currentToolName != "shell" {
		t.Errorf("expected tool name 'shell', got %q", state.currentToolName)
	}
}

func TestTranslateEvent_MessageStart(t *testing.T) {
	h := New(Config{})
	state := &streamState{}

	ev := makeEvent(t, `{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":100,"output_tokens":0}}}`)

	err := h.translateEvent(ev, state, func(e harness.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state.inputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", state.inputTokens)
	}
}

func TestTranslateEvent_MessageDelta(t *testing.T) {
	h := New(Config{})
	state := &streamState{}

	ev := makeEvent(t, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":50}}`)

	err := h.translateEvent(ev, state, func(e harness.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state.outputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", state.outputTokens)
	}
}

func TestTranslateEvent_MessageStop_EmitsUsage(t *testing.T) {
	h := New(Config{})
	state := &streamState{inputTokens: 100, outputTokens: 50}

	ev := makeEvent(t, `{"type":"message_stop"}`)

	var events []harness.Event
	err := h.translateEvent(ev, state, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != harness.EventUsage {
		t.Errorf("expected usage, got %s", events[0].Kind)
	}
	if events[0].Usage.InputTokens != 100 || events[0].Usage.OutputTokens != 50 {
		t.Errorf("unexpected usage: %+v", events[0].Usage)
	}
}

func TestTranslateEvent_ContentBlockStop_Thinking(t *testing.T) {
	h := New(Config{})
	state := &streamState{currentBlockType: "thinking", thinkingText: "some thought"}

	ev := makeEvent(t, `{"type":"content_block_stop","index":0}`)

	var events []harness.Event
	err := h.translateEvent(ev, state, func(e harness.Event) error {
		events = append(events, e)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Thinking stop doesn't emit extra events (already streamed as deltas)
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
	if state.currentBlockType != "" {
		t.Error("block type should be reset")
	}
}

func TestTranslateEvent_FullFlow(t *testing.T) {
	h := New(Config{})
	state := &streamState{}
	var events []harness.Event
	emit := func(e harness.Event) error {
		events = append(events, e)
		return nil
	}

	// Simulate: message_start → thinking_start → thinking_delta → thinking_stop → text_start → text_delta → text_stop → message_delta → message_stop
	steps := []string{
		`{"type":"message_start","message":{"id":"msg_01","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":200,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"I need to "}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"analyze this."}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Here is my answer."}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":75}}`,
		`{"type":"message_stop"}`,
	}

	for _, s := range steps {
		ev := makeEvent(t, s)
		if err := h.translateEvent(ev, state, emit); err != nil {
			t.Fatalf("translate %s: %v", s[:30], err)
		}
	}

	// Expected: 2 thinking deltas + 1 text delta + 1 usage = 4 events
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventThinking {
		t.Errorf("event 0: expected thinking, got %s", events[0].Kind)
	}
	if events[1].Kind != harness.EventThinking {
		t.Errorf("event 1: expected thinking, got %s", events[1].Kind)
	}
	if events[2].Kind != harness.EventText {
		t.Errorf("event 2: expected text, got %s", events[2].Kind)
	}
	if events[3].Kind != harness.EventUsage {
		t.Errorf("event 3: expected usage, got %s", events[3].Kind)
	}
	if events[3].Usage.InputTokens != 200 || events[3].Usage.OutputTokens != 75 {
		t.Errorf("unexpected usage: %+v", events[3].Usage)
	}
}
