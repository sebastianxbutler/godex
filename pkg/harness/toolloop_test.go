package harness

import (
	"context"
	"errors"
	"testing"
)

func TestRunToolLoop_NoToolCalls(t *testing.T) {
	events := []Event{NewTextEvent("hello"), NewDoneEvent()}
	mock := NewMock(MockConfig{Responses: [][]Event{events}})

	handler := &testHandler{results: map[string]*ToolResultEvent{}}
	result, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "hello" {
		t.Errorf("expected 'hello', got %q", result.FinalText)
	}
}

func TestRunToolLoop_WithToolCalls(t *testing.T) {
	mock := NewMock(MockConfig{
		Responses: [][]Event{
			{NewToolCallEvent("c1", "shell", `{"cmd":"ls"}`), NewDoneEvent()},
			{NewTextEvent("done"), NewUsageEvent(100, 50), NewDoneEvent()},
		},
	})

	handler := &testHandler{results: map[string]*ToolResultEvent{
		"c1": {CallID: "c1", Output: "file.go"},
	}}

	result, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "done" {
		t.Errorf("expected 'done', got %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.Usage == nil || result.Usage.InputTokens != 100 {
		t.Error("usage not collected")
	}
}

func TestRunToolLoop_MaxTurnsDefault(t *testing.T) {
	// Default max turns is 10; just verify it doesn't panic with 0
	mock := NewMock(MockConfig{
		Responses: [][]Event{{NewTextEvent("ok"), NewDoneEvent()}},
	})
	handler := &testHandler{results: map[string]*ToolResultEvent{}}
	result, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "ok" {
		t.Errorf("expected 'ok', got %q", result.FinalText)
	}
}

func TestRunToolLoop_StreamError(t *testing.T) {
	injectedErr := errors.New("stream failed")
	mock := NewMock(MockConfig{
		Responses:  [][]Event{{NewTextEvent("a"), NewTextEvent("b")}},
		FailAfterN: 1,
		FailErr:    injectedErr,
	})

	handler := &testHandler{results: map[string]*ToolResultEvent{}}
	_, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{MaxTurns: 5})
	if !errors.Is(err, injectedErr) {
		t.Errorf("expected injected error, got %v", err)
	}
}

func TestRunToolLoop_ToolHandlerError(t *testing.T) {
	mock := NewMock(MockConfig{
		Responses: [][]Event{
			{NewToolCallEvent("c1", "shell", "{}"), NewDoneEvent()},
		},
	})

	handlerErr := errors.New("tool failed")
	handler := &errorHandler{err: handlerErr}

	_, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{MaxTurns: 5})
	if !errors.Is(err, handlerErr) {
		t.Errorf("expected handler error, got %v", err)
	}
}

func TestRunToolLoop_OnEvent(t *testing.T) {
	mock := NewMock(MockConfig{
		Responses: [][]Event{
			{NewTextEvent("hi"), NewDoneEvent()},
		},
	})

	var eventCount int
	handler := &testHandler{results: map[string]*ToolResultEvent{}}
	_, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{
		MaxTurns: 5,
		OnEvent: func(ev Event) error {
			eventCount++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if eventCount != 2 { // text + done
		t.Errorf("expected 2 events, got %d", eventCount)
	}
}

func TestRunToolLoop_OnEventError(t *testing.T) {
	mock := NewMock(MockConfig{
		Responses: [][]Event{
			{NewTextEvent("hi"), NewDoneEvent()},
		},
	})

	cbErr := errors.New("callback error")
	handler := &testHandler{results: map[string]*ToolResultEvent{}}
	_, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{
		MaxTurns: 5,
		OnEvent:  func(ev Event) error { return cbErr },
	})
	if !errors.Is(err, cbErr) {
		t.Errorf("expected callback error, got %v", err)
	}
}

func TestRunToolLoop_CompleteText(t *testing.T) {
	events := []Event{
		NewTextEvent("partial"),
		{Kind: EventText, Text: &TextEvent{Complete: "full output"}},
		NewDoneEvent(),
	}
	mock := NewMock(MockConfig{Responses: [][]Event{events}})
	handler := &testHandler{results: map[string]*ToolResultEvent{}}

	result, err := RunToolLoop(context.Background(), mock.StreamTurn, &Turn{}, handler, LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "full output" {
		t.Errorf("expected 'full output', got %q", result.FinalText)
	}
}

// errorHandler always returns an error
type errorHandler struct {
	err error
}

func (h *errorHandler) Handle(_ context.Context, call ToolCallEvent) (*ToolResultEvent, error) {
	return nil, h.err
}

func (h *errorHandler) Available() []ToolSpec { return nil }
