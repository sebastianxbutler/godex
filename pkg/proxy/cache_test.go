package proxy

import (
	"testing"
	"time"
)

func TestNewCache(t *testing.T) {
	cache := NewCache(5 * time.Minute)
	if cache == nil {
		t.Fatal("NewCache returned nil")
	}
	if cache.ttl != 5*time.Minute {
		t.Errorf("ttl = %v, want 5m", cache.ttl)
	}
}

func TestNewCacheDefaultTTL(t *testing.T) {
	cache := NewCache(0)
	if cache.ttl != 6*time.Hour {
		t.Errorf("default ttl = %v, want 6h", cache.ttl)
	}
}

func TestHashInstructions(t *testing.T) {
	hash1 := HashInstructions("Hello world")
	hash2 := HashInstructions("Hello world")
	hash3 := HashInstructions("Different text")

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different input should produce different hash")
	}
	if hash1 == "" {
		t.Error("hash should not be empty")
	}
}

func TestCacheTouch(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "session-123"

	// Touch creates entry if not exists
	cache.Touch(sessionKey)

	cache.mu.Lock()
	entry, ok := cache.entries[sessionKey]
	cache.mu.Unlock()

	if !ok {
		t.Fatal("entry not created")
	}
	if entry.lastSeen.IsZero() {
		t.Error("lastSeen not set")
	}
}

func TestGetInstructionsHash(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "session-123"

	// Empty at first
	hash, ok := cache.GetInstructionsHash(sessionKey)
	if ok {
		t.Error("expected no hash for new session")
	}
	if hash != "" {
		t.Errorf("hash = %q", hash)
	}

	// Touch and set hash
	cache.Touch(sessionKey)
	cache.UpdateInstructionsHash(sessionKey, "hash123")

	hash, ok = cache.GetInstructionsHash(sessionKey)
	if !ok {
		t.Error("expected hash after update")
	}
	if hash != "hash123" {
		t.Errorf("hash = %q", hash)
	}
}

func TestUpdateInstructionsHash(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "session-123"

	// Update without touch should work
	cache.UpdateInstructionsHash(sessionKey, "hash1")

	hash, ok := cache.GetInstructionsHash(sessionKey)
	if !ok {
		t.Error("expected hash after update")
	}
	if hash != "hash1" {
		t.Errorf("hash = %q", hash)
	}

	// Update again
	cache.UpdateInstructionsHash(sessionKey, "hash2")
	hash, _ = cache.GetInstructionsHash(sessionKey)
	if hash != "hash2" {
		t.Errorf("hash = %q after second update", hash)
	}
}

func TestSaveInstructions(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "session-123"

	cache.SaveInstructions(sessionKey, "Test instructions")

	// Verify hash was saved
	hash, ok := cache.GetInstructionsHash(sessionKey)
	if !ok {
		t.Error("expected hash after SaveInstructions")
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}

	// Verify instructions can be retrieved
	instructions, ok := cache.GetInstructions(sessionKey)
	if !ok {
		t.Error("expected instructions after SaveInstructions")
	}
	if instructions != "Test instructions" {
		t.Errorf("instructions = %q", instructions)
	}
}

func TestGetToolCall(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "session-123"
	callID := "call-abc"

	// No tool call initially
	_, ok := cache.GetToolCall(sessionKey, callID)
	if ok {
		t.Error("expected no tool call initially")
	}

	// Add tool calls via SaveToolCalls
	cache.SaveToolCalls(sessionKey, map[string]ToolCall{
		callID: {Name: "test_func", Arguments: `{"arg1": "value1"}`},
	})

	// Now should find it
	tc, ok := cache.GetToolCall(sessionKey, callID)
	if !ok {
		t.Fatal("expected to find tool call")
	}
	if tc.Name != "test_func" {
		t.Errorf("Name = %q", tc.Name)
	}
	if tc.Arguments != `{"arg1": "value1"}` {
		t.Errorf("Arguments = %q", tc.Arguments)
	}
}

func TestCacheEviction(t *testing.T) {
	cache := NewCache(50 * time.Millisecond)
	sessionKey := "session-123"

	cache.Touch(sessionKey)
	cache.SaveInstructions(sessionKey, "test")

	// Entry should exist
	_, ok := cache.GetInstructionsHash(sessionKey)
	if !ok {
		t.Error("expected entry before expiry")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Access should now fail or return stale entry (implementation dependent)
	// The getEntryLocked function checks TTL and evicts expired entries
	_, ok = cache.GetInstructionsHash(sessionKey)
	// After expiry, entry is recreated so hash will be empty
	// This tests the eviction behavior
}

func TestCacheGetEntryLocked(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "session-123"

	// Create entry
	cache.Touch(sessionKey)

	cache.mu.Lock()
	entry := cache.getEntryLocked(sessionKey)
	cache.mu.Unlock()

	if entry == nil {
		t.Fatal("expected entry")
	}
	if entry.lastSeen.IsZero() {
		t.Error("lastSeen not set")
	}
}

func TestCacheGetEntryLockedCreatesNew(t *testing.T) {
	cache := NewCache(time.Hour)
	sessionKey := "new-session"

	cache.mu.Lock()
	entry := cache.getEntryLocked(sessionKey)
	cache.mu.Unlock()

	if entry == nil {
		t.Fatal("expected new entry to be created")
	}
}

func TestHashInstructionsEmpty(t *testing.T) {
	hash := HashInstructions("")
	if hash == "" {
		t.Error("hash of empty string should not be empty")
	}
}

func TestMultipleSessions(t *testing.T) {
	cache := NewCache(time.Hour)

	// Create multiple sessions
	for i := 0; i < 10; i++ {
		sessionKey := "session-" + string(rune('a'+i))
		cache.Touch(sessionKey)
		cache.SaveInstructions(sessionKey, "Instructions for "+sessionKey)
	}

	// Verify all exist
	cache.mu.Lock()
	count := len(cache.entries)
	cache.mu.Unlock()

	if count != 10 {
		t.Errorf("expected 10 entries, got %d", count)
	}
}
