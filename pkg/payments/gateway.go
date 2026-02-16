package payments

import "context"

type Config struct {
	Enabled       bool   `json:"enabled"`
	Provider      string `json:"provider"`
	TokenMeterURL string `json:"token_meter_url"`
}

type Gateway interface {
	Enabled() bool
	Challenge(ctx context.Context, purpose string, keyID string, model string, authHeader string) (int, map[string]string, []byte, error)
	Redeem(ctx context.Context, authHeader string) (int, []byte, error)
	Pricing(ctx context.Context) (int, []byte, error)
}
