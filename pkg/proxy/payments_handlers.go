package proxy

import (
	"errors"
	"net/http"
	"strings"
)

func (s *Server) handlePaymentRedeem(w http.ResponseWriter, r *http.Request) bool {
	if s.payments == nil || !s.payments.Enabled() {
		return false
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authz, "L402 ") {
		return false
	}
	status, body, err := s.payments.Redeem(r.Context(), authz)
	if err != nil {
		writeError(w, http.StatusPaymentRequired, err)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return true
}

func (s *Server) issuePaymentChallenge(w http.ResponseWriter, r *http.Request, purpose string, keyID string, model string) bool {
	if s.payments == nil || !s.payments.Enabled() {
		return false
	}
	status, headers, body, err := s.payments.Challenge(r.Context(), purpose, keyID, model, r.Header.Get("Authorization"))
	if err != nil {
		writeError(w, http.StatusPaymentRequired, err)
		return true
	}
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	if len(body) == 0 {
		writeError(w, status, errors.New("payment required"))
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
	return true
}

func (s *Server) requireAuthOrPayment(w http.ResponseWriter, r *http.Request, model string) (*KeyRecord, bool) {
	if s.handlePaymentRedeem(w, r) {
		return nil, false
	}
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authz, "Bearer ") {
		if s.issuePaymentChallenge(w, r, "issue_key", "", model) {
			return nil, false
		}
		writeError(w, http.StatusUnauthorized, errUnauthorized())
		return nil, false
	}
	key, ok := s.requireAuth(w, r)
	if !ok {
		return nil, false
	}
	return key, true
}
