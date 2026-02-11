package client

import (
	"context"
	"fmt"

	"godex/pkg/protocol"
)

type ToolHandler interface {
	Handle(ctx context.Context, call ToolCall) (string, error)
}

type ToolLoopOptions struct {
	MaxSteps int
}

func (c *Client) RunToolLoop(ctx context.Context, req protocol.ResponsesRequest, handler ToolHandler, opts ToolLoopOptions) (StreamResult, error) {
	return RunToolLoopWith(ctx, req, handler, opts, c.StreamResponses)
}

func RunToolLoopWith(ctx context.Context, req protocol.ResponsesRequest, handler ToolHandler, opts ToolLoopOptions, stream Streamer) (StreamResult, error) {
	if handler == nil {
		return StreamResult{}, fmt.Errorf("tool handler is required")
	}
	max := opts.MaxSteps
	if max <= 0 {
		max = 4
	}
	current := req

	for step := 0; step < max; step++ {
		result, err := StreamAndCollectWith(ctx, current, stream)
		if err != nil {
			return StreamResult{}, err
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
	return StreamResult{}, fmt.Errorf("tool loop exceeded max steps")
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
