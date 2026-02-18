package harness

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// LogData holds a parsed JSONL log file with the original turn and events.
type LogData struct {
	Turn   *Turn
	Events []Event
	// Entries contains all raw log entries for detailed analysis.
	Entries []LogEntry
}

// LoadLog reads a JSONL log file produced by WithLogger and returns the
// parsed turn and event sequence. This enables offline replay and debugging.
func LoadLog(path string) (*LogData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("loadlog: %w", err)
	}
	defer f.Close()

	data := &LogData{}
	scanner := bufio.NewScanner(f)
	// Allow large lines (up to 10MB)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // skip malformed lines
		}
		data.Entries = append(data.Entries, entry)

		switch entry.Type {
		case "turn_start":
			data.Turn = entry.Turn
		case "event":
			if entry.Event != nil {
				data.Events = append(data.Events, *entry.Event)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("loadlog: scan error: %w", err)
	}

	return data, nil
}

// NewMockFromLog creates a Mock harness pre-loaded with events from a log file.
// This allows replaying the exact event sequence without API calls.
func NewMockFromLog(data *LogData) *Mock {
	return NewMock(MockConfig{
		HarnessName: "replay",
		Responses:   [][]Event{data.Events},
		Record:      true,
	})
}
