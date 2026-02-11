package client

import (
	"context"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

type ToolCall struct {
	CallID    string
	Name      string
	Arguments string
}

type StreamResult struct {
	Text      string
	ToolCalls []ToolCall
}

type Streamer func(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error

// StreamAndCollectWith streams using the provided function and returns collected output text + tool calls.
func StreamAndCollectWith(ctx context.Context, req protocol.ResponsesRequest, stream Streamer) (StreamResult, error) {
	collector := sse.NewCollector()
	calls := map[string]ToolCall{}

	err := stream(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if ev.Value.Type == "response.output_item.added" && ev.Value.Item != nil {
			item := ev.Value.Item
			if item.Type == "function_call" && item.CallID != "" {
				calls[item.CallID] = ToolCall{CallID: item.CallID, Name: item.Name}
			}
		}
		return nil
	})
	if err != nil {
		return StreamResult{}, err
	}

	out := StreamResult{Text: collector.OutputText()}
	for callID, tc := range calls {
		tc.Arguments = collector.FunctionArgs(callID)
		out.ToolCalls = append(out.ToolCalls, tc)
	}
	return out, nil
}

// StreamAndCollect streams a request and returns collected output text + tool calls.
func (c *Client) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error) {
	return StreamAndCollectWith(ctx, req, c.StreamResponses)
}
