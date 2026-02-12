package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	ModeChatGPT = "chatgpt"
	ModeAPIKey  = "api_key"
)

var (
	refreshURL      = "https://auth.openai.com/oauth/token"
	refreshClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	refreshScope    = "openid profile email"
)

var (
	ErrNoToken            = errors.New("no authorization token in auth.json")
	ErrRefreshUnavailable = errors.New("token refresh unavailable for current auth state")
)

type File struct {
	AuthMode string `json:"auth_mode,omitempty"`
	APIKey   string `json:"OPENAI_API_KEY,omitempty"`
	Tokens   Tokens `json:"tokens,omitempty"`
}

type Tokens struct {
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	IDToken      IDTokenV1 `json:"id_token,omitempty"`
}

type IDTokenV1 struct {
	RawJWT           string `json:"raw_jwt,omitempty"`
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
}

func (t *IDTokenV1) UnmarshalJSON(data []byte) error {
	// Accept either a string JWT or an object with fields.
	if len(data) == 0 {
		return nil
	}
	if data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		t.RawJWT = raw
		return nil
	}
	// Fallback to object
	var aux struct {
		RawJWT           string `json:"raw_jwt"`
		ChatGPTAccountID string `json:"chatgpt_account_id"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	t.RawJWT = aux.RawJWT
	t.ChatGPTAccountID = aux.ChatGPTAccountID
	return nil
}

type Store struct {
	path string
	mu   sync.Mutex
	File File
}

type RefreshOptions struct {
	AllowNetwork bool
	HTTPClient   *http.Client
}

func SetRefreshConfig(url, clientID, scope string) {
	if strings.TrimSpace(url) != "" {
		refreshURL = strings.TrimSpace(url)
	}
	if strings.TrimSpace(clientID) != "" {
		refreshClientID = strings.TrimSpace(clientID)
	}
	if strings.TrimSpace(scope) != "" {
		refreshScope = strings.TrimSpace(scope)
	}
}

func DefaultPath() (string, error) {
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return filepath.Join(codexHome, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

func Load(path string) (*Store, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth file: %w", err)
	}
	var f File
	if err := json.Unmarshal(buf, &f); err != nil {
		return nil, fmt.Errorf("parse auth file: %w", err)
	}
	if f.AuthMode == "" {
		f.AuthMode = ModeChatGPT
	}
	return &Store{path: path, File: f}, nil
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) AuthorizationToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return authorizationTokenNoLock(s.File)
}

func (s *Store) AccountID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return accountIDNoLock(s.File)
}

func (s *Store) IsChatGPT() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.File.AuthMode == ModeChatGPT
}

func (s *Store) RefreshToken() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.File.Tokens.RefreshToken
}

func (s *Store) CanRefresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return canRefreshNoLock(s.File)
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveNoLock()
}

func (s *Store) saveNoLock() error {
	out, err := json.MarshalIndent(s.File, "", "  ")
	if err != nil {
		return fmt.Errorf("encode auth file: %w", err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(s.path, out, 0o600); err != nil {
		return fmt.Errorf("write auth file: %w", err)
	}
	return nil
}

func (s *Store) Refresh(ctx context.Context, opts RefreshOptions) error {
	if !opts.AllowNetwork {
		return fmt.Errorf("refresh blocked: %w", ErrRefreshUnavailable)
	}

	s.mu.Lock()
	if !canRefreshNoLock(s.File) {
		s.mu.Unlock()
		return ErrRefreshUnavailable
	}
	refreshToken := s.File.Tokens.RefreshToken
	s.mu.Unlock()

	body := map[string]string{
		"client_id":     refreshClientID,
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"scope":         refreshScope,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode refresh payload: %w", err)
	}

	hc := opts.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(payload))
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
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return fmt.Errorf("decode refresh response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail := strings.TrimSpace(rr.Error)
		if detail == "" {
			detail = resp.Status
		}
		return fmt.Errorf("refresh rejected: %s", detail)
	}
	if rr.AccessToken == "" {
		return errors.New("refresh response missing access_token")
	}

	s.mu.Lock()
	s.File.Tokens.AccessToken = rr.AccessToken
	if rr.RefreshToken != "" {
		s.File.Tokens.RefreshToken = rr.RefreshToken
	}
	if rr.IDToken != "" {
		s.File.Tokens.IDToken.RawJWT = rr.IDToken
	}
	err = s.saveNoLock()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

func authorizationTokenNoLock(f File) (string, error) {
	switch f.AuthMode {
	case ModeAPIKey:
		if f.APIKey == "" {
			return "", ErrNoToken
		}
		return f.APIKey, nil
	case "", ModeChatGPT:
		if f.Tokens.AccessToken == "" {
			return "", ErrNoToken
		}
		return f.Tokens.AccessToken, nil
	default:
		if f.Tokens.AccessToken != "" {
			return f.Tokens.AccessToken, nil
		}
		if f.APIKey != "" {
			return f.APIKey, nil
		}
		return "", ErrNoToken
	}
}

func accountIDNoLock(f File) string {
	if f.Tokens.AccountID != "" {
		return f.Tokens.AccountID
	}
	return f.Tokens.IDToken.ChatGPTAccountID
}

func canRefreshNoLock(f File) bool {
	return f.AuthMode == ModeChatGPT && f.Tokens.RefreshToken != ""
}
