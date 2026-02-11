package client

import (
	"context"
	"testing"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

func TestStreamAndCollectWithToolCall(t *testing.T) {
	stream := func(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
		events := []protocol.StreamEvent{
			{Type: "response.output_item.added", Item: &protocol.OutputItem{ID: "item_1", Type: "function_call", CallID: "call_1", Name: "add"}},
			{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: "{\"a\":2,\"b\":3}"},
			{Type: "response.output_text.delta", Delta: "ignored"},
		}
		for _, ev := range events {
			if err := onEvent(sse.Event{Value: ev}); err != nil {
				return err
			}
		}
		return nil
	}

	res, err := StreamAndCollectWith(context.Background(), protocol.ResponsesRequest{}, stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	call := res.ToolCalls[0]
	if call.CallID != "call_1" || call.Name != "add" {
		t.Fatalf("unexpected call: %+v", call)
	}
	if call.Arguments != "{\"a\":2,\"b\":3}" {
		t.Fatalf("unexpected args: %s", call.Arguments)
	}
}
