package proxy

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// AuditLogger writes request/response audit entries to a JSONL file with rotation.
type AuditLogger struct {
	mu         sync.Mutex
	path       string
	maxBytes   int64
	maxBackups int
}

// AuditEntry records a single request/response pair.
type AuditEntry struct {
	Timestamp  string          `json:"ts"`
	RequestID  string          `json:"request_id,omitempty"`
	KeyID      string          `json:"key_id,omitempty"`
	KeyLabel   string          `json:"key_label,omitempty"`
	Method     string          `json:"method"`
	Path       string          `json:"path"`
	Model      string          `json:"model,omitempty"`
	Backend    string          `json:"backend,omitempty"`
	Status     int             `json:"status"`
	ElapsedMs  int64           `json:"elapsed_ms"`
	InputItems int             `json:"input_items,omitempty"`
	ToolCount  int             `json:"tool_count,omitempty"`
	HasToolCalls bool          `json:"has_tool_calls,omitempty"`
	ToolCallNames []string     `json:"tool_call_names,omitempty"`
	OutputText string          `json:"output_text,omitempty"`
	TokensIn   int             `json:"tokens_in,omitempty"`
	TokensOut  int             `json:"tokens_out,omitempty"`
	Error      string          `json:"error,omitempty"`
	Request    json.RawMessage `json:"request,omitempty"`
}

// NewAuditLogger creates an audit logger. Returns nil if path is empty.
func NewAuditLogger(path string, maxBytes int64, maxBackups int) *AuditLogger {
	if path == "" {
		return nil
	}
	if maxBytes == 0 {
		maxBytes = 10 * 1024 * 1024 // 10MB
	}
	if maxBackups == 0 {
		maxBackups = 3
	}
	return &AuditLogger{
		path:       path,
		maxBytes:   maxBytes,
		maxBackups: maxBackups,
	}
}

// Log writes an audit entry.
func (a *AuditLogger) Log(entry AuditEntry) {
	if a == nil {
		return
	}
	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	_ = a.rotateIfNeeded()

	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	_ = enc.Encode(entry)
}

func (a *AuditLogger) rotateIfNeeded() error {
	if a.maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(a.path)
	if err != nil {
		return nil
	}
	if info.Size() < a.maxBytes {
		return nil
	}
	return rotateFile(a.path, a.maxBackups)
}
