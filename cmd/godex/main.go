package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"godex/pkg/auth"
	"godex/pkg/client"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

type toolFlags []string

type outputFlags []string

func (t *toolFlags) String() string { return strings.Join(*t, ",") }
func (t *toolFlags) Set(v string) error {
	*t = append(*t, v)
	return nil
}

func (o *outputFlags) String() string { return strings.Join(*o, ",") }
func (o *outputFlags) Set(v string) error {
	*o = append(*o, v)
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "exec":
		if err := runExec(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func runExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var prompt string
	var model string
	var instructions string
	var trace bool
	var allowRefresh bool
	var autoTools bool
	var webSearch bool
	var tools toolFlags
	var outputs outputFlags

	fs.StringVar(&prompt, "prompt", "", "User prompt")
	fs.StringVar(&model, "model", "gpt-5.2-codex", "Model name")
	fs.StringVar(&instructions, "instructions", "", "Optional system instructions")
	fs.BoolVar(&trace, "trace", false, "Print raw SSE event JSON")
	fs.BoolVar(&allowRefresh, "allow-refresh", false, "Allow network token refresh on 401")
	fs.BoolVar(&autoTools, "auto-tools", false, "Automatically run tool loop with static outputs")
	fs.BoolVar(&webSearch, "web-search", false, "Enable web_search tool")
	fs.Var(&tools, "tool", "Tool spec (repeatable): web_search or name:json=/path/schema.json")
	fs.Var(&outputs, "tool-output", "Static tool output: name=value or name=$args (repeatable)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("--prompt is required")
	}

	authPath, err := auth.DefaultPath()
	if err != nil {
		return err
	}
	store, err := auth.Load(authPath)
	if err != nil {
		return err
	}

	sessionID, err := newSessionID()
	if err != nil {
		return err
	}

	toolSpecs, err := parseToolSpecs(tools)
	if err != nil {
		return err
	}
	if webSearch {
		toolSpecs = append(toolSpecs, protocol.ToolSpec{Type: "web_search", ExternalWebAccess: true})
	}

	req := protocol.ResponsesRequest{
		Model:             model,
		Instructions:      instructions,
		Input:             []protocol.ResponseInputItem{protocol.UserMessage(prompt)},
		Tools:             toolSpecs,
		ToolChoice:        "auto",
		ParallelToolCalls: false,
		Store:             false,
		Stream:            true,
		Include:           []string{},
		PromptCacheKey:    sessionID,
	}

	cl := client.New(nil, store, client.Config{
		SessionID:    sessionID,
		AllowRefresh: allowRefresh,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if autoTools {
		outputs, err := parseToolOutputs(outputs)
		if err != nil {
			return err
		}
		handler := staticToolHandler{outputs: outputs}
		result, err := cl.RunToolLoop(ctx, req, handler, client.ToolLoopOptions{MaxSteps: 4})
		if err != nil {
			return err
		}
		fmt.Print(result.Text)
		return nil
	}

	collector := sse.NewCollector()
	return cl.StreamResponses(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		if trace {
			fmt.Println(string(ev.Raw))
		}
		switch ev.Value.Type {
		case "response.output_text.delta":
			fmt.Print(ev.Value.Delta)
		case "response.content_part.added":
			if ev.Value.Part != nil && ev.Value.Part.Type == "output_text" {
				fmt.Print(ev.Value.Part.Text)
			}
		}
		return nil
	})
}

func parseToolSpecs(flags []string) ([]protocol.ToolSpec, error) {
	if len(flags) == 0 {
		return nil, nil
	}
	tools := make([]protocol.ToolSpec, 0, len(flags))
	for _, raw := range flags {
		if raw == "web_search" {
			tools = append(tools, protocol.ToolSpec{Type: "web_search", ExternalWebAccess: true})
			continue
		}
		name, path, ok := strings.Cut(raw, ":json=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("invalid --tool %q; expected web_search or name:json=path", raw)
		}
		buf, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read tool schema %s: %w", path, err)
		}
		var rawSchema json.RawMessage
		if err := json.Unmarshal(buf, &rawSchema); err != nil {
			return nil, fmt.Errorf("parse tool schema %s: %w", path, err)
		}
		tools = append(tools, protocol.ToolSpec{
			Type:       "function",
			Name:       name,
			Parameters: rawSchema,
			Strict:     false,
		})
	}
	return tools, nil
}

func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: godex exec --prompt \"...\" [--model gpt-5.2-codex] [--tool web_search] [--tool name:json=schema.json] [--web-search] [--auto-tools --tool-output name=value] [--trace]")
}
