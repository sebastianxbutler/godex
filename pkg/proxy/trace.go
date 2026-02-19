package proxy

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// TraceLogger writes detailed JSONL traces for proxy/harness/model boundaries.
type TraceLogger struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
}

// TraceEntry records one traced payload or event.
type TraceEntry struct {
	Timestamp string          `json:"ts"`
	RequestID string          `json:"request_id,omitempty"`
	Layer     string          `json:"layer,omitempty"`
	Direction string          `json:"direction,omitempty"`
	Path      string          `json:"path,omitempty"`
	Phase     string          `json:"phase,omitempty"`
	Status    int             `json:"status,omitempty"`
	Message   string          `json:"message,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func NewTraceLogger(path string, maxBytes int64, maxBackups int) *TraceLogger {
	if path == "" {
		return nil
	}
	if maxBytes == 0 {
		maxBytes = 25 * 1024 * 1024
	}
	if maxBackups == 0 {
		maxBackups = 5
	}
	return &TraceLogger{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
}

func (t *TraceLogger) Log(entry TraceEntry) {
	if t == nil {
		return
	}
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	_ = t.rotateIfNeeded()
	f, err := os.OpenFile(t.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	_ = enc.Encode(entry)
}

func (t *TraceLogger) rotateIfNeeded() error {
	if t.maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(t.path)
	if err != nil {
		return nil
	}
	if info.Size() < t.maxBytes {
		return nil
	}
	return rotateFile(t.path, t.maxBackups)
}

func (s *Server) tracePayload(requestID, layer, direction, path, phase string, payload any) {
	if s == nil || s.trace == nil {
		return
	}
	var raw []byte
	switch v := payload.(type) {
	case json.RawMessage:
		raw = v
	case []byte:
		raw = v
	default:
		buf, err := json.Marshal(payload)
		if err != nil {
			return
		}
		raw = buf
	}
	s.trace.Log(TraceEntry{
		RequestID: requestID,
		Layer:     layer,
		Direction: direction,
		Path:      path,
		Phase:     phase,
		Payload:   json.RawMessage(raw),
	})
}

func (s *Server) traceMessage(requestID, layer, direction, path, phase, msg string) {
	if s == nil || s.trace == nil {
		return
	}
	s.trace.Log(TraceEntry{
		RequestID: requestID,
		Layer:     layer,
		Direction: direction,
		Path:      path,
		Phase:     phase,
		Message:   msg,
	})
}
