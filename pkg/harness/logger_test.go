package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWithLoggerStreamTurn(t *testing.T) {
	dir := t.TempDir()
	events := []Event{NewTextEvent("hello"), NewUsageEvent(10, 5), NewDoneEvent()}
	inner := NewMock(MockConfig{Responses: [][]Event{events}})
	logged := WithLogger(inner, LoggerConfig{Dir: dir})

	if logged.Name() != "mock" {
		t.Errorf("expected 'mock', got %q", logged.Name())
	}

	var got []Event
	err := logged.StreamTurn(context.Background(), &Turn{Model: "test"}, func(ev Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}

	// Verify JSONL file was written
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("expected 1 log file, got %d", len(files))
	}

	data, _ := os.ReadFile(files[0])
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	// turn_start + 3 events + turn_end = 5 lines
	if len(lines) != 5 {
		t.Errorf("expected 5 log lines, got %d", len(lines))
	}

	// Verify turn_start
	var startEntry LogEntry
	json.Unmarshal([]byte(lines[0]), &startEntry)
	if startEntry.Type != "turn_start" {
		t.Errorf("first line should be turn_start, got %q", startEntry.Type)
	}

	// Verify turn_end has total_ms
	var endEntry LogEntry
	json.Unmarshal([]byte(lines[4]), &endEntry)
	if endEntry.Type != "turn_end" {
		t.Errorf("last line should be turn_end, got %q", endEntry.Type)
	}
	if endEntry.Usage == nil || endEntry.Usage.InputTokens != 10 {
		t.Error("turn_end should have usage")
	}
}

func TestWithLoggerRedaction(t *testing.T) {
	dir := t.TempDir()
	events := []Event{NewDoneEvent()}
	inner := NewMock(MockConfig{Responses: [][]Event{events}})
	logged := WithLogger(inner, LoggerConfig{Dir: dir, Redact: true})

	turn := &Turn{
		Model:        "test",
		Instructions: "This is a very long instruction that should be partially redacted for security reasons",
		UserContext:   &UserContext{AgentsMD: "secret agents content", SoulMD: "soul content"},
	}
	logged.StreamTurn(context.Background(), turn, func(Event) error { return nil })

	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	content := string(data)

	if strings.Contains(content, "should be partially redacted") {
		t.Error("instructions should be redacted")
	}
	if strings.Contains(content, "secret agents content") {
		t.Error("AgentsMD should be redacted")
	}
}

func TestWithLoggerOnEvent(t *testing.T) {
	dir := t.TempDir()
	events := []Event{NewTextEvent("hi"), NewDoneEvent()}
	inner := NewMock(MockConfig{Responses: [][]Event{events}})

	var hookCalled int
	logged := WithLogger(inner, LoggerConfig{
		Dir:     dir,
		OnEvent: func(Event) { hookCalled++ },
	})

	logged.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	if hookCalled != 2 {
		t.Errorf("expected OnEvent called 2 times, got %d", hookCalled)
	}
}

func TestWithLoggerStreamAndCollect(t *testing.T) {
	dir := t.TempDir()
	events := []Event{NewTextEvent("result"), NewDoneEvent()}
	inner := NewMock(MockConfig{Responses: [][]Event{events}})
	logged := WithLogger(inner, LoggerConfig{Dir: dir})

	result, err := logged.StreamAndCollect(context.Background(), &Turn{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "result" {
		t.Errorf("expected 'result', got %q", result.FinalText)
	}
}

func TestWithLoggerListModels(t *testing.T) {
	models := []ModelInfo{{ID: "m1"}}
	inner := NewMock(MockConfig{Models: models})
	logged := WithLogger(inner, LoggerConfig{Dir: t.TempDir()})
	got, err := logged.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Error("models mismatch")
	}
}

func TestWithLoggerStreamError(t *testing.T) {
	dir := t.TempDir()
	events := []Event{NewTextEvent("a"), NewTextEvent("b")}
	inner := NewMock(MockConfig{
		Responses:  [][]Event{events},
		FailAfterN: 1,
	})
	logged := WithLogger(inner, LoggerConfig{Dir: dir})

	err := logged.StreamTurn(context.Background(), &Turn{}, func(Event) error { return nil })
	if err == nil {
		t.Fatal("expected error")
	}

	// Verify error is in turn_end
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), "injected failure") {
		t.Error("turn_end should contain error")
	}
}

func TestWithLoggerRunToolLoop(t *testing.T) {
	dir := t.TempDir()
	events := []Event{NewTextEvent("result"), NewDoneEvent()}
	inner := NewMock(MockConfig{Responses: [][]Event{events}})
	logged := WithLogger(inner, LoggerConfig{Dir: dir})

	handler := &testHandler{results: map[string]*ToolResultEvent{}}
	result, err := logged.RunToolLoop(context.Background(), &Turn{}, handler, LoopOptions{MaxTurns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "result" {
		t.Errorf("expected 'result', got %q", result.FinalText)
	}
}

func TestWithLoggerStreamAndCollectToolCalls(t *testing.T) {
	dir := t.TempDir()
	events := []Event{
		NewTextEvent("hi"),
		NewToolCallEvent("c1", "shell", "{}"),
		{Kind: EventText, Timestamp: events_now(), Text: &TextEvent{Complete: "final"}},
		NewUsageEvent(50, 25),
		NewDoneEvent(),
	}
	inner := NewMock(MockConfig{Responses: [][]Event{events}})
	logged := WithLogger(inner, LoggerConfig{Dir: dir})

	result, err := logged.StreamAndCollect(context.Background(), &Turn{})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalText != "final" {
		t.Errorf("expected 'final', got %q", result.FinalText)
	}
	if len(result.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(result.ToolCalls))
	}
	if result.Usage == nil || result.Usage.InputTokens != 50 {
		t.Error("usage not collected")
	}
}

func events_now() time.Time { return time.Now() }

func TestWithLoggerRedactNoUserContext(t *testing.T) {
	dir := t.TempDir()
	inner := NewMock(MockConfig{Responses: [][]Event{{NewDoneEvent()}}})
	logged := WithLogger(inner, LoggerConfig{Dir: dir, Redact: true})

	turn := &Turn{Instructions: "short"}
	logged.StreamTurn(context.Background(), turn, func(Event) error { return nil })

	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	data, _ := os.ReadFile(files[0])
	// Short instructions should not be redacted
	if !strings.Contains(string(data), "short") {
		t.Error("short instructions should be preserved")
	}
}

func TestRedactString(t *testing.T) {
	short := "short"
	if redactString(short) != short {
		t.Error("short strings should not be redacted")
	}
	long := "this is a very long string that needs redacting"
	r := redactString(long)
	if !strings.Contains(r, "chars redacted") {
		t.Error("long strings should be redacted")
	}
	if !strings.HasPrefix(r, "this is a very long ") {
		t.Error("should keep first 20 chars")
	}
}
