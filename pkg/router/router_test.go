package router

import (
	"context"
	"strings"
	"testing"

	"godex/pkg/harness"
)

// stubHarness is a minimal harness for testing routing.
type stubHarness struct {
	name     string
	models   []harness.ModelInfo
	aliases  map[string]string
	prefixes []string
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
func (s *stubHarness) ExpandAlias(alias string) string {
	if s.aliases != nil {
		if full, ok := s.aliases[alias]; ok {
			return full
		}
		if full, ok := s.aliases[strings.ToLower(alias)]; ok {
			return full
		}
	}
	return alias
}
func (s *stubHarness) MatchesModel(model string) bool {
	lower := strings.ToLower(model)
	if s.aliases != nil {
		if _, ok := s.aliases[model]; ok {
			return true
		}
		if _, ok := s.aliases[lower]; ok {
			return true
		}
		for k, v := range s.aliases {
			if strings.ToLower(k) == lower || strings.ToLower(v) == lower {
				return true
			}
		}
	}
	for _, p := range s.prefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func TestHarnessFor_DelegatesToHarness(t *testing.T) {
	r := New(Config{})
	codex := &stubHarness{name: "codex", prefixes: []string{"gpt-", "o1-", "o3-", "codex-"}}
	claude := &stubHarness{name: "claude", prefixes: []string{"claude-"}, aliases: map[string]string{"sonnet": "claude-sonnet-4-6"}}
	r.Register("codex", codex)
	r.Register("claude", claude)

	tests := []struct {
		model string
		want  string
	}{
		{"gpt-5.2-codex", "codex"},
		{"o1-preview", "codex"},
		{"o3-mini", "codex"},
		{"claude-sonnet-4-5", "claude"},
		{"claude-opus-4-5", "claude"},
		{"sonnet", "claude"},
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

func TestHarnessFor_UserPatternOverride(t *testing.T) {
	r := New(Config{
		UserPatterns: map[string][]string{
			"custom": {"gpt-"},
		},
	})
	codex := &stubHarness{name: "codex", prefixes: []string{"gpt-"}}
	custom := &stubHarness{name: "custom"}
	r.Register("codex", codex)
	r.Register("custom", custom)

	h := r.HarnessFor("gpt-5.2-codex")
	if h == nil || h.Name() != "custom" {
		t.Errorf("expected user pattern override to custom, got %v", h)
	}
}

func TestExpandAlias_UserOverride(t *testing.T) {
	r := New(Config{
		UserAliases: map[string]string{
			"sonnet": "my-custom-sonnet",
		},
	})
	claude := &stubHarness{name: "claude", aliases: map[string]string{"sonnet": "claude-sonnet-4-6"}}
	r.Register("claude", claude)

	got := r.ExpandAlias("sonnet")
	if got != "my-custom-sonnet" {
		t.Errorf("ExpandAlias(sonnet) = %q, want my-custom-sonnet", got)
	}
}

func TestExpandAlias_DelegatesToHarness(t *testing.T) {
	r := New(Config{})
	claude := &stubHarness{name: "claude", aliases: map[string]string{"sonnet": "claude-sonnet-4-6"}}
	r.Register("claude", claude)

	got := r.ExpandAlias("sonnet")
	if got != "claude-sonnet-4-6" {
		t.Errorf("ExpandAlias(sonnet) = %q, want claude-sonnet-4-6", got)
	}
}

func TestHarnessFor_NoMatch(t *testing.T) {
	r := New(Config{})
	codex := &stubHarness{name: "codex"}
	r.Register("codex", codex)

	h := r.HarnessFor("unknown-model-xyz")
	if h != nil {
		t.Errorf("expected nil for unknown model, got %v", h.Name())
	}
}

func TestExpandAlias_NoAlias(t *testing.T) {
	r := New(Config{})
	got := r.ExpandAlias("unknown")
	if got != "unknown" {
		t.Errorf("ExpandAlias(unknown) = %q, want unknown", got)
	}
}

func TestAllModels(t *testing.T) {
	r := New(Config{})
	r.Register("a", &stubHarness{name: "a", models: []harness.ModelInfo{{ID: "m1"}}})
	r.Register("b", &stubHarness{name: "b", models: []harness.ModelInfo{{ID: "m2"}, {ID: "m3"}}})

	all := r.AllModels(context.Background())
	if len(all) != 3 {
		t.Errorf("AllModels() returned %d models, want 3", len(all))
	}
}

func TestList(t *testing.T) {
	r := New(Config{})
	r.Register("a", &stubHarness{name: "a"})
	r.Register("b", &stubHarness{name: "b"})

	names := r.List()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("List() = %v, want [a b]", names)
	}
}

func TestMultiHarness_FirstMatchWins(t *testing.T) {
	r := New(Config{})
	// Both match "gpt-" but first registered wins
	h1 := &stubHarness{name: "first", prefixes: []string{"gpt-"}}
	h2 := &stubHarness{name: "second", prefixes: []string{"gpt-"}}
	r.Register("first", h1)
	r.Register("second", h2)

	h := r.HarnessFor("gpt-5")
	if h == nil || h.Name() != "first" {
		t.Errorf("expected first, got %v", h)
	}
}
