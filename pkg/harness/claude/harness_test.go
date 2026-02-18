package claude

import (
	"context"
	"testing"

	"godex/pkg/harness"
)

func TestNew_Defaults(t *testing.T) {
	h := New(Config{})
	if h.Name() != "claude" {
		t.Errorf("expected 'claude', got %q", h.Name())
	}
	if h.defaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("expected default model, got %q", h.defaultModel)
	}
	if h.maxTokens != 16384 {
		t.Errorf("expected 16384, got %d", h.maxTokens)
	}
}

func TestNew_CustomConfig(t *testing.T) {
	h := New(Config{
		DefaultModel:     "claude-opus-4-20250514",
		DefaultMaxTokens: 8192,
		ThinkingBudget:   5000,
	})
	if h.defaultModel != "claude-opus-4-20250514" {
		t.Errorf("unexpected model: %s", h.defaultModel)
	}
	if h.maxTokens != 8192 {
		t.Errorf("unexpected maxTokens: %d", h.maxTokens)
	}
	if h.thinkBudget != 5000 {
		t.Errorf("unexpected thinkBudget: %d", h.thinkBudget)
	}
}

func TestBuildRequest_Basic(t *testing.T) {
	h := New(Config{DefaultMaxTokens: 8192})
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if string(params.Model) != "claude-sonnet-4-20250514" {
		t.Errorf("unexpected model: %s", params.Model)
	}
	if params.MaxTokens != 8192 {
		t.Errorf("unexpected max_tokens: %d", params.MaxTokens)
	}
	if len(params.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(params.Messages))
	}
	// System prompt should be set
	if len(params.System) == 0 {
		t.Error("expected system prompt")
	}
}

func TestBuildRequest_ModelOverride(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{Model: "claude-opus-4-20250514"}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if string(params.Model) != "claude-opus-4-20250514" {
		t.Errorf("expected claude-opus-4-20250514, got %s", params.Model)
	}
}

func TestBuildRequest_WithThinking(t *testing.T) {
	h := New(Config{ThinkingBudget: 10000, DefaultMaxTokens: 8192})
	turn := &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "think hard"}},
	}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	budget := params.Thinking.GetBudgetTokens()
	if budget == nil || *budget != 10000 {
		t.Errorf("expected budget 10000, got %v", budget)
	}
	// MaxTokens should be bumped to accommodate thinking
	if params.MaxTokens < 14096 {
		t.Errorf("expected max_tokens >= 14096, got %d", params.MaxTokens)
	}
}

func TestBuildRequest_ReasoningLowDisablesThinking(t *testing.T) {
	h := New(Config{ThinkingBudget: 10000})
	turn := &harness.Turn{
		Messages:  []harness.Message{{Role: "user", Content: "quick"}},
		Reasoning: &harness.ReasoningConfig{Effort: "low"},
	}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	budget := params.Thinking.GetBudgetTokens()
	if budget != nil {
		t.Errorf("expected thinking disabled for low effort, got budget %d", *budget)
	}
}

func TestBuildRequest_ReasoningHighEnablesThinking(t *testing.T) {
	h := New(Config{}) // No default thinking budget
	turn := &harness.Turn{
		Messages:  []harness.Message{{Role: "user", Content: "think"}},
		Reasoning: &harness.ReasoningConfig{Effort: "high"},
	}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	budget := params.Thinking.GetBudgetTokens()
	if budget == nil || *budget != 10000 {
		t.Errorf("expected default budget 10000, got %v", budget)
	}
}

func TestBuildRequest_WithTools(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{
		Messages: []harness.Message{{Role: "user", Content: "do it"}},
		Tools: []harness.ToolSpec{
			{
				Name:        "shell",
				Description: "Run a shell command",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string"},
					},
					"required": []any{"command"},
				},
			},
		},
	}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(params.Tools))
	}
	if params.Tools[0].OfTool.Name != "shell" {
		t.Errorf("expected tool name 'shell', got %q", params.Tools[0].OfTool.Name)
	}
}

