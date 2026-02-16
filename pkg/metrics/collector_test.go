package metrics

import (
	"testing"
	"time"
)

func TestCollector(t *testing.T) {
	c, err := NewCollector(Config{Enabled: true})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer c.Close()

	// Record some metrics
	c.Record(RequestMetric{
		Timestamp: time.Now(),
		Backend:   "test",
		Model:     "test-model",
		Latency:   100 * time.Millisecond,
		Status:    "ok",
		TokensIn:  10,
		TokensOut: 20,
	})
	c.Record(RequestMetric{
		Timestamp: time.Now(),
		Backend:   "test",
		Model:     "test-model",
		Latency:   200 * time.Millisecond,
		Status:    "ok",
	})
	c.Record(RequestMetric{
		Timestamp: time.Now(),
		Backend:   "test",
		Model:     "test-model",
		Latency:   50 * time.Millisecond,
		Status:    "error",
		Error:     "test error",
	})

	// Check stats
	stats := c.Stats()
	if len(stats) != 1 {
		t.Errorf("expected 1 backend, got %d", len(stats))
	}

	s := stats["test"]
	if s.Requests != 3 {
		t.Errorf("expected 3 requests, got %d", s.Requests)
	}
	if s.Errors != 1 {
		t.Errorf("expected 1 error, got %d", s.Errors)
	}
	if s.TotalTokens != 30 {
		t.Errorf("expected 30 tokens, got %d", s.TotalTokens)
	}
}

func TestCollectorDisabled(t *testing.T) {
	c, err := NewCollector(Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer c.Close()

	c.Record(RequestMetric{
		Backend: "test",
		Status:  "ok",
	})

	stats := c.Stats()
	if len(stats) != 0 {
		t.Errorf("expected no stats when disabled, got %d", len(stats))
	}
}

func TestCollectorReset(t *testing.T) {
	c, err := NewCollector(Config{Enabled: true})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer c.Close()

	c.Record(RequestMetric{Backend: "test", Status: "ok"})
	
	stats := c.Stats()
	if len(stats) != 1 {
		t.Errorf("expected 1 backend before reset")
	}

	c.Reset()
	
	stats = c.Stats()
	if len(stats) != 0 {
		t.Errorf("expected 0 backends after reset, got %d", len(stats))
	}
}

func TestPercentile(t *testing.T) {
	samples := []int64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	
	// For 10 elements, p50 = index 5 = 60
	if p := percentile(samples, 50); p != 60 {
		t.Errorf("p50: expected 60, got %d", p)
	}
	// p95 = index 9 = 100
	if p := percentile(samples, 95); p != 100 {
		t.Errorf("p95: expected 100, got %d", p)
	}
	// p99 = index 9 = 100
	if p := percentile(samples, 99); p != 100 {
		t.Errorf("p99: expected 100, got %d", p)
	}
	// Edge case: empty slice
	if p := percentile([]int64{}, 50); p != 0 {
		t.Errorf("empty p50: expected 0, got %d", p)
	}
}
