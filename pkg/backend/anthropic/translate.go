package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// translateRequest converts an internal ResponsesRequest to Anthropic MessageNewParams.
func translateRequest(req protocol.ResponsesRequest, defaultMaxTokens int) (anthropic.MessageNewParams, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(defaultMaxTokens),
	}

	// Extract system messages and build conversation
	var systemParts []anthropic.TextBlockParam
	var messages []anthropic.MessageParam

	for _, item := range req.Input {
		switch item.Type {
		case "message":
			content := extractTextContent(item)
			switch item.Role {
			case "system":
				systemParts = append(systemParts, anthropic.TextBlockParam{
					Text: content,
				})
			case "user":
				messages = append(messages, anthropic.NewUserMessage(
					anthropic.NewTextBlock(content),
				))
			case "assistant":
				messages = append(messages, anthropic.NewAssistantMessage(
					anthropic.NewTextBlock(content),
				))
			}

		case "function_call":
			// Assistant's tool use
			var inputMap map[string]any
			if item.Arguments != "" {
				json.Unmarshal([]byte(item.Arguments), &inputMap)
			}
			messages = append(messages, anthropic.NewAssistantMessage(
				anthropic.NewToolUseBlock(item.CallID, inputMap, item.Name),
			))

		case "function_call_output":
			// Tool result from user
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(item.CallID, item.Output, false),
			))
		}
	}

	// Add system prompt from instructions or extracted system messages
	if req.Instructions != "" {
		systemParts = append([]anthropic.TextBlockParam{{Text: req.Instructions}}, systemParts...)
	}
	if len(systemParts) > 0 {
		params.System = systemParts
	}

	// Set messages
	params.Messages = messages

	// Translate tools
	if len(req.Tools) > 0 {
		tools, err := translateTools(req.Tools)
		if err != nil {
			return params, err
		}
		params.Tools = tools
	}

	// Handle tool choice
	if req.ToolChoice != "" {
		params.ToolChoice = translateToolChoice(req.ToolChoice)
	}

	return params, nil
}

// extractTextContent extracts text from a ResponseInputItem.
func extractTextContent(item protocol.ResponseInputItem) string {
	if len(item.Content) > 0 {
		for _, part := range item.Content {
			if part.Type == "input_text" || part.Type == "text" {
				return part.Text
			}
		}
	}
	return ""
}

// translateTools converts internal tool specs to Anthropic format.
func translateTools(tools []protocol.ToolSpec) ([]anthropic.ToolUnionParam, error) {
	var result []anthropic.ToolUnionParam

	for _, t := range tools {
		if t.Type != "function" {
			continue
		}

		// Parse the JSON schema
		var schema anthropic.ToolInputSchemaParam
		if len(t.Parameters) > 0 {
			var schemaMap map[string]any
			if err := json.Unmarshal(t.Parameters, &schemaMap); err != nil {
				return nil, fmt.Errorf("parse tool schema for %s: %w", t.Name, err)
			}
			// The SDK expects properties directly
			if props, ok := schemaMap["properties"].(map[string]any); ok {
				schema.Properties = props
			}
			if req, ok := schemaMap["required"].([]any); ok {
				for _, r := range req {
					if s, ok := r.(string); ok {
						schema.Required = append(schema.Required, s)
					}
				}
			}
		}

		result = append(result, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: schema,
			},
		})
	}

	return result, nil
}

// translateToolChoice converts tool choice string to Anthropic format.
func translateToolChoice(choice string) anthropic.ToolChoiceUnionParam {
	switch choice {
	case "none":
		return anthropic.ToolChoiceUnionParam{
			OfNone: &anthropic.ToolChoiceNoneParam{},
		}
	case "auto":
		return anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{},
		}
	case "required":
		return anthropic.ToolChoiceUnionParam{
			OfAny: &anthropic.ToolChoiceAnyParam{},
		}
	default:
		// If it's a specific function name
		return anthropic.ToolChoiceUnionParam{
			OfTool: &anthropic.ToolChoiceToolParam{
				Name: choice,
			},
		}
	}
}

// translateStreamEvent converts Anthropic stream events to internal SSE events.
func translateStreamEvent(event anthropic.MessageStreamEventUnion, currentItemID, currentToolID *string) []sse.Event {
	var events []sse.Event

	switch e := event.AsAny().(type) {
	case anthropic.ContentBlockStartEvent:
		block := e.ContentBlock
		switch block.Type {
		case "text":
			// Text block started - generate an item ID
			*currentItemID = fmt.Sprintf("item_%d", e.Index)
		case "tool_use":
			// Tool use block started
			toolBlock := block.AsToolUse()
			*currentItemID = fmt.Sprintf("item_%d", e.Index)
			*currentToolID = toolBlock.ID
			events = append(events, sse.Event{
				Value: protocol.StreamEvent{
					Type: "response.output_item.added",
					Item: &protocol.OutputItem{
						ID:     *currentItemID,
						Type:   "function_call",
						Name:   toolBlock.Name,
						CallID: toolBlock.ID,
					},
				},
			})
		}

	case anthropic.ContentBlockDeltaEvent:
		delta := e.Delta
		switch delta.Type {
		case "text_delta":
			textDelta := delta.AsTextDelta()
			events = append(events, sse.Event{
				Value: protocol.StreamEvent{
					Type:  "response.output_text.delta",
					Delta: textDelta.Text,
				},
			})
		case "input_json_delta":
			jsonDelta := delta.AsInputJSONDelta()
			events = append(events, sse.Event{
				Value: protocol.StreamEvent{
					Type:   "response.function_call_arguments.delta",
					ItemID: *currentItemID,
					Delta:  jsonDelta.PartialJSON,
					Item: &protocol.OutputItem{
						CallID: *currentToolID,
					},
				},
			})
		}

	case anthropic.MessageStopEvent:
		events = append(events, sse.Event{
			Value: protocol.StreamEvent{
				Type: "response.done",
			},
		})

	case anthropic.MessageDeltaEvent:
		// Contains usage info
		if e.Usage.OutputTokens > 0 {
			events = append(events, sse.Event{
				Value: protocol.StreamEvent{
					Type: "response.done",
					Response: &protocol.ResponseRef{
						Usage: &protocol.Usage{
							OutputTokens: int(e.Usage.OutputTokens),
						},
					},
				},
			})
		}
	}

	return events
}
