package claude

import (
	"godex/pkg/harness"
)

// MockOption configures a Claude-specific mock harness.
type MockOption func(*harness.MockConfig)

// NewMock creates a mock harness pre-configured with Claude defaults.
func NewMock(opts ...MockOption) *harness.Mock {
	cfg := harness.MockConfig{
		HarnessName: "claude",
		Record:      true,
		Models: []harness.ModelInfo{
			{ID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: "claude"},
			{ID: "claude-opus-4-20250514", Name: "Claude Opus 4", Provider: "claude"},
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return harness.NewMock(cfg)
}

// WithThinkingFlow adds a scripted extended thinking + text response sequence.
// Simulates: thinking deltas → text response → usage.
func WithThinkingFlow(thinkingText, responseText string) MockOption {
	return func(cfg *harness.MockConfig) {
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewThinkingEvent(thinkingText),
			harness.NewTextEvent(responseText),
			harness.NewUsageEvent(500, 200),
		})
	}
}

// WithToolUseFlow adds a scripted tool_use block + result sequence.
// First turn emits: tool call. Second turn emits: text response + usage.
func WithToolUseFlow(toolName, toolArgs, responseText string) MockOption {
	return func(cfg *harness.MockConfig) {
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewToolCallEvent("toolu_01", toolName, toolArgs),
		})
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewTextEvent(responseText),
			harness.NewUsageEvent(800, 150),
		})
	}
}

// WithTextResponse adds a simple text response sequence.
func WithTextResponse(text string) MockOption {
	return func(cfg *harness.MockConfig) {
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewTextEvent(text),
			harness.NewUsageEvent(200, 50),
		})
	}
}

// WithThinkingAndToolUse adds a scripted flow: thinking → tool call → (next turn) text response.
func WithThinkingAndToolUse(thinkingText, toolName, toolArgs, responseText string) MockOption {
	return func(cfg *harness.MockConfig) {
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewThinkingEvent(thinkingText),
			harness.NewToolCallEvent("toolu_01", toolName, toolArgs),
		})
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewTextEvent(responseText),
			harness.NewUsageEvent(1000, 300),
		})
	}
}
