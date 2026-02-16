package anthropic

import (
	"encoding/json"
	"testing"

	"godex/pkg/protocol"
)

func TestTranslateRequest(t *testing.T) {
	req := protocol.ResponsesRequest{
		Model:        "claude-sonnet-4-5-20250929",
		Instructions: "You are a helpful assistant.",
		Input: []protocol.ResponseInputItem{
			{
				Type: "message",
				Role: "user",
				Content: []protocol.InputContentPart{
					{Type: "input_text", Text: "Hello!"},
				},
			},
		},
	}

	params, err := translateRequest(req, 4096)
	if err != nil {
		t.Fatalf("translateRequest failed: %v", err)
	}

	if string(params.Model) != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected claude-sonnet-4-5-20250929, got %s", params.Model)
	}

	if params.MaxTokens != 4096 {
		t.Errorf("expected 4096, got %d", params.MaxTokens)
	}

	if len(params.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(params.System))
	}
	if params.System[0].Text != "You are a helpful assistant." {
		t.Errorf("unexpected system text: %s", params.System[0].Text)
	}

	if len(params.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(params.Messages))
	}
}

func TestTranslateTools(t *testing.T) {
	toolParams := json.RawMessage(`{
		"type": "object",
		"properties": {
			"a": {"type": "integer"},
			"b": {"type": "integer"}
		},
		"required": ["a", "b"]
	}`)

	tools := []protocol.ToolSpec{
		{
			Type:        "function",
			Name:        "add",
			Description: "Add two numbers",
			Parameters:  toolParams,
		},
	}

	result, err := translateTools(tools)
	if err != nil {
		t.Fatalf("translateTools failed: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result))
	}

	tool := result[0].OfTool
	if tool == nil {
		t.Fatal("expected OfTool to be set")
	}
	if tool.Name != "add" {
		t.Errorf("expected add, got %s", tool.Name)
	}
}

func TestTranslateToolChoice(t *testing.T) {
	tests := []struct {
		input string
		check func(t *testing.T, tc interface{})
	}{
		{
			input: "auto",
			check: func(t *testing.T, tc interface{}) {
				// Just verify it doesn't panic and returns something
				if tc == nil {
					t.Error("expected non-nil tool choice")
				}
			},
		},
		{
			input: "none",
			check: func(t *testing.T, tc interface{}) {
				if tc == nil {
					t.Error("expected non-nil tool choice")
				}
			},
		},
		{
			input: "required",
			check: func(t *testing.T, tc interface{}) {
				if tc == nil {
					t.Error("expected non-nil tool choice")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := translateToolChoice(tt.input)
			tt.check(t, result)
		})
	}
}

func TestTranslateRequestWithToolCalls(t *testing.T) {
	// Test translating a conversation that includes function calls and results
	req := protocol.ResponsesRequest{
		Model: "claude-sonnet-4-5-20250929",
		Input: []protocol.ResponseInputItem{
			{
				Type: "message",
				Role: "user",
				Content: []protocol.InputContentPart{
					{Type: "input_text", Text: "What is 2+2?"},
				},
			},
			{
				Type:      "function_call",
				Name:      "calculator",
				CallID:    "call_123",
				Arguments: `{"expression": "2+2"}`,
			},
			{
				Type:   "function_call_output",
				CallID: "call_123",
				Output: "4",
			},
		},
	}

	params, err := translateRequest(req, 4096)
	if err != nil {
		t.Fatalf("translateRequest failed: %v", err)
	}

	// Should have 3 messages: user, assistant (tool_use), user (tool_result)
	if len(params.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(params.Messages))
	}
}
