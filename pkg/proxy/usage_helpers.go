package proxy

import (
	"net/http"
	"time"

	"godex/pkg/protocol"
)

func (s *Server) allowRequest(w http.ResponseWriter, r *http.Request, key *KeyRecord) bool {
	if key == nil {
		writeError(w, http.StatusUnauthorized, errUnauthorized())
		return false
	}
	if !s.limiters.Allow(key.ID, key.Rate, key.Burst) {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, errRateLimited())
		return false
	}
	if key.QuotaTokens > 0 && s.usage != nil {
		if s.usage.TotalTokens(key.ID) >= int(key.QuotaTokens) {
			w.Header().Set("Retry-After", "3600")
			writeError(w, http.StatusTooManyRequests, errQuotaExceeded())
			return false
		}
	}
	return true
}

func (s *Server) recordUsage(r *http.Request, key *KeyRecord, status int, usage *protocol.Usage) {
	if key == nil || s.usage == nil {
		return
	}
	prompt := 0
	completion := 0
	if usage != nil {
		prompt = usage.InputTokens
		completion = usage.OutputTokens
	}
	total := prompt + completion
	if key.QuotaTokens > 0 && total > 0 {
		// quota enforcement is coarse: reject once exceeded by total usage
		// callers can use usage logs for stricter enforcement
	}
	s.usage.Record(UsageEvent{
		Timestamp:        time.Now().UTC(),
		KeyID:            key.ID,
		Label:            key.Label,
		Path:             r.URL.Path,
		Status:           status,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	})
}

func errRateLimited() error {
	return &proxyError{message: "rate limit exceeded"}
}

func errQuotaExceeded() error {
	return &proxyError{message: "quota exceeded"}
}

func errUnauthorized() error {
	return &proxyError{message: "unauthorized"}
}

type proxyError struct {
	message string
}

func (e *proxyError) Error() string { return e.message }
