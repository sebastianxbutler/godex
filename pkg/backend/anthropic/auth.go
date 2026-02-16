// Package anthropic implements the Anthropic Messages API backend.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// OAuthClientID is the Anthropic OAuth client ID used by Claude Code.
	OAuthClientID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	// OAuthTokenURL is the Anthropic OAuth token endpoint.
	OAuthTokenURL = "https://console.anthropic.com/v1/oauth/token"
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

// AccessToken returns the current access token, refreshing if expired.
func (s *TokenStore) AccessToken() (string, error) {
	return s.AccessTokenWithContext(context.Background())
}

// AccessTokenWithContext returns the current access token, refreshing if expired.
func (s *TokenStore) AccessTokenWithContext(ctx context.Context) (string, error) {
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

		// If still expired after reload, try to refresh
		if time.Now().Add(5 * time.Minute).After(creds.ExpiresAt.Time()) {
			if creds.RefreshToken != "" {
				if err := s.Refresh(ctx, RefreshOptions{}); err != nil {
					return "", fmt.Errorf("token refresh failed: %w", err)
				}
				s.mu.RLock()
				creds = s.creds
				s.mu.RUnlock()
			} else {
				return "", fmt.Errorf("token expired at %v and no refresh token available", creds.ExpiresAt.Time())
			}
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

// CanRefresh returns true if we have a refresh token available.
func (s *TokenStore) CanRefresh() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.creds != nil && s.creds.RefreshToken != ""
}

// RefreshToken returns the current refresh token.
func (s *TokenStore) RefreshToken() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.creds == nil {
		return ""
	}
	return s.creds.RefreshToken
}

// ExpiresAt returns when the current access token expires.
func (s *TokenStore) ExpiresAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.creds == nil {
		return time.Time{}
	}
	return s.creds.ExpiresAt.Time()
}

// RefreshOptions configures the token refresh behavior.
type RefreshOptions struct {
	HTTPClient *http.Client
}

// Refresh exchanges the refresh token for a new access token.
func (s *TokenStore) Refresh(ctx context.Context, opts RefreshOptions) error {
	s.mu.Lock()
	if s.creds == nil || s.creds.RefreshToken == "" {
		s.mu.Unlock()
		return fmt.Errorf("no refresh token available")
	}
	refreshToken := s.creds.RefreshToken
	s.mu.Unlock()

	// Build refresh request
	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     OAuthClientID,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode refresh payload: %w", err)
	}

	hc := opts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OAuthTokenURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	var rr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"` // seconds
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return fmt.Errorf("decode refresh response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := rr.ErrorDesc
		if detail == "" {
			detail = rr.Error
		}
		if detail == "" {
			detail = resp.Status
		}
		return fmt.Errorf("refresh rejected (%d): %s", resp.StatusCode, detail)
	}

	if rr.AccessToken == "" {
		return fmt.Errorf("refresh response missing access_token")
	}

	// Update credentials
	s.mu.Lock()
	s.creds.AccessToken = rr.AccessToken
	if rr.RefreshToken != "" {
		s.creds.RefreshToken = rr.RefreshToken
	}
	// Calculate new expiry time
	if rr.ExpiresIn > 0 {
		s.creds.ExpiresAt = UnixMillis(time.Now().Add(time.Duration(rr.ExpiresIn) * time.Second))
	}
	s.mu.Unlock()

	// Save to disk
	if err := s.Save(); err != nil {
		return fmt.Errorf("save refreshed credentials: %w", err)
	}

	return nil
}

// Save writes the current credentials back to disk.
func (s *TokenStore) Save() error {
	s.mu.RLock()
	creds := s.creds
	path := s.path
	s.mu.RUnlock()

	if creds == nil {
		return fmt.Errorf("no credentials to save")
	}

	// Read existing file to preserve other fields
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing credentials: %w", err)
	}

	var cf credentialsFile
	if len(data) > 0 {
		if err := json.Unmarshal(data, &cf); err != nil {
			// If can't parse, start fresh
			cf = credentialsFile{}
		}
	}

	cf.ClaudeAIOAuth = creds

	out, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}
	out = append(out, '\n')

	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// MarshalJSON implements json.Marshaler for UnixMillis.
func (u UnixMillis) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(u).UnixMilli())
}
