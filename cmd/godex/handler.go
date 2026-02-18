package main

import (
	"context"
	"fmt"
	"strings"

	"godex/pkg/client"
	"godex/pkg/harness"
)

type staticToolHandler struct {
	outputs map[string]string
}

func (h staticToolHandler) Handle(ctx context.Context, call client.ToolCall) (string, error) {
	if h.outputs == nil {
		return "", fmt.Errorf("no outputs configured")
	}
	val, ok := h.outputs[call.Name]
	if !ok {
		return "", fmt.Errorf("no output configured for %s", call.Name)
	}
	if val == "$args" {
		return call.Arguments, nil
	}
	return val, nil
}

// execToolHandler implements harness.ToolHandler for godex exec with static outputs.
type execToolHandler struct {
	outputs map[string]string
}

func (h execToolHandler) Handle(ctx context.Context, call harness.ToolCallEvent) (*harness.ToolResultEvent, error) {
	if h.outputs == nil {
		return nil, fmt.Errorf("no outputs configured")
	}
	val, ok := h.outputs[call.Name]
	if !ok {
		return nil, fmt.Errorf("no output configured for %s", call.Name)
	}
	output := val
	if output == "$args" {
		output = call.Arguments
	}
	return &harness.ToolResultEvent{
		CallID: call.CallID,
		Output: output,
	}, nil
}

func (h execToolHandler) Available() []harness.ToolSpec {
	return nil // tools are already set on the Turn
}

func parseToolOutputs(flags []string) (map[string]string, error) {
	outputs := map[string]string{}
	for _, raw := range flags {
		name, value, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("invalid --tool-output %q; expected name=value", raw)
		}
		outputs[strings.TrimSpace(name)] = value
	}
	return outputs, nil
}
