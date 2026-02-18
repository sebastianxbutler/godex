package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// LoggerConfig configures the event logging wrapper.
type LoggerConfig struct {
	// Dir is the output directory. One .jsonl file per turn.
	Dir string

	// Redact sensitive fields (instructions, API keys) from logged requests.
	Redact bool

	// OnEvent is a real-time event hook for live debugging.
	OnEvent func(Event)
}

// LogEntry is a single line in the JSONL log file.
type LogEntry struct {
	Timestamp string     `json:"ts"`
	Type      string     `json:"type"`                 // "turn_start", "event", "turn_end"
	Turn      *Turn      `json:"turn,omitempty"`        // For turn_start
	Kind      string     `json:"kind,omitempty"`        // Event kind string
	Event     *Event     `json:"event,omitempty"`       // The raw event
	LatencyMs int64      `json:"latency_ms,omitempty"`  // Ms since last event
	TotalMs   int64      `json:"total_ms,omitempty"`    // For turn_end
	Usage     *UsageEvent `json:"usage,omitempty"`       // For turn_end
	Error     string     `json:"error,omitempty"`       // For turn_end on error
}

// loggerHarness wraps a Harness with JSONL event logging.
type loggerHarness struct {
	inner    Harness
	cfg      LoggerConfig
	turnSeq  atomic.Int64
}

// WithLogger wraps any Harness with event logging that records the full
// turn lifecycle to JSONL files with timestamps and latency tracking.
func WithLogger(h Harness, cfg LoggerConfig) Harness {
	return &loggerHarness{inner: h, cfg: cfg}
}

func (l *loggerHarness) Name() string { return l.inner.Name() }

func (l *loggerHarness) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return l.inner.ListModels(ctx)
}

func (l *loggerHarness) StreamTurn(ctx context.Context, turn *Turn, onEvent func(Event) error) error {
	seq := l.turnSeq.Add(1)
	w, err := l.openLog(seq)
	if err != nil {
		// Fall through without logging if we can't open the file.
		return l.inner.StreamTurn(ctx, turn, onEvent)
	}
	defer w.Close()

	logTurn := turn
	if l.cfg.Redact {
		logTurn = l.redactTurn(turn)
	}

	turnStart := time.Now()
	l.writeLine(w, LogEntry{
		Timestamp: turnStart.Format(time.RFC3339Nano),
		Type:      "turn_start",
		Turn:      logTurn,
	})

	lastEvent := turnStart
	var lastUsage *UsageEvent

	streamErr := l.inner.StreamTurn(ctx, turn, func(ev Event) error {
		now := time.Now()
		latency := now.Sub(lastEvent).Milliseconds()
		lastEvent = now

		if ev.Kind == EventUsage {
			lastUsage = ev.Usage
		}

		l.writeLine(w, LogEntry{
			Timestamp: now.Format(time.RFC3339Nano),
			Type:      "event",
			Kind:      ev.Kind.String(),
			Event:     &ev,
			LatencyMs: latency,
		})

		if l.cfg.OnEvent != nil {
			l.cfg.OnEvent(ev)
		}

		return onEvent(ev)
	})

	endEntry := LogEntry{
		Timestamp: time.Now().Format(time.RFC3339Nano),
		Type:      "turn_end",
		TotalMs:   time.Since(turnStart).Milliseconds(),
		Usage:     lastUsage,
	}
	if streamErr != nil {
		endEntry.Error = streamErr.Error()
	}
	l.writeLine(w, endEntry)

	return streamErr
}

func (l *loggerHarness) StreamAndCollect(ctx context.Context, turn *Turn) (*TurnResult, error) {
	start := time.Now()
	result := &TurnResult{}
	err := l.StreamTurn(ctx, turn, func(ev Event) error {
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

func (l *loggerHarness) RunToolLoop(ctx context.Context, turn *Turn, handler ToolHandler, opts LoopOptions) (*TurnResult, error) {
	return l.inner.RunToolLoop(ctx, turn, handler, opts)
}

func (l *loggerHarness) openLog(seq int64) (*os.File, error) {
	if err := os.MkdirAll(l.cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	name := fmt.Sprintf("turn-%s-%03d.jsonl",
		time.Now().Format("2006-01-02"),
		seq,
	)
	return os.Create(filepath.Join(l.cfg.Dir, name))
}

func (l *loggerHarness) writeLine(f *os.File, entry LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f.Write(data)
	f.Write([]byte("\n"))
}

func (l *loggerHarness) redactTurn(turn *Turn) *Turn {
	cp := *turn
	if cp.Instructions != "" {
		cp.Instructions = redactString(cp.Instructions)
	}
	if cp.UserContext != nil {
		uc := *cp.UserContext
		if uc.AgentsMD != "" {
			uc.AgentsMD = "[REDACTED]"
		}
		if uc.SoulMD != "" {
			uc.SoulMD = "[REDACTED]"
		}
		cp.UserContext = &uc
	}
	return &cp
}

// redactString keeps the first 20 chars and replaces the rest.
func redactString(s string) string {
	if len(s) <= 20 {
		return s
	}
	return s[:20] + strings.Repeat("*", 10) + fmt.Sprintf(" [%d chars redacted]", len(s)-20)
}
