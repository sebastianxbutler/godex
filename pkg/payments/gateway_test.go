package payments

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewTokenMeterGateway(t *testing.T) {
	cfg := Config{
		Enabled:       true,
		Provider:      "l402",
		TokenMeterURL: "http://localhost:9900",
	}

	gw := NewTokenMeterGateway(cfg)
	if gw == nil {
		t.Fatal("NewTokenMeterGateway returned nil")
	}

	tmg, ok := gw.(*TokenMeterGateway)
	if !ok {
		t.Fatal("expected TokenMeterGateway type")
	}
	if !tmg.cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want bool
	}{
		{
			name: "enabled_with_url",
			cfg:  Config{Enabled: true, TokenMeterURL: "http://localhost:9900"},
			want: true,
		},
		{
			name: "disabled",
			cfg:  Config{Enabled: false, TokenMeterURL: "http://localhost:9900"},
			want: false,
		},
		{
			name: "enabled_no_url",
			cfg:  Config{Enabled: true, TokenMeterURL: ""},
			want: false,
		},
		{
			name: "enabled_whitespace_url",
			cfg:  Config{Enabled: true, TokenMeterURL: "   "},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw := NewTokenMeterGateway(tt.cfg)
			if got := gw.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnabledNilGateway(t *testing.T) {
	var gw *TokenMeterGateway
	if gw.Enabled() {
		t.Error("nil gateway should not be enabled")
	}
}

func TestChallengeDisabled(t *testing.T) {
	gw := NewTokenMeterGateway(Config{Enabled: false})

	status, headers, body, err := gw.Challenge(context.Background(), "topup", "key_123", "gpt-5", "")
	if err == nil {
		t.Error("expected error for disabled gateway")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", status, http.StatusUnauthorized)
	}
	if headers != nil {
		t.Error("expected nil headers")
	}
	if body != nil {
		t.Error("expected nil body")
	}
}

func TestChallengeSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/l402/challenge" {
			t.Errorf("path = %q, want /l402/challenge", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}

		var payload map[string]string
		json.NewDecoder(r.Body).Decode(&payload)

		if payload["purpose"] != "topup" {
			t.Errorf("purpose = %q", payload["purpose"])
		}
		if payload["key_id"] != "key_123" {
			t.Errorf("key_id = %q", payload["key_id"])
		}

		w.Header().Set("WWW-Authenticate", "L402 token=\"abc\", invoice=\"lnbc...\"")
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"invoice":"lnbc..."}`))
	}))
	defer server.Close()

	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: server.URL,
	})

	status, headers, body, err := gw.Challenge(context.Background(), "topup", "key_123", "gpt-5", "")
	if err != nil {
		t.Fatalf("Challenge error: %v", err)
	}
	if status != http.StatusPaymentRequired {
		t.Errorf("status = %d", status)
	}
	if headers["WWW-Authenticate"] == "" {
		t.Error("expected WWW-Authenticate header")
	}
	if len(body) == 0 {
		t.Error("expected body")
	}
}

func TestRedeemDisabled(t *testing.T) {
	gw := NewTokenMeterGateway(Config{Enabled: false})

	status, body, err := gw.Redeem(context.Background(), "L402 token:preimage")
	if err == nil {
		t.Error("expected error for disabled gateway")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d", status)
	}
	if body != nil {
		t.Error("expected nil body")
	}
}

func TestRedeemSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/l402/redeem" {
			t.Errorf("path = %q", r.URL.Path)
		}

		var payload map[string]string
		json.NewDecoder(r.Body).Decode(&payload)

		if payload["auth_header"] != "L402 token:preimage" {
			t.Errorf("auth_header = %q", payload["auth_header"])
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"api_key":"gxk_test","tokens":10000}`))
	}))
	defer server.Close()

	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: server.URL,
	})

	status, body, err := gw.Redeem(context.Background(), "L402 token:preimage")
	if err != nil {
		t.Fatalf("Redeem error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if len(body) == 0 {
		t.Error("expected body")
	}
}

func TestPricingDisabled(t *testing.T) {
	gw := NewTokenMeterGateway(Config{Enabled: false})

	status, body, err := gw.Pricing(context.Background())
	if err == nil {
		t.Error("expected error for disabled gateway")
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d", status)
	}
	if body != nil {
		t.Error("expected nil body")
	}
}

func TestPricingSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pricing" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q", r.Method)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","btc_usd":68000}`))
	}))
	defer server.Close()

	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: server.URL,
	})

	status, body, err := gw.Pricing(context.Background())
	if err != nil {
		t.Fatalf("Pricing error: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d", status)
	}
	if len(body) == 0 {
		t.Error("expected body")
	}
}

func TestChallengeNetworkError(t *testing.T) {
	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: "http://localhost:99999", // invalid port
	})

	status, _, _, err := gw.Challenge(context.Background(), "topup", "key_123", "gpt-5", "")
	if err == nil {
		t.Error("expected network error")
	}
	if status != http.StatusPaymentRequired {
		t.Errorf("status = %d", status)
	}
}

func TestRedeemNetworkError(t *testing.T) {
	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: "http://localhost:99999",
	})

	status, _, err := gw.Redeem(context.Background(), "L402 token:preimage")
	if err == nil {
		t.Error("expected network error")
	}
	if status != http.StatusPaymentRequired {
		t.Errorf("status = %d", status)
	}
}

func TestPricingNetworkError(t *testing.T) {
	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: "http://localhost:99999",
	})

	status, _, err := gw.Pricing(context.Background())
	if err == nil {
		t.Error("expected network error")
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d", status)
	}
}

func TestURLWithTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify path doesn't have double slashes
		if r.URL.Path == "//v1/pricing" {
			t.Error("path has double slash")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	gw := NewTokenMeterGateway(Config{
		Enabled:       true,
		TokenMeterURL: server.URL + "/", // trailing slash
	})

	gw.Pricing(context.Background())
}

func TestConfigStruct(t *testing.T) {
	cfg := Config{
		Enabled:       true,
		Provider:      "l402",
		TokenMeterURL: "http://localhost:9900",
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Enabled != cfg.Enabled {
		t.Error("Enabled mismatch")
	}
	if decoded.Provider != cfg.Provider {
		t.Error("Provider mismatch")
	}
	if decoded.TokenMeterURL != cfg.TokenMeterURL {
		t.Error("TokenMeterURL mismatch")
	}
}

func TestGatewayInterface(t *testing.T) {
	var _ Gateway = (*TokenMeterGateway)(nil)
}
