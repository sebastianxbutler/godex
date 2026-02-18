package harness

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockName(t *testing.T) {
	m := NewMock(MockConfig{})
	if m.Name() != "mock" {
		t.Errorf("expected 'mock', got %q", m.Name())
	}
	m2 := NewMock(MockConfig{HarnessName: "test"})
	if m2.Name() != "test" {
		t.Errorf("expected 'test', got %q", m2.Name())
	}
}

func TestMockStreamTurn(t *testing.T) {
	events := []Event{
		NewTextEvent("hello"),
		NewTextEvent(" world"),
		NewDoneEvent(),
	}
	m := NewMock(MockConfig{Responses: [][]Event{events}})

	var got []Event
	err := m.StreamTurn(context.Background(), &Turn{Model: "test"}, func(ev Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Text.Delta != "hello" {
		t.Errorf("expected 'hello', got %q", got[0].Text.Delta)
	}
}

func TestMockNoMoreResponses(t *testing.T) {
	m := NewMock(MockConfig{Responses: [][]Event{{NewDoneEvent()}}})
	m.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	err := m.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	if err == nil {
		t.Fatal("expected error for exhausted responses")
	}
}

func TestMockRecord(t *testing.T) {
	m := NewMock(MockConfig{
		Record:    true,
		Responses: [][]Event{{NewDoneEvent()}},
	})
	turn := &Turn{Model: "gpt-4"}
	m.StreamTurn(context.Background(), turn, func(Event) error { return nil })
	rec := m.Recorded()
	if len(rec) != 1 || rec[0].Model != "gpt-4" {
		t.Errorf("recorded turns mismatch")
	}
}

func TestMockFailAfterN(t *testing.T) {
	events := []Event{NewTextEvent("a"), NewTextEvent("b"), NewTextEvent("c")}
	injectedErr := errors.New("boom")
	m := NewMock(MockConfig{
		Responses:  [][]Event{events},
		FailAfterN: 2,
		FailErr:    injectedErr,
	})

	var count int
	err := m.StreamTurn(context.Background(), &Turn{}, func(Event) error {
		count++
		return nil
	})
	if !errors.Is(err, injectedErr) {
		t.Errorf("expected injected error, got %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 events before failure, got %d", count)
	}
}

func TestMockFailAfterNDefaultErr(t *testing.T) {
	events := []Event{NewTextEvent("a"), NewTextEvent("b")}
	m := NewMock(MockConfig{
		Responses:  [][]Event{events},
		FailAfterN: 1,
	})
	err := m.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockContextCancel(t *testing.T) {
	events := []Event{NewTextEvent("a"), NewTextEvent("b")}
	m := NewMock(MockConfig{
		Responses:  [][]Event{events},
		EventDelay: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.StreamTurn(ctx, &Turn{}, func(Event) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestMockStreamAndCollect(t *testing.T) {
	events := []Event{
		NewTextEvent("hello"),
		NewUsageEvent(100, 50),
		NewToolCallEvent("c1", "shell", `{"cmd":"ls"}`),
		NewDoneEvent(),
	}
	m := NewMock(MockConfig{Responses: [][]Event{events}})
	result, err := m.StreamAndCollect(context.Background(), &Turn{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "hello" {
		t.Errorf("expected 'hello', got %q", result.FinalText)
	}
	if result.Usage == nil || result.Usage.InputTokens != 100 {
		t.Error("usage not collected")
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
}

func TestMockStreamAndCollectCompleteText(t *testing.T) {
	events := []Event{
		NewTextEvent("partial"),
		{Kind: EventText, Timestamp: time.Now(), Text: &TextEvent{Complete: "full text"}},
		NewDoneEvent(),
	}
	m := NewMock(MockConfig{Responses: [][]Event{events}})
	result, err := m.StreamAndCollect(context.Background(), &Turn{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "full text" {
		t.Errorf("expected 'full text', got %q", result.FinalText)
	}
}

func TestMockCallCount(t *testing.T) {
	m := NewMock(MockConfig{Responses: [][]Event{{NewDoneEvent()}, {NewDoneEvent()}}})
	m.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	if m.CallCount() != 1 {
		t.Errorf("expected 1, got %d", m.CallCount())
	}
	m.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	if m.CallCount() != 2 {
		t.Errorf("expected 2, got %d", m.CallCount())
	}
}

func TestMockListModels(t *testing.T) {
	models := []ModelInfo{{ID: "gpt-4", Name: "GPT-4"}}
	m := NewMock(MockConfig{Models: models})
	got, err := m.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "gpt-4" {
		t.Error("models mismatch")
	}
}

func TestMockCallbackError(t *testing.T) {
	m := NewMock(MockConfig{Responses: [][]Event{{NewTextEvent("a")}}})
	cbErr := errors.New("callback err")
	err := m.StreamTurn(context.Background(), &Turn{}, func(Event) error { return cbErr })
	if !errors.Is(err, cbErr) {
		t.Errorf("expected callback error, got %v", err)
	}
}

// Simple tool handler for testing RunToolLoop
type testHandler struct {
	results map[string]*ToolResultEvent
}

func (h *testHandler) Handle(_ context.Context, call ToolCallEvent) (*ToolResultEvent, error) {
	if r, ok := h.results[call.CallID]; ok {
		return r, nil
	}
	return &ToolResultEvent{CallID: call.CallID, Output: "ok"}, nil
}

func (h *testHandler) Available() []ToolSpec {
	return []ToolSpec{{Name: "test"}}
}

func TestMockRunToolLoop(t *testing.T) {
	// Turn 1: tool call, Turn 2: final text
	m := NewMock(MockConfig{
		Responses: [][]Event{
			{NewToolCallEvent("c1", "shell", "{}"), NewDoneEvent()},
			{NewTextEvent("done"), NewDoneEvent()},
		},
	})
	handler := &testHandler{}
	result, err := m.RunToolLoop(context.Background(), &Turn{}, handler, LoopOptions{MaxTurns: 5})
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
