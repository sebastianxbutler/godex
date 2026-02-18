package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// Config holds configuration for the Codex harness.
type Config struct {
	// Client is the underlying Codex API client.
	Client *Client

	// DefaultModel is the model to use when Turn.Model is empty.
	DefaultModel string

	// NativeTools forces the use of Codex's built-in tools (shell, apply_patch,
	// update_plan) even when the caller provides their own tools. When false
	// (default), caller-provided tools replace the defaults in proxy mode.
	NativeTools bool

	// ExtraAliases are additional aliases merged with defaults.
	ExtraAliases map[string]string

	// ExtraPrefixes are additional match prefixes merged with defaults.
	ExtraPrefixes []string
}

// Harness implements harness.Harness for the Codex/Responses API.
type Harness struct {
	client        *Client
	defaultModel  string
	nativeTools   bool
	extraAliases  map[string]string
	extraPrefixes []string
}

// Ensure Harness implements the interface.
var _ harness.Harness = (*Harness)(nil)

// New creates a new Codex harness wrapping an existing backend client.
func New(cfg Config) *Harness {
	model := cfg.DefaultModel
	if model == "" {
		model = "gpt-5.2-codex"
	}
	return &Harness{
		client:        cfg.Client,
		defaultModel:  model,
		nativeTools:   cfg.NativeTools,
		extraAliases:  cfg.ExtraAliases,
		extraPrefixes: cfg.ExtraPrefixes,
	}
}

// Name returns "codex".
func (h *Harness) Name() string { return "codex" }

// StreamTurn executes a single turn, translating SSE events to structured harness events.
func (h *Harness) StreamTurn(ctx context.Context, turn *harness.Turn, onEvent func(harness.Event) error) error {
	req, err := h.buildRequest(turn)
	if err != nil {
		return fmt.Errorf("codex: build request: %w", err)
	}

	collector := sse.NewCollector()

	err = h.client.StreamResponses(ctx, req, func(ev sse.Event) error {
		collector.Observe(ev.Value)
		return h.translateEvent(ev.Value, collector, onEvent)
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

// ListModels returns available Codex models.
func (h *Harness) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	return h.listModelsWithDiscovery(ctx)
}

// buildRequest translates a harness.Turn into a protocol.ResponsesRequest.
func (h *Harness) buildRequest(turn *harness.Turn) (protocol.ResponsesRequest, error) {
	model := turn.Model
	if model == "" {
		model = h.defaultModel
	}

	// Build the system prompt.
	// - Proxy mode (caller tools, !nativeTools): keep Codex base prompt but
	//   replace tool-specific sections with caller's instructions.
	// - Native mode (no caller tools, or nativeTools flag): full Codex prompt.
	proxyMode := len(turn.Tools) > 0 && !h.nativeTools
	var instructions string
	if proxyMode {
		var err error
		instructions, err = BuildProxySystemPrompt(turn)
		if err != nil {
			return protocol.ResponsesRequest{}, err
		}
	} else {
		var err error
		instructions, err = BuildSystemPrompt(turn)
		if err != nil {
			return protocol.ResponsesRequest{}, err
		}
	}

	// Convert messages to protocol input items
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

	// Build tools: in proxy mode use caller-provided tools,
	// otherwise fall back to the default Codex tool set.
	var tools []protocol.ToolSpec
	if proxyMode {
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
	} else {
		tools = DefaultTools()
	}

	// Add reasoning config
	var reasoning *protocol.Reasoning
	if turn.Reasoning != nil {
		reasoning = &protocol.Reasoning{
			Effort: turn.Reasoning.Effort,
		}
		if turn.Reasoning.Summaries {
			reasoning.Summary = "auto"
		}
	}

	return protocol.ResponsesRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
		Tools:        tools,
		ToolChoice:   "auto",
		Reasoning:    reasoning,
		Store:        false,
		Stream:       true,
	}, nil
}

// translateEvent converts a raw SSE StreamEvent into structured harness events.
func (h *Harness) translateEvent(ev protocol.StreamEvent, collector *sse.Collector, emit func(harness.Event) error) error {
	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta != "" {
			return emit(harness.NewTextEvent(ev.Delta))
		}

	case "response.output_text.done":
		// Final text is already accumulated via deltas

	case "response.output_item.added":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			// We'll emit the tool call when it's done (arguments complete)
		}

	case "response.function_call_arguments.done":
		if ev.Item != nil {
			callID := ev.Item.CallID
			name := ev.Item.Name
			args := collector.FunctionArgs(callID)
			if args == "" {
				args = ev.Item.Arguments
			}

			// Check if this is an update_plan call — translate to PlanEvent
			if name == "update_plan" {
				return h.emitPlanEvents(args, emit)
			}

			return emit(harness.NewToolCallEvent(callID, name, args))
		}

	case "response.output_item.done":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			callID := ev.Item.CallID
			name := ev.Item.Name
			args := collector.FunctionArgs(callID)
			if args == "" {
				args = ev.Item.Arguments
			}

			if name == "update_plan" {
				return h.emitPlanEvents(args, emit)
			}

			return emit(harness.NewToolCallEvent(callID, name, args))
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

// emitPlanEvents parses update_plan arguments and emits PlanEvent for each step.
func (h *Harness) emitPlanEvents(argsJSON string, emit func(harness.Event) error) error {
	var plan struct {
		Steps []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &plan); err != nil {
		// If we can't parse, emit as a regular tool call
		return emit(harness.NewToolCallEvent("", "update_plan", argsJSON))
	}
	for i, step := range plan.Steps {
		ev := harness.Event{
			Kind:      harness.EventPlanUpdate,
			Timestamp: time.Now(),
			Plan: &harness.PlanEvent{
				Title:     step.Title,
				Status:    step.Status,
				StepIndex: i,
			},
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
	return nil
}
