package claude

import (
	"context"
	"fmt"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"godex/pkg/harness"
)

// fakeStreamer implements messageStreamer for testing the real harness code path.
type fakeStreamer struct {
	events []anthropic.MessageStreamEventUnion
	models []harness.ModelInfo
	err    error
}

func (f *fakeStreamer) StreamMessages(ctx context.Context, params anthropic.MessageNewParams, onEvent func(anthropic.MessageStreamEventUnion) error) error {
	if f.err != nil {
		return f.err
	}
	for _, ev := range f.events {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStreamer) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	return f.models, nil
}

func makeTestEvent(jsonStr string) anthropic.MessageStreamEventUnion {
	var ev anthropic.MessageStreamEventUnion
	ev.UnmarshalJSON([]byte(jsonStr))
	return ev
}

func TestStreamTurn_ViaTestClient(t *testing.T) {
	h := New(Config{})
	h.testClient = &fakeStreamer{
		events: []anthropic.MessageStreamEventUnion{
			makeTestEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			makeTestEvent(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello world"}}`),
			makeTestEvent(`{"type":"content_block_stop","index":0}`),
			makeTestEvent(`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":50,"output_tokens":0}}}`),
			makeTestEvent(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`),
			makeTestEvent(`{"type":"message_stop"}`),
		},
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

	// Should have text delta + usage + done
	hasText := false
	hasDone := false
	for _, ev := range events {
		if ev.Kind == harness.EventText {
			hasText = true
		}
		if ev.Kind == harness.EventDone {
			hasDone = true
		}
	}
	if !hasText {
		t.Error("expected text event")
	}
	if !hasDone {
		t.Error("expected done event")
	}
}

func TestStreamTurn_StreamError(t *testing.T) {
	h := New(Config{})
	h.testClient = &fakeStreamer{err: fmt.Errorf("connection refused")}

	err := h.StreamTurn(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}, func(ev harness.Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListModels_ViaTestClient(t *testing.T) {
	h := New(Config{})
	h.testClient = &fakeStreamer{
		models: []harness.ModelInfo{
			{ID: "claude-sonnet-4-20250514", Provider: "claude"},
		},
	}

	models, err := h.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
}

func TestStreamAndCollect_ViaTestClient(t *testing.T) {
	h := New(Config{})
	h.testClient = &fakeStreamer{
		events: []anthropic.MessageStreamEventUnion{
			makeTestEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			makeTestEvent(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Response text"}}`),
			makeTestEvent(`{"type":"content_block_stop","index":0}`),
			makeTestEvent(`{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","usage":{"input_tokens":20,"output_tokens":0}}}`),
			makeTestEvent(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`),
			makeTestEvent(`{"type":"message_stop"}`),
		},
	}

	result, err := h.StreamAndCollect(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Response text" {
		t.Errorf("expected 'Response text', got %q", result.FinalText)
	}
}

func TestRunToolLoop_ViaTestClient(t *testing.T) {
	// Just test that it delegates properly
	h := New(Config{})
	h.testClient = &fakeStreamer{
		events: []anthropic.MessageStreamEventUnion{
			makeTestEvent(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`),
			makeTestEvent(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Done"}}`),
			makeTestEvent(`{"type":"content_block_stop","index":0}`),
			makeTestEvent(`{"type":"message_stop"}`),
		},
	}

	handler := &simpleHandler{}
	result, err := h.RunToolLoop(context.Background(), &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "test"}},
	}, handler, harness.LoopOptions{MaxTurns: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Done" {
		t.Errorf("expected 'Done', got %q", result.FinalText)
	}
}

type simpleHandler struct{}

func (h *simpleHandler) Handle(_ context.Context, call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
	return &harness.ToolResultEvent{CallID: call.CallID, Output: "ok"}, nil
}

func (h *simpleHandler) Available() []harness.ToolSpec { return nil }
