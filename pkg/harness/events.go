package harness

import "time"

// EventKind identifies the type of structured event emitted during a turn.
type EventKind int

const (
	// EventText indicates model text output (streaming delta or complete).
	EventText EventKind = iota
	// EventThinking indicates a thinking/reasoning block (e.g. Claude extended thinking).
	EventThinking
	// EventToolCall indicates the model wants to call a tool.
	EventToolCall
	// EventToolResult indicates a tool execution result.
	EventToolResult
	// EventPlanUpdate indicates a plan step was added or completed.
	EventPlanUpdate
	// EventPreamble indicates a brief status update before an action.
	EventPreamble
	// EventUsage indicates token usage statistics.
	EventUsage
	// EventError indicates an error during the turn.
	EventError
	// EventDone indicates the turn is complete.
	EventDone
)

// String returns the human-readable name of the event kind.
func (k EventKind) String() string {
	switch k {
	case EventText:
		return "text"
	case EventThinking:
		return "thinking"
	case EventToolCall:
		return "tool_call"
	case EventToolResult:
		return "tool_result"
	case EventPlanUpdate:
		return "plan_update"
	case EventPreamble:
		return "preamble"
	case EventUsage:
		return "usage"
	case EventError:
		return "error"
	case EventDone:
		return "done"
	default:
		return "unknown"
	}
}

// Event is a structured agentic event emitted during a turn. Exactly one of
// the typed fields is populated, determined by Kind.
type Event struct {
	Kind      EventKind `json:"kind"`
	Timestamp time.Time `json:"timestamp"`

	Text       *TextEvent       `json:"text,omitempty"`
	Thinking   *ThinkingEvent   `json:"thinking,omitempty"`
	ToolCall   *ToolCallEvent   `json:"tool_call,omitempty"`
	ToolResult *ToolResultEvent `json:"tool_result,omitempty"`
	Plan       *PlanEvent       `json:"plan,omitempty"`
	Preamble   *PreambleEvent   `json:"preamble,omitempty"`
	Usage      *UsageEvent      `json:"usage,omitempty"`
	Error      *ErrorEvent      `json:"error,omitempty"`
}

// TextEvent carries a model text output delta or complete text.
type TextEvent struct {
	Delta    string `json:"delta,omitempty"`    // Incremental text chunk
	Complete string `json:"complete,omitempty"` // Full text (set on final)
}

// ThinkingEvent carries a thinking/reasoning block.
type ThinkingEvent struct {
	Delta    string `json:"delta,omitempty"`    // Incremental thinking text
	Complete string `json:"complete,omitempty"` // Full thinking block
	Summary  string `json:"summary,omitempty"`  // Optional summary
}

// ToolCallEvent carries a tool call request from the model.
type ToolCallEvent struct {
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded arguments
}

// ToolResultEvent carries the result of a tool execution.
type ToolResultEvent struct {
	CallID  string `json:"call_id"`
	Output  string `json:"output"`
	IsError bool   `json:"is_error,omitempty"`
}

// PlanEvent carries a plan update (e.g. Codex update_plan).
type PlanEvent struct {
	StepID    string `json:"step_id,omitempty"`
	Title     string `json:"title"`
	Status    string `json:"status"` // "pending", "in_progress", "done", "failed"
	StepIndex int    `json:"step_index,omitempty"`
}

// PreambleEvent carries a brief status message shown before an action.
type PreambleEvent struct {
	Text string `json:"text"`
}

// UsageEvent carries token usage statistics for a turn.
type UsageEvent struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

// ErrorEvent carries error information from the turn.
type ErrorEvent struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Retry   bool   `json:"retry,omitempty"` // Whether the caller should retry
}

// NewTextEvent creates a text event with the given delta.
func NewTextEvent(delta string) Event {
	return Event{
		Kind:      EventText,
		Timestamp: time.Now(),
		Text:      &TextEvent{Delta: delta},
	}
}

// NewThinkingEvent creates a thinking event with the given delta.
func NewThinkingEvent(delta string) Event {
	return Event{
		Kind:      EventThinking,
		Timestamp: time.Now(),
		Thinking:  &ThinkingEvent{Delta: delta},
	}
}

// NewToolCallEvent creates a tool call event.
func NewToolCallEvent(callID, name, args string) Event {
	return Event{
		Kind:      EventToolCall,
		Timestamp: time.Now(),
		ToolCall:  &ToolCallEvent{CallID: callID, Name: name, Arguments: args},
	}
}

// NewToolResultEvent creates a tool result event.
func NewToolResultEvent(callID, output string, isError bool) Event {
	return Event{
		Kind:      EventToolResult,
		Timestamp: time.Now(),
		ToolResult: &ToolResultEvent{CallID: callID, Output: output, IsError: isError},
	}
}

// NewPlanEvent creates a plan update event.
func NewPlanEvent(title, status string) Event {
	return Event{
		Kind:      EventPlanUpdate,
		Timestamp: time.Now(),
		Plan:      &PlanEvent{Title: title, Status: status},
	}
}

// NewPreambleEvent creates a preamble event.
func NewPreambleEvent(text string) Event {
	return Event{
		Kind:      EventPreamble,
		Timestamp: time.Now(),
		Preamble:  &PreambleEvent{Text: text},
	}
}

// NewUsageEvent creates a usage event.
func NewUsageEvent(input, output int) Event {
	return Event{
		Kind:      EventUsage,
		Timestamp: time.Now(),
		Usage:     &UsageEvent{InputTokens: input, OutputTokens: output, TotalTokens: input + output},
	}
}

// NewErrorEvent creates an error event.
func NewErrorEvent(message string) Event {
	return Event{
		Kind:      EventError,
		Timestamp: time.Now(),
		Error:     &ErrorEvent{Message: message},
	}
}

// NewDoneEvent creates a done event signaling turn completion.
func NewDoneEvent() Event {
	return Event{
		Kind:      EventDone,
		Timestamp: time.Now(),
	}
}
