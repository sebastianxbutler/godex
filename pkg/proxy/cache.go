package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

type ToolCall struct {
	Name      string
	Arguments string
}

type cacheEntry struct {
	instructions     string
	instructionsHash string
	toolCalls        map[string]ToolCall
	lastSeen         time.Time
}

type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*cacheEntry
}

func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &Cache{ttl: ttl, entries: map[string]*cacheEntry{}}
}

func HashInstructions(instructions string) string {
	h := sha256.Sum256([]byte(instructions))
	return hex.EncodeToString(h[:])
}

func (c *Cache) Touch(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.getEntryLocked(key)
}

func (c *Cache) GetInstructionsHash(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.getEntryLocked(key)
	if entry == nil {
		return "", false
	}
	if entry.instructionsHash == "" {
		return "", false
	}
	return entry.instructionsHash, true
}

func (c *Cache) GetInstructions(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.getEntryLocked(key)
	if entry == nil || strings.TrimSpace(entry.instructions) == "" {
		return "", false
	}
	return entry.instructions, true
}

func (c *Cache) UpdateInstructionsHash(key, hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.getEntryLocked(key)
	if entry == nil {
		return
	}
	entry.instructionsHash = hash
}

func (c *Cache) SaveInstructions(key, instructions string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.getEntryLocked(key)
	if entry == nil {
		return
	}
	entry.instructions = instructions
	entry.instructionsHash = HashInstructions(instructions)
}

func (c *Cache) SaveToolCalls(key string, calls map[string]ToolCall) {
	if len(calls) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.getEntryLocked(key)
	if entry == nil {
		return
	}
	if entry.toolCalls == nil {
		entry.toolCalls = map[string]ToolCall{}
	}
	for callID, call := range calls {
		entry.toolCalls[callID] = call
	}
}

func (c *Cache) GetToolCall(key, callID string) (ToolCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.getEntryLocked(key)
	if entry == nil || entry.toolCalls == nil {
		return ToolCall{}, false
	}
	call, ok := entry.toolCalls[callID]
	return call, ok
}

func (c *Cache) getEntryLocked(key string) *cacheEntry {
	if key == "" {
		return nil
	}
	if entry, ok := c.entries[key]; ok {
		if time.Since(entry.lastSeen) <= c.ttl {
			entry.lastSeen = time.Now()
			return entry
		}
		delete(c.entries, key)
	}
	entry := &cacheEntry{lastSeen: time.Now()}
	c.entries[key] = entry
	return entry
}
