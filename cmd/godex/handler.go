package main

import (
	"context"
	"fmt"
	"strings"

	"godex/pkg/client"
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
