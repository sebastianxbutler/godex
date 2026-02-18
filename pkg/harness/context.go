package harness

import "context"

type contextKey string

const providerKeyKey contextKey = "provider-key"

// WithProviderKey returns a context with a provider API key override.
func WithProviderKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, providerKeyKey, key)
}

// ProviderKey extracts the provider API key override from the context, if any.
func ProviderKey(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(providerKeyKey).(string)
	return key, ok && key != ""
}
