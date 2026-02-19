package main

import (
	"context"
	"fmt"
	"time"

	"godex/pkg/auth"
	harnessCodex "godex/pkg/harness/codex"
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
		Model: "gpt-5.2-codex",
		Input: []protocol.ResponseInputItem{protocol.UserMessage("What is the weather in Austin, TX? Use web_search.")},
		Tools: []protocol.ToolSpec{
			{Type: "web_search", ExternalWebAccess: true},
		},
		ToolChoice:     "auto",
		Stream:         true,
		PromptCacheKey: "web-search-example",
	}

	cl := harnessCodex.NewClient(nil, store, harnessCodex.ClientConfig{SessionID: "web-search-example"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := cl.StreamAndCollect(ctx, req)
	if err != nil {
		panic(err)
	}
	fmt.Println(res.Text)
}
