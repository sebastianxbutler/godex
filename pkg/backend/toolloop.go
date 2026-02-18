package backend

import (
	"context"
	"fmt"

	"godex/pkg/protocol"
)

// RunToolLoop executes a tool loop with any Backend implementation.
// It calls StreamAndCollect, checks for tool calls, executes them via handler,
// and sends follow-up requests until the model produces a final text response
// or MaxSteps is reached.
func RunToolLoop(ctx context.Context, be Backend, req protocol.ResponsesRequest, handler ToolHandler, opts ToolLoopOptions) (StreamResult, error) {
	if handler == nil {
		return StreamResult{}, fmt.Errorf("tool handler is required")
	}
	max := opts.MaxSteps
	if max <= 0 {
		max = 4
	}
	current := req

	for step := 0; step < max; step++ {
		result, err := be.StreamAndCollect(ctx, current)
		if err != nil {
			return StreamResult{}, err
		}
		if len(result.ToolCalls) == 0 {
			return result, nil
		}

		outputs := map[string]string{}
		for _, call := range result.ToolCalls {
			out, herr := handler.Handle(ctx, call)
			if herr != nil {
				out = "err: " + herr.Error()
			}
			outputs[call.CallID] = out
		}

		current = followupRequest(req, BuildToolFollowupInputs(result.ToolCalls, outputs))
	}
	return StreamResult{}, fmt.Errorf("tool loop exceeded %d steps", max)
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
func BuildToolFollowupInputs(calls []ToolCall, outputs map[string]string) []protocol.ResponseInputItem {
	items := make([]protocol.ResponseInputItem, 0, len(calls)*2)
	for _, call := range calls {
		items = append(items, protocol.FunctionCallInput(call.Name, call.CallID, call.Arguments))
		output := outputs[call.CallID]
		items = append(items, protocol.FunctionCallOutputInput(call.CallID, output))
	}
	return items
}
