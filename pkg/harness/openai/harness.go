package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// Config holds configuration for the OpenAI-compatible harness.
type Config struct {
	// Client is the underlying OpenAI-compatible API client.
	Client *Client

	// DefaultModel is the model to use when Turn.Model is empty.
	DefaultModel string
}

// streamClient abstracts the streaming API for testing.
type streamClient interface {
	StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error
	ListModels(ctx context.Context) ([]harness.ModelInfo, error)
}

// Harness implements harness.Harness for any OpenAI Chat Completions-compatible
// provider. It translates Chat Completions SSE into Codex-format events, then
// further translates those into structured harness.Event types.
type Harness struct {
	client       streamClient
	defaultModel string
}

var _ harness.Harness = (*Harness)(nil)

// New creates a new OpenAI-compatible harness.
func New(cfg Config) *Harness {
	model := cfg.DefaultModel
	if model == "" {
		model = "gpt-4o"
	}
	var sc streamClient
	if cfg.Client != nil {
		sc = cfg.Client
	}
	return &Harness{
		client:       sc,
		defaultModel: model,
	}
}

// Name returns "openai".
func (h *Harness) Name() string { return "openai" }

// StreamTurn executes a single turn, translating SSE events to structured harness events.
func (h *Harness) StreamTurn(ctx context.Context, turn *harness.Turn, onEvent func(harness.Event) error) error {
	if h.client == nil {
		return fmt.Errorf("openai: no client configured")
	}

	req, err := h.buildRequest(turn)
	if err != nil {
		return fmt.Errorf("openai: build request: %w", err)
	}

	// The client translates Chat Completions SSE into Codex-format
	// protocol.StreamEvent. We translate those into harness.Event.
	err = h.client.StreamResponses(ctx, req, func(ev sse.Event) error {
		return h.translateEvent(ev.Value, onEvent)
	})
	if err != nil {
		return err
	}

	return onEvent(harness.NewDoneEvent())
}

// StreamAndCollect executes a turn and returns collected results.
func (h *Harness) StreamAndCollect(ctx context.Context, turn *harness.Turn) (*harness.TurnResult, error) {
	start := time.Now()
	result := &harness.TurnResult{}
	err := h.StreamTurn(ctx, turn, func(ev harness.Event) error {
		result.Events = append(result.Events, ev)
		switch ev.Kind {
		case harness.EventText:
			if ev.Text != nil {
				result.FinalText += ev.Text.Delta
				if ev.Text.Complete != "" {
					result.FinalText = ev.Text.Complete
				}
			}
		case harness.EventUsage:
			result.Usage = ev.Usage
		case harness.EventToolCall:
			if ev.ToolCall != nil {
				result.ToolCalls = append(result.ToolCalls, *ev.ToolCall)
			}
		}
		return nil
	})
	result.Duration = time.Since(start)
	return result, err
}

// RunToolLoop executes the full agentic loop with the given tool handler.
func (h *Harness) RunToolLoop(ctx context.Context, turn *harness.Turn, handler harness.ToolHandler, opts harness.LoopOptions) (*harness.TurnResult, error) {
	return harness.RunToolLoop(ctx, h.StreamTurn, turn, handler, opts)
}

// ListModels returns available models.
func (h *Harness) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	if h.client == nil {
		return nil, fmt.Errorf("openai: no client configured")
	}
	return h.client.ListModels(ctx)
}

// buildRequest translates a harness.Turn into a protocol.ResponsesRequest.
func (h *Harness) buildRequest(turn *harness.Turn) (protocol.ResponsesRequest, error) {
	model := turn.Model
	if model == "" {
		model = h.defaultModel
	}

	instructions, err := BuildSystemPrompt(turn)
	if err != nil {
		return protocol.ResponsesRequest{}, err
	}

	input := make([]protocol.ResponseInputItem, 0, len(turn.Messages))
	for _, msg := range turn.Messages {
		switch msg.Role {
		case "user":
			input = append(input, protocol.UserMessage(msg.Content))
		case "tool":
			input = append(input, protocol.FunctionCallOutputInput(msg.ToolID, msg.Content))
		case "assistant":
			if msg.ToolID != "" {
				input = append(input, protocol.FunctionCallInput(msg.Name, msg.ToolID, msg.Content))
			} else {
				input = append(input, protocol.ResponseInputItem{
					Type: "message",
					Role: "assistant",
					Content: []protocol.InputContentPart{{
						Type: "input_text",
						Text: msg.Content,
					}},
				})
			}
		}
	}

	// Convert tools to protocol format
	var tools []protocol.ToolSpec
	for _, t := range turn.Tools {
		var params json.RawMessage
		if t.Parameters != nil {
			params, _ = json.Marshal(t.Parameters)
		}
		tools = append(tools, protocol.ToolSpec{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}

	var toolChoice string
	if len(tools) > 0 {
		toolChoice = "auto"
	}

	return protocol.ResponsesRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
		Tools:        tools,
		ToolChoice:   toolChoice,
		Stream:       true,
	}, nil
}

// translateEvent converts a Codex-format StreamEvent (produced by the backend
// openapi client's Chat Completions → Codex SSE translation) into harness events.
func (h *Harness) translateEvent(ev protocol.StreamEvent, emit func(harness.Event) error) error {
	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta != "" {
			return emit(harness.NewTextEvent(ev.Delta))
		}

	case "response.output_item.added":
		// Tool call started — we emit on completion

	case "response.function_call_arguments.done":
		if ev.Item != nil {
			return emit(harness.NewToolCallEvent(ev.Item.CallID, ev.Item.Name, ev.Item.Arguments))
		}

	case "response.output_item.done":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			return emit(harness.NewToolCallEvent(ev.Item.CallID, ev.Item.Name, ev.Item.Arguments))
		}

	case "response.completed", "response.done":
		if ev.Response != nil && ev.Response.Usage != nil {
			return emit(harness.NewUsageEvent(
				ev.Response.Usage.InputTokens,
				ev.Response.Usage.OutputTokens,
			))
		}

	case "error":
		msg := ev.Message
		if msg == "" {
			msg = "unknown error"
		}
		return emit(harness.NewErrorEvent(msg))
	}

	return nil
}

// end of file
