// Package openai implements the OpenAI-compatible harness for any Chat
// Completions API provider (OpenAI, Gemini, Groq, local models, etc.).
package openai

import (
	"context"

	"godex/pkg/backend"
	backendOAI "godex/pkg/backend/openapi"
	"godex/pkg/harness"
	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// ClientWrapper wraps the existing backend openapi.Client to adapt it for
// harness use. It delegates all API calls to the underlying client.
type ClientWrapper struct {
	inner *backendOAI.Client
}

// NewClientWrapper creates a wrapper around an existing OpenAI-compatible backend client.
func NewClientWrapper(client *backendOAI.Client) *ClientWrapper {
	return &ClientWrapper{inner: client}
}

// StreamRaw sends a protocol request and streams raw SSE events.
func (w *ClientWrapper) StreamRaw(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	return w.inner.StreamResponses(ctx, req, onEvent)
}

// StreamAndCollectRaw sends a request and returns the raw backend result.
func (w *ClientWrapper) StreamAndCollectRaw(ctx context.Context, req protocol.ResponsesRequest) (backend.StreamResult, error) {
	return w.inner.StreamAndCollect(ctx, req)
}

// ListModelsRaw returns models from the underlying backend.
func (w *ClientWrapper) ListModelsRaw(ctx context.Context) ([]backend.ModelInfo, error) {
	return w.inner.ListModels(ctx)
}

// ConvertModels translates backend.ModelInfo to harness.ModelInfo.
func ConvertModels(models []backend.ModelInfo) []harness.ModelInfo {
	out := make([]harness.ModelInfo, len(models))
	for i, m := range models {
		out[i] = harness.ModelInfo{
			ID:       m.ID,
			Name:     m.DisplayName,
			Provider: "openai",
		}
	}
	return out
}
