package proxy

import (
	"net/http"
	"time"

	"godex/pkg/protocol"
)

func (s *Server) allowRequest(w http.ResponseWriter, r *http.Request, key *KeyRecord) (bool, string) {
	if key == nil {
		writeError(w, http.StatusUnauthorized, errUnauthorized())
		return false, "unauthorized"
	}
	if !s.limiters.Allow(key.ID, key.Rate, key.Burst) {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusTooManyRequests, errRateLimited())
		return false, "rate"
	}
	if key.QuotaTokens > 0 && s.usage != nil {
		if s.usage.TotalTokens(key.ID) >= int(key.QuotaTokens) {
			w.Header().Set("Retry-After", "3600")
			writeError(w, http.StatusTooManyRequests, errQuotaExceeded())
			return false, "quota"
		}
	}
	if key.TokenAllowance > 0 {
		rec, _, err := s.keys.UpdateAllowanceWindow(key.ID, key.TokenAllowance, time.Duration(key.AllowanceDurationSec)*time.Second, time.Now().UTC())
		if err == nil {
			key.TokenBalance = rec.TokenBalance
		}
		if key.TokenBalance <= 0 {
			return false, "tokens"
		}
	}
	return true, ""
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
	if total > 0 && s.keys != nil {
		_, _ = s.keys.AddTokens(key.ID, int64(-total))
	}
	s.usage.Record(UsageEvent{
		Timestamp:        time.Now().UTC(),
		KeyID:            key.ID,
		Label:            key.Label,
		Path:             reqPath(r),
		Status:           status,
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      total,
	})
}

func reqPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return r.URL.Path
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
