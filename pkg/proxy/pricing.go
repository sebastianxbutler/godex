package proxy

import (
	"net/http"
)

func (s *Server) handlePricing(w http.ResponseWriter, r *http.Request) {
	if s.payments == nil || !s.payments.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "disabled",
			"message": "payments not enabled",
		})
		return
	}
	status, body, err := s.payments.Pricing(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "unavailable",
			"message": "token-meter not running",
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
