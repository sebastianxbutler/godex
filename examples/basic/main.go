package main

import (
	"context"
	"fmt"
	"time"

	"godex/pkg/auth"
	"godex/pkg/client"
	"godex/pkg/protocol"
)

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
		Model:          "gpt-5.2-codex",
		Instructions:   "",
		Input:          []protocol.ResponseInputItem{protocol.UserMessage("Hello from godex")},
		ToolChoice:     "auto",
		Stream:         true,
		PromptCacheKey: "example",
	}

	cl := client.New(nil, store, client.Config{SessionID: "example"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := cl.StreamAndCollect(ctx, req)
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Text)
}
