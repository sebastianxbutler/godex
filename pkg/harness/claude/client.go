// Package claude implements the Claude harness for the Anthropic Messages API.
package claude

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	backendAnth "godex/pkg/backend/anthropic"
	"godex/pkg/harness"
)

// ClientWrapper wraps the existing Anthropic backend client, providing
// direct access to the Anthropic SDK for harness use.
type ClientWrapper struct {
	tokens *backendAnth.TokenStore
	cfg    ClientConfig
}

// ClientConfig holds configuration for the Claude client wrapper.
type ClientConfig struct {
	// DefaultMaxTokens is used when not specified in the request.
	DefaultMaxTokens int

	// DefaultThinkingBudget is the default budget_tokens for extended thinking.
	DefaultThinkingBudget int
}

// NewClientWrapper creates a wrapper around the existing Anthropic token store.
func NewClientWrapper(tokens *backendAnth.TokenStore, cfg ClientConfig) *ClientWrapper {
	if cfg.DefaultMaxTokens <= 0 {
		cfg.DefaultMaxTokens = 16384
	}
	if cfg.DefaultThinkingBudget <= 0 {
		cfg.DefaultThinkingBudget = 10000
	}
	return &ClientWrapper{tokens: tokens, cfg: cfg}
}

// StreamMessages starts a streaming Messages API call and invokes onEvent for
// each raw Anthropic stream event.
func (w *ClientWrapper) StreamMessages(ctx context.Context, params anthropic.MessageNewParams, onEvent func(anthropic.MessageStreamEventUnion) error) error {
	token, err := w.tokens.AccessToken()
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	client := anthropic.NewClient(
		option.WithAuthToken(token),
		option.WithHeader("anthropic-beta", "oauth-2025-04-20"),
	)

	stream := client.Messages.NewStreaming(ctx, params)
	for stream.Next() {
		if err := onEvent(stream.Current()); err != nil {
			return err
		}
	}
	return stream.Err()
}

// ListModels returns available Claude models.
func (w *ClientWrapper) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	token, err := w.tokens.AccessToken()
	if err != nil {
		return nil, fmt.Errorf("get access token: %w", err)
	}

	client := anthropic.NewClient(
		option.WithAuthToken(token),
		option.WithHeader("anthropic-beta", "oauth-2025-04-20"),
	)

	page, err := client.Models.List(ctx, anthropic.ModelListParams{})
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}

	var models []harness.ModelInfo
	for _, m := range page.Data {
		models = append(models, harness.ModelInfo{
			ID:       m.ID,
			Name:     m.DisplayName,
			Provider: "claude",
		})
	}
	return models, nil
}
