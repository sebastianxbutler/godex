package openai

import (
	"godex/pkg/harness"
)

// MockOption configures an OpenAI-specific mock harness.
type MockOption func(*harness.MockConfig)

// NewMock creates a mock harness pre-configured with OpenAI-compatible defaults.
func NewMock(opts ...MockOption) *harness.Mock {
	cfg := harness.MockConfig{
		HarnessName: "openai",
		Record:      true,
		Models: []harness.ModelInfo{
			{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai"},
			{ID: "gpt-4o-mini", Name: "GPT-4o Mini", Provider: "openai"},
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return harness.NewMock(cfg)
}

// WithFunctionCallFlow adds a scripted function call + result sequence.
// First turn emits: tool call. Second turn emits: text response + usage.
func WithFunctionCallFlow(toolName, toolArgs, responseText string) MockOption {
	return func(cfg *harness.MockConfig) {
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewToolCallEvent("call_01", toolName, toolArgs),
		})
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewTextEvent(responseText),
			harness.NewUsageEvent(600, 120),
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

// WithMultipleFunctionCalls adds a flow with multiple parallel function calls
// followed by a text response.
func WithMultipleFunctionCalls(calls []harness.ToolCallEvent, responseText string) MockOption {
	return func(cfg *harness.MockConfig) {
		events := make([]harness.Event, 0, len(calls))
		for _, c := range calls {
			events = append(events, harness.NewToolCallEvent(c.CallID, c.Name, c.Arguments))
		}
		cfg.Responses = append(cfg.Responses, events)
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewTextEvent(responseText),
			harness.NewUsageEvent(800, 200),
		})
	}
}

// WithErrorResponse adds a scripted error response.
func WithErrorResponse(message string) MockOption {
	return func(cfg *harness.MockConfig) {
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewErrorEvent(message),
		})
	}
}
