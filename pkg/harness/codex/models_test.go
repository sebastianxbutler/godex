package codex

import (
	"testing"
)

func TestExpandAlias(t *testing.T) {
	h := New(Config{})
	tests := []struct {
		in, want string
	}{
		{"gpt", "gpt-5.2-codex"},
		{"GPT", "gpt-5.2-codex"},
		{"gpt-mini", "gpt-5-mini-2025-08-07"},
		{"codex", "gpt-5.3-codex"},
		{"codex53", "gpt-5.3-codex"},
		{"unknown", "unknown"},
		{"sonnet", "sonnet"}, // not a codex alias
	}
	for _, tt := range tests {
		got := h.ExpandAlias(tt.in)
		if got != tt.want {
			t.Errorf("ExpandAlias(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandAlias_ExtraAliases(t *testing.T) {
	h := New(Config{ExtraAliases: map[string]string{"mymodel": "gpt-custom"}})
	if got := h.ExpandAlias("mymodel"); got != "gpt-custom" {
		t.Errorf("got %q, want gpt-custom", got)
	}
	// Default still works
	if got := h.ExpandAlias("gpt"); got != "gpt-5.2-codex" {
		t.Errorf("got %q, want gpt-5.2-codex", got)
	}
}

func TestMatchesModel(t *testing.T) {
	h := New(Config{})
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-5.2-codex", true},
		{"gpt-4o", true},
		{"o1-preview", true},
		{"o3-mini", true},
		{"codex-something", true},
		{"gpt", true},       // alias key
		{"codex53", true},   // alias key
		{"claude-sonnet", false},
		{"unknown", false},
	}
	for _, tt := range tests {
		got := h.MatchesModel(tt.model)
		if got != tt.want {
			t.Errorf("MatchesModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestMatchesModel_ExtraPrefixes(t *testing.T) {
	h := New(Config{ExtraPrefixes: []string{"custom-"}})
	if !h.MatchesModel("custom-model") {
		t.Error("expected custom-model to match")
	}
	if !h.MatchesModel("gpt-5") {
		t.Error("expected gpt-5 to still match")
	}
}
