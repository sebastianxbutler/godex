package harness

import (
	"context"
	"time"
)

// RunToolLoop is the generic agentic tool loop shared by all harnesses.
// It calls StreamTurn, collects tool calls, executes them via handler,
// builds follow-up messages, and repeats until no tool calls remain or
// max turns is reached.
func RunToolLoop(
	ctx context.Context,
	streamTurn func(ctx context.Context, turn *Turn, onEvent func(Event) error) error,
	turn *Turn,
	handler ToolHandler,
	opts LoopOptions,
) (*TurnResult, error) {
	start := time.Now()
	combined := &TurnResult{}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}

	currentTurn := turn
	for i := 0; i < maxTurns; i++ {
		var pendingCalls []ToolCallEvent
		err := streamTurn(ctx, currentTurn, func(ev Event) error {
			combined.Events = append(combined.Events, ev)
			if opts.OnEvent != nil {
				if err := opts.OnEvent(ev); err != nil {
					return err
				}
			}
			switch ev.Kind {
			case EventText:
				if ev.Text != nil {
					combined.FinalText += ev.Text.Delta
					if ev.Text.Complete != "" {
						combined.FinalText = ev.Text.Complete
					}
				}
			case EventUsage:
				combined.Usage = ev.Usage
			case EventToolCall:
				if ev.ToolCall != nil {
					pendingCalls = append(pendingCalls, *ev.ToolCall)
					combined.ToolCalls = append(combined.ToolCalls, *ev.ToolCall)
				}
			}
			return nil
		})
		if err != nil {
			combined.Duration = time.Since(start)
			return combined, err
		}

		if len(pendingCalls) == 0 {
			break
		}

		// Execute tools and build follow-up messages
		followupMsgs := make([]Message, 0, len(pendingCalls)*2)
		for _, call := range pendingCalls {
			result, err := handler.Handle(ctx, call)
			if err != nil {
				combined.Duration = time.Since(start)
				return combined, err
			}
			if result != nil {
				ev := NewToolResultEvent(result.CallID, result.Output, result.IsError)
				combined.Events = append(combined.Events, ev)
			}
			followupMsgs = append(followupMsgs,
				Message{Role: "assistant", Content: call.Arguments, Name: call.Name, ToolID: call.CallID},
				Message{Role: "tool", Content: result.Output, ToolID: call.CallID},
			)
		}

		nextTurn := *currentTurn
		nextTurn.Messages = append(nextTurn.Messages, followupMsgs...)
		currentTurn = &nextTurn
	}

	combined.Duration = time.Since(start)
	return combined, nil
}
