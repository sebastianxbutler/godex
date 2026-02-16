package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type KeyStore interface {
	Add(label, rate string, burst int, quota int64, providedKey string, ttl time.Duration) (KeyInfo, string, error)
	SetTokenPolicy(id string, balance int64, allowance int64, duration time.Duration) (KeyInfo, error)
	AddTokens(id string, delta int64) (KeyInfo, error)
}

type KeyInfo struct {
	ID                   string
	TokenBalance         int64
	TokenAllowance       int64
	AllowanceDurationSec int64
}

type Server struct {
	socketPath string
	keys       KeyStore
}

func New(socketPath string, keys KeyStore) *Server {
	return &Server{socketPath: socketPath, keys: keys}
}

func (s *Server) Start(ctx context.Context) error {
	if s == nil || s.keys == nil {
		return errors.New("admin server: missing keystore")
	}
	path := expandPath(s.socketPath)
	if path == "" {
		return errors.New("admin server: socket path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/keys", s.handleKeys)
	mux.HandleFunc("/admin/keys/", s.handleKeyActions)
	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = server.Close()
		_ = listener.Close()
		_ = os.Remove(path)
	}()
	return server.Serve(listener)
}

func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	rec, secret, err := s.keys.Add("token-meter", "60/m", 10, 0, "", 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key_id":     rec.ID,
		"api_key":    secret,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleKeyActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin/keys/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	keyID := parts[0]
	action := parts[1]
	switch action {
	case "policy":
		s.handlePolicy(w, r, keyID)
	case "add-tokens":
		s.handleAddTokens(w, r, keyID)
	default:
		writeError(w, http.StatusNotFound, errors.New("not found"))
	}
}

func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request, keyID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var payload struct {
		TokenAllowance    int64  `json:"token_allowance"`
		AllowanceDuration string `json:"allowance_duration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var duration time.Duration
	if strings.TrimSpace(payload.AllowanceDuration) != "" {
		d, err := time.ParseDuration(payload.AllowanceDuration)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		duration = d
	}
	rec, err := s.keys.SetTokenPolicy(keyID, payload.TokenAllowance, payload.TokenAllowance, duration)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key_id":             rec.ID,
		"token_balance":      rec.TokenBalance,
		"token_allowance":    rec.TokenAllowance,
		"allowance_duration": fmt.Sprintf("%ds", rec.AllowanceDurationSec),
	})
}

func (s *Server) handleAddTokens(w http.ResponseWriter, r *http.Request, keyID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	var payload struct {
		Tokens int64 `json:"tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	rec, err := s.keys.AddTokens(keyID, payload.Tokens)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"key_id":        rec.ID,
		"token_balance": rec.TokenBalance,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	if err == nil {
		w.WriteHeader(status)
		return
	}
	writeJSON(w, status, map[string]any{
		"error": map[string]any{"message": err.Error(), "type": "admin_error"},
	})
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return strings.Replace(path, "~", home, 1)
		}
	}
	return path
}
