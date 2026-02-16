package payments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

type TokenMeterGateway struct {
	cfg Config
}

func NewTokenMeterGateway(cfg Config) Gateway {
	return &TokenMeterGateway{cfg: cfg}
}

func (g *TokenMeterGateway) Enabled() bool {
	return g != nil && g.cfg.Enabled && strings.TrimSpace(g.cfg.TokenMeterURL) != ""
}

func (g *TokenMeterGateway) Challenge(ctx context.Context, purpose string, keyID string, model string, authHeader string) (int, map[string]string, []byte, error) {
	if !g.Enabled() {
		return http.StatusUnauthorized, nil, nil, errors.New("payments disabled")
	}
	payload := map[string]string{"purpose": purpose, "key_id": keyID, "model": model, "auth_header": authHeader}
	buf, _ := json.Marshal(payload)
	resp, err := g.post(ctx, "/l402/challenge", buf)
	if err != nil {
		return http.StatusPaymentRequired, nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	headers := map[string]string{}
	if wa := resp.Header.Get("WWW-Authenticate"); wa != "" {
		headers["WWW-Authenticate"] = wa
	}
	return resp.StatusCode, headers, body, nil
}

func (g *TokenMeterGateway) Redeem(ctx context.Context, authHeader string) (int, []byte, error) {
	if !g.Enabled() {
		return http.StatusUnauthorized, nil, errors.New("payments disabled")
	}
	payload := map[string]string{"auth_header": authHeader}
	buf, _ := json.Marshal(payload)
	resp, err := g.post(ctx, "/l402/redeem", buf)
	if err != nil {
		return http.StatusPaymentRequired, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

func (g *TokenMeterGateway) Pricing(ctx context.Context) (int, []byte, error) {
	if !g.Enabled() {
		return http.StatusServiceUnavailable, nil, errors.New("payments disabled")
	}
	resp, err := g.get(ctx, "/v1/pricing")
	if err != nil {
		return http.StatusServiceUnavailable, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

func (g *TokenMeterGateway) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	url := strings.TrimRight(g.cfg.TokenMeterURL, "/") + path
	client := http.DefaultClient
	if strings.HasPrefix(g.cfg.TokenMeterURL, "unix://") {
		sock := strings.TrimPrefix(g.cfg.TokenMeterURL, "unix://")
		client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", sock)
		}}}
		url = "http://unix" + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func (g *TokenMeterGateway) get(ctx context.Context, path string) (*http.Response, error) {
	url := strings.TrimRight(g.cfg.TokenMeterURL, "/") + path
	client := http.DefaultClient
	if strings.HasPrefix(g.cfg.TokenMeterURL, "unix://") {
		sock := strings.TrimPrefix(g.cfg.TokenMeterURL, "unix://")
		client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return net.Dial("unix", sock)
		}}}
		url = "http://unix" + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}
