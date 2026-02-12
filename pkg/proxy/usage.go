package proxy

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type UsageEvent struct {
	Timestamp        time.Time `json:"ts"`
	KeyID            string    `json:"key_id"`
	Label            string    `json:"label,omitempty"`
	Path             string    `json:"path"`
	Status           int       `json:"status"`
	PromptTokens     int       `json:"prompt_tokens,omitempty"`
	CompletionTokens int       `json:"completion_tokens,omitempty"`
	TotalTokens      int       `json:"total_tokens,omitempty"`
}

type UsageStore struct {
	path           string
	summaryPath    string
	eventsPath     string
	maxBytes       int64
	maxBackups     int
	eventsMaxBytes int64
	eventsBackups  int
	window         time.Duration
	windowStart    time.Time
	mu             sync.Mutex
	counts         map[string]int
	lastSeen       map[string]time.Time
}

func NewUsageStore(path string, summaryPath string, maxBytes int64, maxBackups int, window time.Duration, eventsPath string, eventsMaxBytes int64, eventsBackups int) *UsageStore {
	store := &UsageStore{
		path:           path,
		summaryPath:    summaryPath,
		eventsPath:     eventsPath,
		maxBytes:       maxBytes,
		maxBackups:     maxBackups,
		eventsMaxBytes: eventsMaxBytes,
		eventsBackups:  eventsBackups,
		window:         window,
		counts:         map[string]int{},
		lastSeen:       map[string]time.Time{},
	}
	if window > 0 {
		store.windowStart = time.Now().UTC().Truncate(window)
	}
	return store
}

func (u *UsageStore) Record(ev UsageEvent) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if strings.TrimSpace(u.path) != "" {
		_ = u.rotateIfNeeded()
		f, err := os.OpenFile(u.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			enc := json.NewEncoder(f)
			_ = enc.Encode(ev)
			_ = f.Close()
		}
	}
	u.resetIfWindowElapsed(time.Now().UTC())
	if ev.Path == "__reset__" {
		u.resetKeyInternal(ev.KeyID, "manual", ev.Timestamp)
		return
	}
	if ev.TotalTokens > 0 {
		u.counts[ev.KeyID] += ev.TotalTokens
	}
	if !ev.Timestamp.IsZero() {
		u.lastSeen[ev.KeyID] = ev.Timestamp
	}
	u.persistSummaryLocked()
}

func (u *UsageStore) rotateIfNeeded() error {
	if u.maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(u.path)
	if err != nil {
		return nil
	}
	if info.Size() < u.maxBytes {
		return nil
	}
	return rotateFile(u.path, u.maxBackups)
}

func (u *UsageStore) TotalTokens(keyID string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetIfWindowElapsed(time.Now().UTC())
	return u.counts[keyID]
}

