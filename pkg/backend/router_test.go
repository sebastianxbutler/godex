package backend

import (
	"context"
	"testing"

	"godex/pkg/protocol"
	"godex/pkg/sse"
)

// mockBackend implements Backend for testing.
type mockBackend struct {
	name string
}

func (m *mockBackend) Name() string { return m.name }
func (m *mockBackend) StreamResponses(ctx context.Context, req protocol.ResponsesRequest, onEvent func(sse.Event) error) error {
	return nil
}
func (m *mockBackend) StreamAndCollect(ctx context.Context, req protocol.ResponsesRequest) (StreamResult, error) {
	return StreamResult{}, nil
}
func (m *mockBackend) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return []ModelInfo{{ID: m.name + "-model"}}, nil
}

func TestRouterBackendFor(t *testing.T) {
	config := DefaultRouterConfig()
	router := NewRouter(config)

	anthropic := &mockBackend{name: "anthropic"}
	codex := &mockBackend{name: "codex"}
	router.Register("anthropic", anthropic)
	router.Register("codex", codex)

	tests := []struct {
		model string
		want  string
	}{
		{"claude-sonnet-4-5-20250929", "anthropic"},
		{"claude-opus-4-5-20250929", "anthropic"},
		{"sonnet", "anthropic"},
		{"opus", "anthropic"},
		{"haiku", "anthropic"},
		{"gpt-4o", "codex"},
		{"gpt-4o-mini", "codex"},
		{"o1-preview", "codex"},
		{"o3-mini", "codex"},
		{"codex-mini", "codex"},
		{"unknown-model", "codex"}, // falls back to default
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			b := router.BackendFor(tt.model)
			if b == nil {
				t.Fatalf("no backend for model %q", tt.model)
			}
			if b.Name() != tt.want {
				t.Errorf("model %q: got backend %q, want %q", tt.model, b.Name(), tt.want)
			}
		})
	}
}

func TestRouterExpandAlias(t *testing.T) {
	config := DefaultRouterConfig()
	router := NewRouter(config)

	tests := []struct {
		input string
		want  string
	}{
		{"sonnet", "claude-sonnet-4-5-20250929"},
		{"opus", "claude-opus-4-5"},
		{"haiku", "claude-haiku-4-5"},
		{"gpt-4o", "gpt-4o"},           // no alias
		{"SONNET", "claude-sonnet-4-5-20250929"}, // case insensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := router.ExpandAlias(tt.input)
			if got != tt.want {
				t.Errorf("ExpandAlias(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRouterListAllModels(t *testing.T) {
	config := DefaultRouterConfig()
	router := NewRouter(config)

	anthropic := &mockBackend{name: "anthropic"}
	codex := &mockBackend{name: "codex"}
	router.Register("anthropic", anthropic)
	router.Register("codex", codex)

	models := router.ListAllModels(context.Background())
	if len(models) != 2 {
		t.Errorf("expected 2 backends, got %d", len(models))
	}
	if _, ok := models["anthropic"]; !ok {
		t.Error("missing anthropic models")
	}
	if _, ok := models["codex"]; !ok {
		t.Error("missing codex models")
	}

	all := router.AllModels(context.Background())
	if len(all) != 2 {
		t.Errorf("expected 2 models, got %d", len(all))
	}
}
