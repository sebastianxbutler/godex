package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"godex/pkg/harness"
)

// Config holds configuration for the Claude harness.
type Config struct {
	// Client is the underlying Anthropic client wrapper.
	Client *ClientWrapper

	// DefaultModel is the model used when Turn.Model is empty.
	DefaultModel string

	// DefaultMaxTokens is the max_tokens for API calls.
	DefaultMaxTokens int

	// ThinkingBudget is the budget_tokens for extended thinking.
	// Set to 0 to disable extended thinking.
	ThinkingBudget int
}

// messageStreamer abstracts the streaming API for testing.
type messageStreamer interface {
	StreamMessages(ctx context.Context, params anthropic.MessageNewParams, onEvent func(anthropic.MessageStreamEventUnion) error) error
	ListModels(ctx context.Context) ([]harness.ModelInfo, error)
}

// Harness implements harness.Harness for the Anthropic Messages API.
type Harness struct {
	client       *ClientWrapper
	defaultModel string
	maxTokens    int
	thinkBudget  int
	testClient   messageStreamer // for testing only; nil in production
}

var _ harness.Harness = (*Harness)(nil)

// New creates a new Claude harness.
func New(cfg Config) *Harness {
	model := cfg.DefaultModel
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	maxTokens := cfg.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = 16384
	}
	return &Harness{
		client:       cfg.Client,
		defaultModel: model,
		maxTokens:    maxTokens,
		thinkBudget:  cfg.ThinkingBudget,
	}
}

// Name returns "claude".
func (h *Harness) Name() string { return "claude" }

// StreamTurn executes a single turn using the Anthropic Messages API.
func (h *Harness) StreamTurn(ctx context.Context, turn *harness.Turn, onEvent func(harness.Event) error) error {
	params, err := h.buildRequest(turn)
	if err != nil {
		return fmt.Errorf("claude: build request: %w", err)
	}

	state := &streamState{}

	streamer := messageStreamer(h.client)
	if h.testClient != nil {
		streamer = h.testClient
	}

	err = streamer.StreamMessages(ctx, params, func(ev anthropic.MessageStreamEventUnion) error {
		return h.translateEvent(ev, state, onEvent)
	})
	if err != nil {
		return err
	}

	return onEvent(harness.NewDoneEvent())
}

// StreamAndCollect executes a turn and returns the collected result.
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

// RunToolLoop executes the full agentic loop.
func (h *Harness) RunToolLoop(ctx context.Context, turn *harness.Turn, handler harness.ToolHandler, opts harness.LoopOptions) (*harness.TurnResult, error) {
	return harness.RunToolLoop(ctx, h.StreamTurn, turn, handler, opts)
}

// ListModels returns available Claude models.
func (h *Harness) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	if h.testClient != nil {
		return h.testClient.ListModels(ctx)
	}
	return h.client.ListModels(ctx)
}

