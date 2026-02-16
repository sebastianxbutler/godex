// Package anthropic implements the Anthropic Messages API backend.
package anthropic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultCredentialsPath is the default location for Claude Code credentials.
var DefaultCredentialsPath = filepath.Join(os.Getenv("HOME"), ".claude", ".credentials.json")

// Credentials holds OAuth tokens for the Anthropic API.
type Credentials struct {
	AccessToken      string       `json:"accessToken"`
	RefreshToken     string       `json:"refreshToken"`
	ExpiresAt        UnixMillis   `json:"expiresAt"`
	SubscriptionType string       `json:"subscriptionType"`
	RateLimitTier    string       `json:"rateLimitTier"`
}

// UnixMillis is a time.Time that unmarshals from Unix milliseconds.
type UnixMillis time.Time

func (u *UnixMillis) UnmarshalJSON(data []byte) error {
	var ms int64
	if err := json.Unmarshal(data, &ms); err != nil {
		// Try parsing as string (ISO format) as fallback
		var s string
		if err2 := json.Unmarshal(data, &s); err2 != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
		*u = UnixMillis(t)
		return nil
	}
	*u = UnixMillis(time.UnixMilli(ms))
	return nil
}

func (u UnixMillis) Time() time.Time {
	return time.Time(u)
}

// credentialsFile represents the structure of ~/.claude/.credentials.json
type credentialsFile struct {
	ClaudeAIOAuth *Credentials `json:"claudeAiOauth"`
}

// TokenStore manages OAuth tokens with automatic refresh.
type TokenStore struct {
	path  string
	creds *Credentials
	mu    sync.RWMutex
}

// NewTokenStore creates a new token store that reads from the given path.
func NewTokenStore(path string) *TokenStore {
	if path == "" {
		path = DefaultCredentialsPath
	}
	return &TokenStore{path: path}
}

// Load reads credentials from disk.
func (s *TokenStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}

	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return fmt.Errorf("parse credentials: %w", err)
	}

	if cf.ClaudeAIOAuth == nil {
		return fmt.Errorf("no claudeAiOauth credentials found")
	}

	s.creds = cf.ClaudeAIOAuth
	return nil
}

// AccessToken returns the current access token, reloading if expired.
func (s *TokenStore) AccessToken() (string, error) {
	s.mu.RLock()
	creds := s.creds
	s.mu.RUnlock()

	// If no credentials loaded, try to load them
	if creds == nil {
		if err := s.Load(); err != nil {
			return "", err
		}
		s.mu.RLock()
		creds = s.creds
		s.mu.RUnlock()
	}

	// Check if token is expired (with 5 minute buffer)
	if time.Now().Add(5 * time.Minute).After(creds.ExpiresAt.Time()) {
		// Try to reload from disk - Claude Code may have refreshed it
		if err := s.Load(); err != nil {
			return "", fmt.Errorf("token expired and reload failed: %w", err)
		}
		s.mu.RLock()
		creds = s.creds
		s.mu.RUnlock()

		// If still expired after reload, return error
		if time.Now().Add(5 * time.Minute).After(creds.ExpiresAt.Time()) {
			return "", fmt.Errorf("token expired at %v, please run 'claude auth login' to refresh", creds.ExpiresAt.Time())
		}
	}

	return creds.AccessToken, nil
}

// IsExpired returns true if the token is expired or will expire soon.
func (s *TokenStore) IsExpired() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.creds == nil {
		return true
	}
	return time.Now().Add(5 * time.Minute).After(s.creds.ExpiresAt.Time())
}

// SubscriptionType returns the subscription type (e.g., "max", "pro").
func (s *TokenStore) SubscriptionType() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.creds == nil {
		return ""
	}
	return s.creds.SubscriptionType
}
