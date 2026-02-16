package codex

import (
	"context"
	"fmt"

	"godex/pkg/backend"
	"godex/pkg/protocol"
)

// RunToolLoop executes a tool loop with the given handler.
func (c *Client) RunToolLoop(ctx context.Context, req protocol.ResponsesRequest, handler backend.ToolHandler, opts backend.ToolLoopOptions) (backend.StreamResult, error) {
	if handler == nil {
		return backend.StreamResult{}, fmt.Errorf("tool handler is required")
	}
	max := opts.MaxSteps
	if max <= 0 {
		max = 4
	}
	current := req

	for step := 0; step < max; step++ {
		result, err := c.StreamAndCollect(ctx, current)
		if err != nil {
			return backend.StreamResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			return result, nil
		}

		outputs := map[string]string{}
		for _, call := range result.ToolCalls {
			out, err := handler.Handle(ctx, call)
			if err != nil {
				out = "err: " + err.Error()
			}
			outputs[call.CallID] = out
		}

		current = followupRequest(req, BuildToolFollowupInputs(result.ToolCalls, outputs))
	}
	return backend.StreamResult{}, fmt.Errorf("tool loop exceeded max steps")
}

func followupRequest(base protocol.ResponsesRequest, input []protocol.ResponseInputItem) protocol.ResponsesRequest {
	return protocol.ResponsesRequest{
		Model:             base.Model,
		Instructions:      base.Instructions,
		Input:             input,
		Tools:             base.Tools,
		ToolChoice:        "auto",
		ParallelToolCalls: base.ParallelToolCalls,
		Reasoning:         base.Reasoning,
		Store:             base.Store,
		Stream:            true,
		Include:           base.Include,
		PromptCacheKey:    base.PromptCacheKey,
		Text:              base.Text,
	}
}

// BuildToolFollowupInputs builds follow-up input items containing the tool call
// and tool output pairs. Outputs map is keyed by call_id.
func BuildToolFollowupInputs(calls []backend.ToolCall, outputs map[string]string) []protocol.ResponseInputItem {
	items := make([]protocol.ResponseInputItem, 0, len(calls)*2)
	for _, call := range calls {
		items = append(items, protocol.FunctionCallInput(call.Name, call.CallID, call.Arguments))
		output := outputs[call.CallID]
		items = append(items, protocol.FunctionCallOutputInput(call.CallID, output))
	}
	return items
}
