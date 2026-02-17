// Package metrics provides per-backend metrics collection.
package metrics

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// RequestMetric records a single request.
type RequestMetric struct {
	Timestamp time.Time     `json:"ts"`
	Backend   string        `json:"backend"`
	Model     string        `json:"model"`
	Latency   time.Duration `json:"latency_ms"`
	Status    string        `json:"status"` // "ok", "error"
	Error     string        `json:"error,omitempty"`
	TokensIn  int           `json:"tokens_in,omitempty"`
	TokensOut int           `json:"tokens_out,omitempty"`
}

// MarshalJSON customizes JSON output for latency.
func (m RequestMetric) MarshalJSON() ([]byte, error) {
	type Alias RequestMetric
	return json.Marshal(&struct {
		Alias
		LatencyMs int64 `json:"latency_ms"`
	}{
		Alias:     Alias(m),
		LatencyMs: m.Latency.Milliseconds(),
	})
}

// BackendStats holds aggregated stats for a backend.
type BackendStats struct {
	Backend     string  `json:"backend"`
	Requests    int64   `json:"requests"`
	Errors      int64   `json:"errors"`
	LatencyP50  int64   `json:"latency_p50_ms"`
	LatencyP95  int64   `json:"latency_p95_ms"`
	LatencyP99  int64   `json:"latency_p99_ms"`
	TotalTokens int64   `json:"total_tokens"`
	ErrorRate   float64 `json:"error_rate"`
}

// Collector collects and aggregates metrics.
type Collector struct {
	mu          sync.RWMutex
	enabled     bool
	logRequests bool
	path        string
	file        *os.File
	
	// Per-backend latency samples (for percentiles)
	latencies map[string][]int64
	
	// Per-backend counters
	requests    map[string]int64
	errors      map[string]int64
	totalTokens map[string]int64
}

// Config configures the metrics collector.
type Config struct {
	Enabled     bool
	Path        string
	LogRequests bool
}

// NewCollector creates a new metrics collector.
func NewCollector(cfg Config) (*Collector, error) {
	c := &Collector{
		enabled:     cfg.Enabled,
		logRequests: cfg.LogRequests,
		path:        cfg.Path,
		latencies:   make(map[string][]int64),
		requests:    make(map[string]int64),
		errors:      make(map[string]int64),
		totalTokens: make(map[string]int64),
	}

	if cfg.Path != "" && cfg.Enabled {
		f, err := os.OpenFile(cfg.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		c.file = f
	}

	return c, nil
}

// Record records a request metric.
func (c *Collector) Record(m RequestMetric) {
	if !c.enabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Update counters
	c.requests[m.Backend]++
	if m.Status == "error" {
		c.errors[m.Backend]++
	}
	c.totalTokens[m.Backend] += int64(m.TokensIn + m.TokensOut)

	// Store latency sample (keep last 1000 per backend)
	latencyMs := m.Latency.Milliseconds()
	samples := c.latencies[m.Backend]
	if len(samples) >= 1000 {
		samples = samples[1:]
	}
	c.latencies[m.Backend] = append(samples, latencyMs)

	// Persist if configured
	if c.file != nil && c.logRequests {
		data, _ := json.Marshal(m)
		c.file.Write(append(data, '\n'))
	}
}

// Stats returns aggregated stats for all backends.
func (c *Collector) Stats() map[string]*BackendStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]*BackendStats)
	
	for backend := range c.requests {
		stats := &BackendStats{
			Backend:     backend,
			Requests:    c.requests[backend],
			Errors:      c.errors[backend],
			TotalTokens: c.totalTokens[backend],
		}
		
		if stats.Requests > 0 {
			stats.ErrorRate = float64(stats.Errors) / float64(stats.Requests)
		}

		// Calculate percentiles
		if samples := c.latencies[backend]; len(samples) > 0 {
			sorted := make([]int64, len(samples))
			copy(sorted, samples)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
			
			stats.LatencyP50 = percentile(sorted, 50)
			stats.LatencyP95 = percentile(sorted, 95)
			stats.LatencyP99 = percentile(sorted, 99)
		}

		result[backend] = stats
	}

	return result
}

// StatsForBackend returns stats for a specific backend.
func (c *Collector) StatsForBackend(backend string) *BackendStats {
	stats := c.Stats()
	if s, ok := stats[backend]; ok {
		return s
	}
	return &BackendStats{Backend: backend}
}

// Reset clears all collected metrics.
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.latencies = make(map[string][]int64)
	c.requests = make(map[string]int64)
	c.errors = make(map[string]int64)
	c.totalTokens = make(map[string]int64)
}

// Close closes the metrics file if open.
func (c *Collector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.file != nil {
		return c.file.Close()
	}
	return nil
}

// percentile calculates the p-th percentile of a sorted slice.
func percentile(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (len(sorted) * p) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
