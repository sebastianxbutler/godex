package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"godex/pkg/protocol"
)

func parseOpenAIInput(raw json.RawMessage) ([]OpenAIItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		return []OpenAIItem{{Type: "message", Role: "user", Content: text}}, nil
	case '[':
		var items []OpenAIItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		return items, nil
	case '{':
		var item OpenAIItem
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		return []OpenAIItem{item}, nil
	default:
		return nil, fmt.Errorf("invalid input shape")
	}
}

func extractText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := extractText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			return t
		}
		if t, ok := v["content"].(string); ok {
			return t
		}
		return ""
	case OpenAIRespContent:
		return v.Text
	case OpenAIChatMessage:
		return extractText(v.Content)
	}
	return ""
}

func buildSystemAndInput(sessionKey string, items []OpenAIItem, cache *Cache) ([]protocol.ResponseInputItem, string, error) {
	var systemParts []string
	var input []protocol.ResponseInputItem
	seenCalls := map[string]bool{}

	for _, item := range items {
		switch item.Type {
		case "function_call":
			if item.CallID == "" {
				return nil, "", errors.New("function_call missing call_id")
			}
			seenCalls[item.CallID] = true
			input = append(input, protocol.FunctionCallInput(item.Name, item.CallID, item.Arguments))
		case "function_call_output":
			if item.CallID == "" {
				return nil, "", errors.New("function_call_output missing call_id")
			}
			if !seenCalls[item.CallID] {
				if cache == nil {
					return nil, "", fmt.Errorf("missing function_call for %s", item.CallID)
				}
				call, ok := cache.GetToolCall(sessionKey, item.CallID)
				if !ok {
					return nil, "", fmt.Errorf("missing function_call for %s", item.CallID)
				}
				input = append(input, protocol.FunctionCallInput(call.Name, item.CallID, call.Arguments))
				seenCalls[item.CallID] = true
			}
			input = append(input, protocol.FunctionCallOutputInput(item.CallID, item.Output))
		default:
			role := item.Role
			if role == "" && item.Type == "message" {
				role = "user"
			}
			text := extractText(item.Content)
			if role == "system" || role == "developer" {
				if strings.TrimSpace(text) != "" {
					systemParts = append(systemParts, text)
				}
				continue
			}
			if role != "" && strings.TrimSpace(text) != "" {
				input = append(input, protocol.ResponseInputItem{
					Type: "message",
					Role: role,
					Content: []protocol.InputContentPart{{
						Type: "input_text",
						Text: text,
					}},
				})
			}
		}
	}

	return input, strings.Join(systemParts, "\n\n"), nil
}

func mergeInstructions(base string, system string) string {
	if strings.TrimSpace(base) == "" {
		return strings.TrimSpace(system)
	}
	if strings.TrimSpace(system) == "" {
		return strings.TrimSpace(base)
	}
	return strings.TrimSpace(base) + "\n\n" + strings.TrimSpace(system)
}

func mapTools(tools []OpenAITool) []protocol.ToolSpec {
	if len(tools) == 0 {
		return nil
	}
	out := make([]protocol.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		switch tool.Type {
		case "function":
			if tool.Function == nil {
				continue
			}
			out = append(out, protocol.ToolSpec{
				Type:        "function",
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
				Strict:      false,
			})
		case "web_search":
			out = append(out, protocol.ToolSpec{Type: "web_search", ExternalWebAccess: true})
		}
	}
	return out
}

func mapChatTools(tools []OpenAIChatTool) []protocol.ToolSpec {
	if len(tools) == 0 {
		return nil
	}
	out := make([]protocol.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		switch tool.Type {
		case "function":
			if tool.Function == nil {
				continue
			}
			out = append(out, protocol.ToolSpec{
				Type:        "function",
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
				Strict:      false,
			})
		case "web_search":
			out = append(out, protocol.ToolSpec{Type: "web_search", ExternalWebAccess: true})
		}
	}
	return out
}

func resolveToolChoice(choice any, tools []protocol.ToolSpec) (string, []protocol.ToolSpec) {
	if choice == nil {
		return "auto", tools
	}
	switch v := choice.(type) {
	case string:
		return v, tools
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				return "auto", filterToolsByName(tools, name)
			}
		}
		if name, ok := v["name"].(string); ok && name != "" {
			return "auto", filterToolsByName(tools, name)
		}
	}
	return "auto", tools
}

func filterToolsByName(tools []protocol.ToolSpec, name string) []protocol.ToolSpec {
	if name == "" || len(tools) == 0 {
		return tools
	}
	out := make([]protocol.ToolSpec, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == name {
			out = append(out, tool)
		}
	}
	return out
}
