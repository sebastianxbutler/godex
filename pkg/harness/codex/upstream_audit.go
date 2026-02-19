package codex

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type upstreamAuditLogger struct {
	mu   sync.Mutex
	path string
}

func newUpstreamAuditLogger(path string) *upstreamAuditLogger {
	if path == "" {
		return nil
	}
	return &upstreamAuditLogger{path: path}
}

func (l *upstreamAuditLogger) Log(entry map[string]any) {
	if l == nil {
		return
	}
	entry["ts"] = time.Now().UTC().Format(time.RFC3339Nano)

	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	_ = enc.Encode(entry)
}
