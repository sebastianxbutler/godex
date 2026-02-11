package main

import (
	"context"
	"fmt"
	"time"

	"godex/pkg/auth"
	"godex/pkg/client"
	"godex/pkg/protocol"
)

type staticHandler struct{}

func (staticHandler) Handle(ctx context.Context, call client.ToolCall) (string, error) {
	switch call.Name {
	case "add":
		return "5", nil
	default:
		return "err: unknown tool", nil
	}
}

func main() {
	path, err := auth.DefaultPath()
	if err != nil {
		panic(err)
	}
	store, err := auth.Load(path)
	if err != nil {
		panic(err)
	}

	req := protocol.ResponsesRequest{
		Model: "gpt-5.2-codex",
		Input: []protocol.ResponseInputItem{protocol.UserMessage("Call add(a=2,b=3)")},
		Tools: []protocol.ToolSpec{
			{
				Type:        "function",
				Name:        "add",
				Description: "Add two numbers",
				Parameters:  []byte(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
			},
		},
		ToolChoice:     "auto",
		Stream:         true,
		PromptCacheKey: "tool-loop-example",
	}

	cl := client.New(nil, store, client.Config{SessionID: "tool-loop-example"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := cl.RunToolLoop(ctx, req, staticHandler{}, client.ToolLoopOptions{MaxSteps: 2})
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Text)
}