// buildRequest translates a harness.Turn to Anthropic MessageNewParams.
func (h *Harness) buildRequest(turn *harness.Turn) (anthropic.MessageNewParams, error) {
	model := turn.Model
	if model == "" {
		model = h.defaultModel
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(h.maxTokens),
	}

	// Build the system prompt using Claude-specific patterns
	systemText, err := BuildSystemPrompt(turn)
	if err != nil {
		return params, fmt.Errorf("build system prompt: %w", err)
	}
	if systemText != "" {
		params.System = []anthropic.TextBlockParam{{Text: systemText}}
	}

	// Convert messages
	var messages []anthropic.MessageParam
	for _, msg := range turn.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(msg.Content),
			))
		case "assistant":
			if msg.ToolID != "" {
				var inputMap map[string]any
				if msg.Content != "" {
					json.Unmarshal([]byte(msg.Content), &inputMap)
				}
				messages = append(messages, anthropic.NewAssistantMessage(
					anthropic.NewToolUseBlock(msg.ToolID, inputMap, msg.Name),
				))
			} else {
				messages = append(messages, anthropic.NewAssistantMessage(
					anthropic.NewTextBlock(msg.Content),
				))
			}
		case "tool":
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolID, msg.Content, false),
			))
		}
	}
	params.Messages = messages

	// Convert tools
	if len(turn.Tools) > 0 {
		var tools []anthropic.ToolUnionParam
		for _, t := range turn.Tools {
			schema := anthropic.ToolInputSchemaParam{}
			if t.Parameters != nil {
				if props, ok := t.Parameters["properties"].(map[string]any); ok {
					schema.Properties = props
				}
				if req, ok := t.Parameters["required"].([]any); ok {
					for _, r := range req {
						if s, ok := r.(string); ok {
							schema.Required = append(schema.Required, s)
						}
					}
				}
			}
			tools = append(tools, anthropic.ToolUnionParam{
				OfTool: &anthropic.ToolParam{
					Name:        t.Name,
					Description: anthropic.String(t.Description),
					InputSchema: schema,
				},
			})
		}
		params.Tools = tools
		params.ToolChoice = anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{},
		}
	}

	// Handle extended thinking
	thinkBudget := h.thinkBudget
	if turn.Reasoning != nil {
		switch turn.Reasoning.Effort {
		case "high":
			if thinkBudget == 0 {
				thinkBudget = 10000
			}
		case "low":
			thinkBudget = 0 // disable thinking
		}
	}
	if thinkBudget > 0 {
		params.Thinking = anthropic.ThinkingConfigParamOfEnabled(int64(thinkBudget))
		// Extended thinking requires higher max_tokens
		if params.MaxTokens < int64(thinkBudget)+4096 {
			params.MaxTokens = int64(thinkBudget) + 4096
		}
	}

	return params, nil
}

// streamState tracks state while translating a stream of Anthropic events.
type streamState struct {
	currentBlockType string // "text", "thinking", "tool_use"
	currentToolID    string
	currentToolName  string
	thinkingText     string
	toolArgsJSON     string
	inputTokens      int
	outputTokens     int
}

// translateEvent converts a raw Anthropic stream event to harness events.
func (h *Harness) translateEvent(event anthropic.MessageStreamEventUnion, state *streamState, emit func(harness.Event) error) error {
	switch e := event.AsAny().(type) {
	case anthropic.ContentBlockStartEvent:
		block := e.ContentBlock
		switch block.Type {
		case "text":
			state.currentBlockType = "text"
		case "thinking":
			state.currentBlockType = "thinking"
			state.thinkingText = ""
		case "tool_use":
			state.currentBlockType = "tool_use"
			toolBlock := block.AsToolUse()
			state.currentToolID = toolBlock.ID
			state.currentToolName = toolBlock.Name
			state.toolArgsJSON = ""
		}

	case anthropic.ContentBlockDeltaEvent:
		delta := e.Delta
		switch delta.Type {
		case "text_delta":
			textDelta := delta.AsTextDelta()
			return emit(harness.NewTextEvent(textDelta.Text))

		case "thinking_delta":
			thinkDelta := delta.AsThinkingDelta()
			state.thinkingText += thinkDelta.Thinking
			return emit(harness.NewThinkingEvent(thinkDelta.Thinking))

		case "input_json_delta":
			jsonDelta := delta.AsInputJSONDelta()
			state.toolArgsJSON += jsonDelta.PartialJSON
		}

	case anthropic.ContentBlockStopEvent:
		blockType := state.currentBlockType
		state.currentBlockType = ""
		switch blockType {
		case "tool_use":
			return emit(harness.NewToolCallEvent(
				state.currentToolID,
				state.currentToolName,
				state.toolArgsJSON,
			))
		case "thinking":
			// Complete thinking block already streamed as deltas
		}

	case anthropic.MessageStartEvent:
		if e.Message.Usage.InputTokens > 0 {
			state.inputTokens = int(e.Message.Usage.InputTokens)
		}

	case anthropic.MessageDeltaEvent:
		if e.Usage.OutputTokens > 0 {
			state.outputTokens = int(e.Usage.OutputTokens)
		}

	case anthropic.MessageStopEvent:
		if state.inputTokens > 0 || state.outputTokens > 0 {
			return emit(harness.NewUsageEvent(state.inputTokens, state.outputTokens))
		}
	}

	return nil
}
