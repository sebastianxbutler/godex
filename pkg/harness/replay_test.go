package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLogAndReplay(t *testing.T) {
	// Create a log file
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.jsonl")

	turn := &Turn{Model: "gpt-4", Instructions: "be helpful"}
	entries := []LogEntry{
		{Timestamp: "2026-02-18T10:00:00Z", Type: "turn_start", Turn: turn},
		{Timestamp: "2026-02-18T10:00:01Z", Type: "event", Kind: "text",
			Event: &Event{Kind: EventText, Text: &TextEvent{Delta: "hello"}}},
		{Timestamp: "2026-02-18T10:00:02Z", Type: "event", Kind: "done",
			Event: &Event{Kind: EventDone}},
		{Timestamp: "2026-02-18T10:00:02Z", Type: "turn_end", TotalMs: 2000},
	}

	f, _ := os.Create(logPath)
	enc := json.NewEncoder(f)
	for _, e := range entries {
		enc.Encode(e)
	}
	f.Close()

	// Load the log
	data, err := LoadLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if data.Turn == nil || data.Turn.Model != "gpt-4" {
		t.Error("turn not loaded")
	}
	if len(data.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(data.Events))
	}
	if len(data.Entries) != 4 {
		t.Errorf("expected 4 entries, got %d", len(data.Entries))
	}

	// Replay through mock
	mock := NewMockFromLog(data)
	if mock.Name() != "replay" {
		t.Errorf("expected 'replay', got %q", mock.Name())
	}

	var got []Event
	err = mock.StreamTurn(context.Background(), &Turn{}, func(ev Event) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}
	if got[0].Text == nil || got[0].Text.Delta != "hello" {
		t.Error("first event should be text 'hello'")
	}
}

func TestLoadLogMissingFile(t *testing.T) {
	_, err := LoadLog("/nonexistent/path.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadLogMalformedLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "bad.jsonl")
	os.WriteFile(logPath, []byte("not json\n{\"ts\":\"t\",\"type\":\"turn_end\"}\n"), 0o644)

	data, err := LoadLog(logPath)
	if err != nil {
		t.Fatal(err)
	}
	// Should skip malformed line and parse the valid one
	if len(data.Entries) != 1 {
		t.Errorf("expected 1 valid entry, got %d", len(data.Entries))
	}
}

func TestNewMockFromLogRecord(t *testing.T) {
	data := &LogData{
		Events: []Event{NewTextEvent("hi")},
	}
	mock := NewMockFromLog(data)
	turn := &Turn{Model: "test"}
	mock.StreamTurn(context.Background(), turn, func(Event) error { return nil })
	rec := mock.Recorded()
	if len(rec) != 1 || rec[0].Model != "test" {
		t.Error("mock from log should record turns")
	}
}
