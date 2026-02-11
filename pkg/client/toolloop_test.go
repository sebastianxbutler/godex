package client

import (
	"context"
	"testing"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

type stubHandler struct{}

func (stubHandler) Handle(ctx context.Context, call ToolCall) (string, error) {
	if call.Name == "add" {
		return "5", nil
	}
	return "", nil
}

func TestRunToolLoopWith(t *testing.T) {
	calls := 0
	stream := func(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
		calls++
		if calls == 1 {
			events := []protocol.StreamEvent{
				{Type: "response.output_item.added", Item: &protocol.OutputItem{ID: "item_1", Type: "function_call", CallID: "call_1", Name: "add"}},
				{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: "{\"a\":2,\"b\":3}"},
			}
			for _, ev := range events {
				if err := onEvent(sse.Event{Value: ev}); err != nil {
					return err
				}
			}
			return nil
		}
		events := []protocol.StreamEvent{
			{Type: "response.output_text.delta", Delta: "done"},
		}
		for _, ev := range events {
			if err := onEvent(sse.Event{Value: ev}); err != nil {
				return err
			}
		}
		return nil
	}

	req := protocol.ResponsesRequest{Model: "gpt-5.2-codex"}
	res, err := RunToolLoopWith(context.Background(), req, stubHandler{}, ToolLoopOptions{MaxSteps: 2}, stream)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "done" {
		t.Fatalf("unexpected result text: %q", res.Text)
	}
}
