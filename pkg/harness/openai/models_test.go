package openai

import "testing"

func TestExpandAlias(t *testing.T) {
	h := New(Config{Aliases: map[string]string{"mini": "gpt-mini"}})
	if got := h.ExpandAlias("mini"); got != "gpt-mini" {
		t.Errorf("got %q, want gpt-mini", got)
	}
	if got := h.ExpandAlias("unknown"); got != "unknown" {
		t.Errorf("got %q, want unknown", got)
	}
}

func TestMatchesModel(t *testing.T) {
	h := New(Config{Aliases: map[string]string{"mini": "gpt-mini"}, Prefixes: []string{"gpt-", "custom-"}})
	tests := []struct {
		model string
		want  bool
	}{
		{"mini", true},
		{"gpt-mini", true},
		{"gpt-4o", true},
		{"custom-model", true},
		{"other", false},
	}
	for _, tt := range tests {
		got := h.MatchesModel(tt.model)
		if got != tt.want {
			t.Errorf("MatchesModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestMatchesModel_NoConfig(t *testing.T) {
	h := New(Config{})
	if h.MatchesModel("gpt-4o") {
		t.Error("expected no match when no prefixes or aliases configured")
	}
}
