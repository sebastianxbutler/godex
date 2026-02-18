package openai

import (
	"context"
	"testing"

	"godex/pkg/harness"
)

func TestNewMock_Defaults(t *testing.T) {
	mock := NewMock()
	if mock.Name() != "openai" {
		t.Errorf("expected 'openai', got %q", mock.Name())
	}
	models, err := mock.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestWithFunctionCallFlow(t *testing.T) {
	mock := NewMock(WithFunctionCallFlow("shell", `{"command":"ls"}`, "Files listed."))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test"}

	// First turn: tool call
	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
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
	if events[0].ToolCall.Name != "shell" {
		t.Errorf("expected 'shell', got %q", events[0].ToolCall.Name)
	}
	if events[0].ToolCall.CallID != "call_01" {
		t.Errorf("unexpected call ID: %s", events[0].ToolCall.CallID)
	}

	// Second turn: text response
	events = events[:0]
	err = mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventText {
		t.Errorf("expected text, got %s", events[0].Kind)
	}
	if events[0].Text.Delta != "Files listed." {
		t.Errorf("unexpected text: %q", events[0].Text.Delta)
	}
	if events[1].Kind != harness.EventUsage {
		t.Errorf("expected usage, got %s", events[1].Kind)
	}
}

func TestWithTextResponse(t *testing.T) {
	mock := NewMock(WithTextResponse("Hello from OpenAI!"))
	result, err := mock.StreamAndCollect(context.Background(), &harness.Turn{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Hello from OpenAI!" {
		t.Errorf("expected 'Hello from OpenAI!', got %q", result.FinalText)
	}
	if result.Usage == nil {
		t.Error("expected usage")
	}
}

func TestWithMultipleFunctionCalls(t *testing.T) {
	calls := []harness.ToolCallEvent{
		{CallID: "call_01", Name: "read", Arguments: `{"path":"a.go"}`},
		{CallID: "call_02", Name: "read", Arguments: `{"path":"b.go"}`},
	}
	mock := NewMock(WithMultipleFunctionCalls(calls, "Read both files."))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test"}

	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ToolCall.CallID != "call_01" {
		t.Errorf("unexpected first call ID: %s", events[0].ToolCall.CallID)
	}
	if events[1].ToolCall.CallID != "call_02" {
		t.Errorf("unexpected second call ID: %s", events[1].ToolCall.CallID)
	}
}

func TestWithErrorResponse(t *testing.T) {
	mock := NewMock(WithErrorResponse("quota exceeded"))

	var events []harness.Event
	err := mock.StreamTurn(context.Background(), &harness.Turn{Model: "test"}, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != harness.EventError {
		t.Errorf("expected error, got %s", events[0].Kind)
	}
	if events[0].Error.Message != "quota exceeded" {
		t.Errorf("unexpected error msg: %q", events[0].Error.Message)
	}
}

func TestMock_RecordsTurns(t *testing.T) {
	mock := NewMock(WithTextResponse("ok"))
	turn := &harness.Turn{
		Model:    "gpt-4o-mini",
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}
	mock.StreamAndCollect(context.Background(), turn)
	recorded := mock.Recorded()
	if len(recorded) != 1 {
		t.Fatalf("expected 1 recorded turn, got %d", len(recorded))
	}
	if recorded[0].Model != "gpt-4o-mini" {
		t.Errorf("unexpected model: %s", recorded[0].Model)
	}
}
