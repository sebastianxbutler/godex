package proxy

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type rateLimiter struct {
	ratePerSec float64
	capacity   float64
	last       time.Time
	budget     float64
	mu         sync.Mutex
}

func newRateLimiter(ratePerSec float64, capacity float64) *rateLimiter {
	now := time.Now()
	return &rateLimiter{ratePerSec: ratePerSec, capacity: capacity, last: now, budget: capacity}
}

func (l *rateLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.budget = minFloat(l.capacity, l.budget+elapsed*l.ratePerSec)
	if l.budget >= 1 {
		l.budget -= 1
		return true
	}
	return false
}

type LimiterStore struct {
	mu       sync.Mutex
	entries  map[string]*rateLimiter
	defRate  string
	defBurst int
}

func NewLimiterStore(defRate string, defBurst int) *LimiterStore {
	return &LimiterStore{entries: map[string]*rateLimiter{}, defRate: defRate, defBurst: defBurst}
}

func (s *LimiterStore) Allow(keyID string, rateSpec string, burst int) bool {
	lim := s.getLimiter(keyID, rateSpec, burst)
	if lim == nil {
		return true
	}
	return lim.Allow()
}

func (s *LimiterStore) getLimiter(keyID string, rateSpec string, burst int) *rateLimiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lim := s.entries[keyID]; lim != nil {
		return lim
	}
	if strings.TrimSpace(rateSpec) == "" {
		rateSpec = s.defRate
	}
	if burst == 0 {
		burst = s.defBurst
	}
	perSec, perWindow, err := parseRate(rateSpec)
	if err != nil {
		return nil
	}
	capacity := float64(burst)
	if capacity < float64(perWindow) {
		capacity = float64(perWindow)
	}
	lim := newRateLimiter(perSec, capacity)
	s.entries[keyID] = lim
	return lim
}

func parseRate(spec string) (perSec float64, perWindow int, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, 0, fmt.Errorf("empty rate")
	}
	parts := strings.Split(spec, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid rate spec")
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	unit := strings.TrimSpace(parts[1])
	var dur time.Duration
	switch unit {
	case "s", "sec", "second", "seconds":
		dur = time.Second
	case "m", "min", "minute", "minutes":
		dur = time.Minute
	case "h", "hr", "hour", "hours":
		dur = time.Hour
	default:
		return 0, 0, fmt.Errorf("invalid rate unit")
	}
	return float64(n) / dur.Seconds(), n, nil
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
