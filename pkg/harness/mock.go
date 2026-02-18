package harness

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MockConfig configures a mock harness for deterministic testing.
type MockConfig struct {
	// HarnessName is the name returned by Name(). Defaults to "mock".
	HarnessName string

	// Responses contains scripted event sequences. Each call to StreamTurn
	// pops the next sequence from the front.
	Responses [][]Event

	// EventDelay simulates latency between emitted events.
	EventDelay time.Duration

	// FailAfterN causes StreamTurn to return FailErr after emitting N events.
	// 0 means no failure injection.
	FailAfterN int

	// FailErr is the error returned when FailAfterN is triggered.
	FailErr error

	// Record enables recording of all Turn requests for later assertion.
	Record bool

	// Models is the list returned by ListModels.
	Models []ModelInfo
}

// Mock implements Harness with scripted responses for deterministic testing
// without API calls.
type Mock struct {
	mu        sync.Mutex
	cfg       MockConfig
	callIndex int
	recorded  []*Turn
}

// NewMock creates a new mock harness with the given configuration.
func NewMock(cfg MockConfig) *Mock {
	if cfg.HarnessName == "" {
		cfg.HarnessName = "mock"
	}
	return &Mock{cfg: cfg}
}

// Name returns the mock harness name.
func (m *Mock) Name() string { return m.cfg.HarnessName }

// StreamTurn emits the next scripted event sequence via the callback.
func (m *Mock) StreamTurn(ctx context.Context, turn *Turn, onEvent func(Event) error) error {
	m.mu.Lock()
	if m.cfg.Record {
		m.recorded = append(m.recorded, turn)
	}
	idx := m.callIndex
	m.callIndex++
	m.mu.Unlock()

	if idx >= len(m.cfg.Responses) {
		return fmt.Errorf("mock: no more scripted responses (call %d, have %d)", idx, len(m.cfg.Responses))
	}

	events := m.cfg.Responses[idx]
	for i, ev := range events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if m.cfg.FailAfterN > 0 && i >= m.cfg.FailAfterN {
			if m.cfg.FailErr != nil {
				return m.cfg.FailErr
			}
			return fmt.Errorf("mock: injected failure after %d events", m.cfg.FailAfterN)
		}

		if m.cfg.EventDelay > 0 {
			time.Sleep(m.cfg.EventDelay)
		}

		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

// StreamAndCollect executes a turn and collects all events into a TurnResult.
func (m *Mock) StreamAndCollect(ctx context.Context, turn *Turn) (*TurnResult, error) {
	start := time.Now()
	result := &TurnResult{}
	err := m.StreamTurn(ctx, turn, func(ev Event) error {
		result.Events = append(result.Events, ev)
		switch ev.Kind {
		case EventText:
			if ev.Text != nil {
				result.FinalText += ev.Text.Delta
				if ev.Text.Complete != "" {
					result.FinalText = ev.Text.Complete
				}
			}
		case EventUsage:
			result.Usage = ev.Usage
		case EventToolCall:
			if ev.ToolCall != nil {
				result.ToolCalls = append(result.ToolCalls, *ev.ToolCall)
			}
		}
		return nil
	})
	result.Duration = time.Since(start)
	return result, err
}

// RunToolLoop executes a simplified tool loop using the mock's scripted responses.
// It calls StreamTurn for each cycle, executing tool calls via the handler.
func (m *Mock) RunToolLoop(ctx context.Context, turn *Turn, handler ToolHandler, opts LoopOptions) (*TurnResult, error) {
	start := time.Now()
	combined := &TurnResult{}
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10 // safety limit
	}

	for i := 0; i < maxTurns; i++ {
		var pendingCalls []ToolCallEvent
		err := m.StreamTurn(ctx, turn, func(ev Event) error {
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

		// Execute tool calls
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
		}
	}

	combined.Duration = time.Since(start)
	return combined, nil
}

// ListModels returns the configured mock models.
func (m *Mock) ListModels(_ context.Context) ([]ModelInfo, error) {
	return m.cfg.Models, nil
}

// ExpandAlias returns the alias unchanged (mock has no aliases).
func (m *Mock) ExpandAlias(alias string) string { return alias }

// MatchesModel returns false (mock does not match any model by default).
func (m *Mock) MatchesModel(model string) bool { return false }

// Recorded returns all Turn requests received when Record is true.
func (m *Mock) Recorded() []*Turn {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Turn, len(m.recorded))
	copy(out, m.recorded)
	return out
}

// CallCount returns how many times StreamTurn has been called.
func (m *Mock) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callIndex
}
