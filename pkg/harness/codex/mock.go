package codex

import (
	"godex/pkg/harness"
)

// MockOption configures a Codex-specific mock harness.
type MockOption func(*harness.MockConfig)

// NewMock creates a mock harness pre-configured with Codex defaults.
func NewMock(opts ...MockOption) *harness.Mock {
	cfg := harness.MockConfig{
		HarnessName: "codex",
		Record:      true,
		Models: []harness.ModelInfo{
			{ID: "gpt-5.2-codex", Name: "GPT-5.2 Codex", Provider: "codex"},
		},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return harness.NewMock(cfg)
}

// WithApplyPatchFlow adds a scripted apply_patch tool call + result sequence.
// The harness will emit: preamble → tool_call(apply_patch) → text response.
func WithApplyPatchFlow(filename, patchContent string) MockOption {
	return func(cfg *harness.MockConfig) {
		patch := "*** Begin Patch\n*** Update File: " + filename + "\n" + patchContent + "\n*** End Patch"
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewPreambleEvent("Applying patch to " + filename),
			harness.NewToolCallEvent("call_patch_1", "apply_patch", patch),
		})
		// Second turn after tool result: text response
		cfg.Responses = append(cfg.Responses, []harness.Event{
			harness.NewTextEvent("Patch applied successfully to " + filename + "."),
			harness.NewUsageEvent(500, 100),
		})
	}
}

// WithPlanFlow adds a scripted plan update event sequence.
func WithPlanFlow(steps []harness.PlanEvent) MockOption {
	return func(cfg *harness.MockConfig) {
		events := make([]harness.Event, 0, len(steps)+1)
		events = append(events, harness.NewPreambleEvent("Creating plan..."))
		for _, step := range steps {
			events = append(events, harness.NewPlanEvent(step.Title, step.Status))
		}
		cfg.Responses = append(cfg.Responses, events)
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
