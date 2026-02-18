package claude

import "testing"

func TestExpandAlias(t *testing.T) {
	h := New(Config{})
	cases := []struct {
		in   string
		want string
	}{
		{"sonnet", "claude-sonnet-4-6"},
		{"SONNET", "claude-sonnet-4-6"},
		{"sonnet45", "claude-sonnet-4-5"},
		{"opus", "claude-opus-4-6"},
		{"opus45", "claude-opus-4-5"},
		{"haiku", "claude-haiku-4-5"},
		{"unknown", "unknown"},
		{"gpt", "gpt"},
	}
	for _, tt := range cases {
		if got := h.ExpandAlias(tt.in); got != tt.want {
			t.Errorf("ExpandAlias(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandAlias_ExtraAliases(t *testing.T) {
	h := New(Config{ExtraAliases: map[string]string{"myalias": "claude-custom"}})
	if got := h.ExpandAlias("myalias"); got != "claude-custom" {
		t.Errorf("got %q, want claude-custom", got)
	}
	if got := h.ExpandAlias("sonnet"); got != "claude-sonnet-4-6" {
		t.Errorf("got %q, want claude-sonnet-4-6", got)
	}
}

func TestMatchesModel(t *testing.T) {
	h := New(Config{})
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-5", true},
		{"claude-haiku-4-5", true},
		{"sonnet", true},
		{"opus", true},
		{"haiku", true},
		{"SONNET", true},
		{"gpt-5", false},
		{"unknown", false},
	}
	for _, tt := range cases {
		if got := h.MatchesModel(tt.model); got != tt.want {
			t.Errorf("MatchesModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestMatchesModel_AliasValues(t *testing.T) {
	h := New(Config{})
	if !h.MatchesModel("claude-sonnet-4-6") {
		t.Error("expected alias value to match")
	}
}

func TestMatchesModel_ExtraAliases(t *testing.T) {
	h := New(Config{ExtraAliases: map[string]string{"tiny": "claude-tiny"}})
	if !h.MatchesModel("tiny") {
		t.Error("expected extra alias key to match")
	}
	if !h.MatchesModel("claude-tiny") {
		t.Error("expected extra alias value to match")
	}
}

func TestMatchesModel_Prefix(t *testing.T) {
	h := New(Config{})
	if !h.MatchesModel("claude-something") {
		t.Error("expected claude- prefix to match")
	}
}

func TestExpandAlias_ExtraOverridesDefault(t *testing.T) {
	h := New(Config{ExtraAliases: map[string]string{"sonnet": "custom-sonnet"}})
	if got := h.ExpandAlias("sonnet"); got != "custom-sonnet" {
		t.Errorf("got %q, want custom-sonnet", got)
	}
}
