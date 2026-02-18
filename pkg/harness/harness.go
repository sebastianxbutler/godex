// Package harness defines the core interface for provider-agnostic agentic
// runtimes. Each LLM provider (Codex, Claude, OpenAI) implements the Harness
// interface to handle prompt injection, streaming, tool execution, and
// structured event emission.
package harness

import (
	"context"
	"time"
)

// Harness is the core interface that all provider harnesses implement.
// It handles the full agentic loop: prompt injection, streaming, tool
// execution, and structured event emission.
type Harness interface {
	// Name returns the harness identifier (e.g. "codex", "claude", "openai").
	Name() string

	// StreamTurn executes a single agentic turn and streams structured events
	// via the onEvent callback. The callback may return an error to abort the
	// stream early.
	StreamTurn(ctx context.Context, turn *Turn, onEvent func(Event) error) error

	// StreamAndCollect executes a turn and returns the collected result.
	StreamAndCollect(ctx context.Context, turn *Turn) (*TurnResult, error)

	// RunToolLoop executes the full agentic loop: model call → tool execution →
	// follow-up → ... until the model produces a final response or max steps.
	RunToolLoop(ctx context.Context, turn *Turn, handler ToolHandler, opts LoopOptions) (*TurnResult, error)

	// ListModels returns available models for this harness.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// ExpandAlias expands a model alias to its full name.
	// Returns the input unchanged if no alias matches.
	ExpandAlias(alias string) string

	// MatchesModel returns true if this harness handles the given model.
	MatchesModel(model string) bool
}

// Message represents a single message in the conversation history.
type Message struct {
	Role    string `json:"role"`    // "user", "assistant", "system", "tool"
	Content string `json:"content"` // Text content
	Name    string `json:"name,omitempty"`
	ToolID  string `json:"tool_id,omitempty"` // For tool result messages
}

// ToolSpec describes a tool available to the model.
type ToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Parameters is the JSON Schema for the tool's input.
	Parameters map[string]any `json:"parameters,omitempty"`
}

// EnvironmentCtx describes the execution environment for prompt injection.
type EnvironmentCtx struct {
	WorkingDir  string            `json:"working_dir,omitempty"`
	Shell       string            `json:"shell,omitempty"`
	Platform    string            `json:"platform,omitempty"`
	Sandbox     string            `json:"sandbox,omitempty"` // "full", "network-off", "none"
	CustomAttrs map[string]string `json:"custom_attrs,omitempty"`
}

// PermissionsCtx configures the approval policy for tool execution.
type PermissionsCtx struct {
	Mode          string   `json:"mode"`                    // "full-auto", "suggest", "ask-every-time"
	AllowedTools  []string `json:"allowed_tools,omitempty"` // Auto-approved tool names
	SandboxPolicy string   `json:"sandbox_policy,omitempty"`
}

// ReasoningConfig controls model reasoning/thinking behavior.
type ReasoningConfig struct {
	Effort    string `json:"effort,omitempty"`    // "low", "medium", "high"
	Summaries bool   `json:"summaries,omitempty"` // Include reasoning summaries
}

// UserContext holds user-provided context files like AGENTS.md.
type UserContext struct {
	AgentsMD      string `json:"agents_md,omitempty"`
	SoulMD        string `json:"soul_md,omitempty"`
	Collaboration string `json:"collaboration,omitempty"` // "default", "plan"
}

// Turn represents a single agentic turn request.
type Turn struct {
	Model        string            `json:"model"`
	Instructions string            `json:"instructions,omitempty"`
	Messages     []Message         `json:"messages"`
	Tools        []ToolSpec        `json:"tools,omitempty"`
	Environment  *EnvironmentCtx   `json:"environment,omitempty"`
	Permissions  *PermissionsCtx   `json:"permissions,omitempty"`
	Reasoning    *ReasoningConfig  `json:"reasoning,omitempty"`
	UserContext  *UserContext       `json:"user_context,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
}

// TurnResult is the collected output of a completed turn.
type TurnResult struct {
	// Events is the full sequence of events emitted during the turn.
	Events []Event `json:"events"`
	// FinalText is the concatenated text output from the model.
	FinalText string `json:"final_text"`
	// Usage contains token usage information if available.
	Usage *UsageEvent `json:"usage,omitempty"`
	// Duration is the wall-clock time for the turn.
	Duration time.Duration `json:"duration"`
	// ToolCalls contains all tool calls made during this turn.
	ToolCalls []ToolCallEvent `json:"tool_calls,omitempty"`
}

// ToolHandler executes tool calls on behalf of the harness.
type ToolHandler interface {
	// Handle executes a tool call and returns the result.
	Handle(ctx context.Context, call ToolCallEvent) (*ToolResultEvent, error)
	// Available returns the tool specs this handler provides.
	Available() []ToolSpec
}

// LoopOptions configures the agentic tool loop.
type LoopOptions struct {
	// MaxTurns limits the number of model→tool→model cycles. 0 = unlimited.
	MaxTurns int `json:"max_turns,omitempty"`
	// MaxTokens limits total token usage across all turns.
	MaxTokens int `json:"max_tokens,omitempty"`
	// OnEvent is called for each event during the loop.
	OnEvent func(Event) error `json:"-"`
}

// ModelInfo describes an available model.
type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Provider string `json:"provider,omitempty"`
}
