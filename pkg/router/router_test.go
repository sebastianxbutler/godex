package router

import (
	"context"
	"testing"

	"godex/pkg/harness"
)

// stubHarness is a minimal harness for testing routing.
type stubHarness struct {
	name   string
	models []harness.ModelInfo
}

func (s *stubHarness) Name() string { return s.name }
func (s *stubHarness) StreamTurn(ctx context.Context, turn *harness.Turn, onEvent func(harness.Event) error) error {
	return nil
}
func (s *stubHarness) StreamAndCollect(ctx context.Context, turn *harness.Turn) (*harness.TurnResult, error) {
	return &harness.TurnResult{}, nil
}
func (s *stubHarness) RunToolLoop(ctx context.Context, turn *harness.Turn, handler harness.ToolHandler, opts harness.LoopOptions) (*harness.TurnResult, error) {
	return &harness.TurnResult{}, nil
}
func (s *stubHarness) ListModels(ctx context.Context) ([]harness.ModelInfo, error) {
	return s.models, nil
}

func TestHarnessFor_PrefixMatch(t *testing.T) {
	r := New(DefaultConfig())
	codex := &stubHarness{name: "codex"}
	claude := &stubHarness{name: "claude"}
	r.Register("codex", codex)
	r.Register("claude", claude)

	tests := []struct {
		model string
		want  string
	}{
		{"gpt-5.2-codex", "codex"},
		{"gpt-4o", "codex"},
		{"o1-preview", "codex"},
		{"o3-mini", "codex"},
		{"claude-sonnet-4-5-20250929", "claude"},
		{"claude-opus-4-5", "claude"},
	}
	for _, tt := range tests {
		h := r.HarnessFor(tt.model)
		if h == nil {
			t.Errorf("HarnessFor(%q) = nil, want %q", tt.model, tt.want)
			continue
		}
		if h.Name() != tt.want {
			t.Errorf("HarnessFor(%q).Name() = %q, want %q", tt.model, h.Name(), tt.want)
		}
	}
}

func TestHarnessFor_Default(t *testing.T) {
	r := New(DefaultConfig())
	codex := &stubHarness{name: "codex"}
	r.Register("codex", codex)

	h := r.HarnessFor("unknown-model-xyz")
	if h == nil {
		t.Fatal("expected default harness, got nil")
	}
	if h.Name() != "codex" {
		t.Errorf("got %q, want codex", h.Name())
	}
}

func TestHarnessFor_NoMatch(t *testing.T) {
	r := New(Config{Patterns: map[string][]string{"x": {"x-"}}})
	// No harnesses registered
	h := r.HarnessFor("x-model")
	if h != nil {
		t.Errorf("expected nil, got %v", h)
	}
}

func TestExpandAlias(t *testing.T) {
	r := New(DefaultConfig())
	if got := r.ExpandAlias("sonnet"); got != "claude-sonnet-4-5-20250929" {
		t.Errorf("got %q", got)
	}
	if got := r.ExpandAlias("unknown"); got != "unknown" {
		t.Errorf("got %q", got)
	}
	// Case insensitive
	if got := r.ExpandAlias("SONNET"); got != "claude-sonnet-4-5-20250929" {
		t.Errorf("got %q", got)
	}
}

func TestList(t *testing.T) {
	r := New(DefaultConfig())
	r.Register("codex", &stubHarness{name: "codex"})
	r.Register("claude", &stubHarness{name: "claude"})

	names := r.List()
	if len(names) != 2 {
		t.Errorf("got %d names, want 2", len(names))
	}
}

func TestGet(t *testing.T) {
	r := New(DefaultConfig())
	codex := &stubHarness{name: "codex"}
	r.Register("codex", codex)

	if r.Get("codex") != codex {
		t.Error("Get(codex) returned wrong harness")
	}
	if r.Get("missing") != nil {
		t.Error("Get(missing) should be nil")
	}
}

func TestAllModels(t *testing.T) {
	r := New(DefaultConfig())
	r.Register("codex", &stubHarness{
		name:   "codex",
		models: []harness.ModelInfo{{ID: "gpt-5.2-codex"}},
	})
	r.Register("claude", &stubHarness{
		name:   "claude",
		models: []harness.ModelInfo{{ID: "claude-sonnet-4-5"}},
	})

	models := r.AllModels(context.Background())
	if len(models) != 2 {
		t.Errorf("got %d models, want 2", len(models))
	}
}

func TestHarnessFor_ExactMatch(t *testing.T) {
	r := New(Config{
		Patterns: map[string][]string{
			"claude": {"sonnet"},
		},
	})
	claude := &stubHarness{name: "claude"}
	r.Register("claude", claude)

	h := r.HarnessFor("sonnet")
	if h == nil || h.Name() != "claude" {
		t.Errorf("exact match 'sonnet' should route to claude")
	}
}
