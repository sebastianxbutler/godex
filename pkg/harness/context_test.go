package harness

import (
	"context"
	"testing"
)

func TestWithProviderKey(t *testing.T) {
	ctx := context.Background()

	// No key set
	key, ok := ProviderKey(ctx)
	if ok || key != "" {
		t.Error("expected no key")
	}

	// Set a key
	ctx = WithProviderKey(ctx, "sk-test-123")
	key, ok = ProviderKey(ctx)
	if !ok || key != "sk-test-123" {
		t.Errorf("expected 'sk-test-123', got %q (ok=%v)", key, ok)
	}

	// Empty string should return false
	ctx2 := WithProviderKey(context.Background(), "")
	key, ok = ProviderKey(ctx2)
	if ok {
		t.Error("empty key should return ok=false")
	}
}