func TestBuildRequest_MessageTypes(t *testing.T) {
	h := New(Config{})
	turn := &harness.Turn{
		Messages: []harness.Message{
			{Role: "user", Content: "do it"},
			{Role: "assistant", Content: `{"command":"ls"}`, Name: "shell", ToolID: "toolu_01"},
			{Role: "tool", Content: "file1.go\nfile2.go", ToolID: "toolu_01"},
			{Role: "assistant", Content: "Done!"},
		},
	}
	params, err := h.buildRequest(turn)
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(params.Messages))
	}
}

// Mock tests

func TestNewMock_Defaults(t *testing.T) {
	mock := NewMock()
	if mock.Name() != "claude" {
		t.Errorf("expected 'claude', got %q", mock.Name())
	}
	models, err := mock.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestWithThinkingFlow(t *testing.T) {
	mock := NewMock(WithThinkingFlow("Let me think...", "Here's the answer."))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test", Messages: []harness.Message{{Role: "user", Content: "think"}}}

	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventThinking {
		t.Errorf("expected thinking, got %s", events[0].Kind)
	}
	if events[0].Thinking.Delta != "Let me think..." {
		t.Errorf("unexpected thinking text: %q", events[0].Thinking.Delta)
	}
	if events[1].Kind != harness.EventText {
		t.Errorf("expected text, got %s", events[1].Kind)
	}
	if events[2].Kind != harness.EventUsage {
		t.Errorf("expected usage, got %s", events[2].Kind)
	}
}

func TestWithToolUseFlow(t *testing.T) {
	mock := NewMock(WithToolUseFlow("shell", `{"command":"ls"}`, "Found 3 files."))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test"}

	// First turn: tool call
	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Kind != harness.EventToolCall {
		t.Errorf("expected tool_call, got %s", events[0].Kind)
	}
	if events[0].ToolCall.Name != "shell" {
		t.Errorf("expected 'shell', got %q", events[0].ToolCall.Name)
	}

	// Second turn: text response
	events = events[:0]
	err = mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventText {
		t.Errorf("expected text, got %s", events[0].Kind)
	}
}

func TestWithTextResponse(t *testing.T) {
	mock := NewMock(WithTextResponse("Hello from Claude!"))

	ctx := context.Background()
	result, err := mock.StreamAndCollect(ctx, &harness.Turn{Model: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "Hello from Claude!" {
		t.Errorf("expected 'Hello from Claude!', got %q", result.FinalText)
	}
	if result.Usage == nil {
		t.Error("expected usage")
	}
}

func TestWithThinkingAndToolUse(t *testing.T) {
	mock := NewMock(WithThinkingAndToolUse(
		"I should run the tests first",
		"shell", `{"command":"go test ./..."}`,
		"All tests pass!",
	))

	ctx := context.Background()
	turn := &harness.Turn{Model: "test"}

	// First turn: thinking + tool call
	var events []harness.Event
	err := mock.StreamTurn(ctx, turn, func(ev harness.Event) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Kind != harness.EventThinking {
		t.Errorf("expected thinking, got %s", events[0].Kind)
	}
	if events[1].Kind != harness.EventToolCall {
		t.Errorf("expected tool_call, got %s", events[1].Kind)
	}
}

func TestMock_RecordsTurns(t *testing.T) {
	mock := NewMock(WithTextResponse("ok"))

	turn := &harness.Turn{
		Model:    "test",
		Messages: []harness.Message{{Role: "user", Content: "hi"}},
	}
	mock.StreamAndCollect(context.Background(), turn)

	recorded := mock.Recorded()
	if len(recorded) != 1 {
		t.Fatalf("expected 1 recorded turn, got %d", len(recorded))
	}
	if recorded[0].Model != "test" {
		t.Errorf("unexpected model: %s", recorded[0].Model)
	}
}