func (u *UsageStore) ResetKey(keyID string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.resetKeyInternal(keyID, "manual", time.Now().UTC())
	if strings.TrimSpace(u.path) == "" {
		return
	}
	_ = u.rotateIfNeeded()
	f, err := os.OpenFile(u.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	_ = enc.Encode(UsageEvent{Timestamp: time.Now().UTC(), KeyID: keyID, Path: "__reset__", Status: http.StatusNoContent})
}

func (u *UsageStore) LoadFromFile() error {
	if strings.TrimSpace(u.path) == "" {
		return u.loadSummary()
	}
	events, err := ReadUsage(u.path, u.window, "")
	if err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.counts = map[string]int{}
	u.lastSeen = map[string]time.Time{}
	if u.window > 0 {
		u.windowStart = time.Now().UTC().Truncate(u.window)
	}
	for _, ev := range events {
		if ev.Path == "__reset__" {
			u.resetKeyInternal(ev.KeyID, "manual", ev.Timestamp)
			continue
		}
		u.counts[ev.KeyID] += ev.TotalTokens
		if ev.Timestamp.After(u.lastSeen[ev.KeyID]) {
			u.lastSeen[ev.KeyID] = ev.Timestamp
		}
	}
	return u.persistSummaryLocked()
}

func (u *UsageStore) resetIfWindowElapsed(now time.Time) {
	if u.window <= 0 {
		return
	}
	if u.windowStart.IsZero() {
		u.windowStart = now.Truncate(u.window)
		return
	}
	if now.Sub(u.windowStart) >= u.window {
		for key := range u.counts {
			u.resetKeyInternal(key, "window", now)
		}
		u.counts = map[string]int{}
		u.lastSeen = map[string]time.Time{}
		u.windowStart = now.Truncate(u.window)
		u.persistSummaryLocked()
	}
}

func (u *UsageStore) resetKeyInternal(keyID string, reason string, now time.Time) {
	u.counts[keyID] = 0
	u.lastSeen[keyID] = now
	u.persistSummaryLocked()
	u.emitEventLocked("reset", keyID, reason, now)
}

func (u *UsageStore) emitEventLocked(kind string, keyID string, reason string, now time.Time) {
	if strings.TrimSpace(u.eventsPath) == "" {
		return
	}
	_ = u.rotateEventsIfNeeded()
	f, err := os.OpenFile(u.eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	_ = enc.Encode(map[string]any{
		"ts":     now.Format(time.RFC3339),
		"event":  kind,
		"key_id": keyID,
		"reason": reason,
	})
}

func (u *UsageStore) rotateEventsIfNeeded() error {
	if u.eventsMaxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(u.eventsPath)
	if err != nil {
		return nil
	}
	if info.Size() < u.eventsMaxBytes {
		return nil
	}
	return rotateFile(u.eventsPath, u.eventsBackups)
}

func (u *UsageStore) persistSummaryLocked() error {
	if strings.TrimSpace(u.summaryPath) == "" {
		return nil
	}
	summary := map[string]any{"updated_at": time.Now().UTC().Format(time.RFC3339), "totals": map[string]any{}}
	vals := map[string]any{}
	for key, total := range u.counts {
		entry := map[string]any{"total_tokens": total}
		if last := u.lastSeen[key]; !last.IsZero() {
			entry["last_seen"] = last.Format(time.RFC3339)
		}
		vals[key] = entry
	}
	summary["totals"] = vals
	buf, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(u.summaryPath, buf, 0o600)
}

func (u *UsageStore) loadSummary() error {
	if strings.TrimSpace(u.summaryPath) == "" {
		return nil
	}
	buf, err := os.ReadFile(u.summaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var payload struct {
		Totals map[string]struct {
			TotalTokens int    `json:"total_tokens"`
			LastSeen    string `json:"last_seen"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(buf, &payload); err != nil {
		return err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.counts = map[string]int{}
	u.lastSeen = map[string]time.Time{}
	for key, entry := range payload.Totals {
		u.counts[key] = entry.TotalTokens
		if entry.LastSeen != "" {
			if ts, err := time.Parse(time.RFC3339, entry.LastSeen); err == nil {
				u.lastSeen[key] = ts
			}
		}
	}
	return nil
}

type UsageSummary struct {
	KeyID       string
	Label       string
	Requests    int
	TotalTokens int
	LastSeen    time.Time
}

func ReadUsage(path string, since time.Duration, keyFilter string) ([]UsageEvent, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	cutoff := time.Time{}
	if since > 0 {
		cutoff = time.Now().Add(-since)
	}
	var out []UsageEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var ev UsageEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if !cutoff.IsZero() && ev.Timestamp.Before(cutoff) {
			continue
		}
		if keyFilter != "" && ev.KeyID != keyFilter {
			continue
		}
		out = append(out, ev)
	}
	return out, scanner.Err()
}

func SummarizeUsage(events []UsageEvent) []UsageSummary {
	m := map[string]UsageSummary{}
	for _, ev := range events {
		s := m[ev.KeyID]
		s.KeyID = ev.KeyID
		s.Label = ev.Label
		s.Requests++
		s.TotalTokens += ev.TotalTokens
		if ev.Timestamp.After(s.LastSeen) {
			s.LastSeen = ev.Timestamp
		}
		m[ev.KeyID] = s
	}
	out := make([]UsageSummary, 0, len(m))
	for _, s := range m {
		out = append(out, s)
	}
	return out
}
