package aliases

import (
	"context"
	"testing"
)

func TestPickLatest(t *testing.T) {
	models := []ModelInfo{
		{ID: "claude-opus-4-5"},
		{ID: "claude-opus-4-6"},
		{ID: "claude-opus-4-5-20250929"},
		{ID: "claude-sonnet-4-5-20250929"},
	}

	got := pickLatest(models, "claude-opus-", "", nil)
	if got != "claude-opus-4-6" {
		t.Errorf("pickLatest = %q, want %q", got, "claude-opus-4-6")
	}

	got = pickLatest(models, "claude-sonnet-", "", nil)
	if got != "claude-sonnet-4-5-20250929" {
		t.Errorf("pickLatest = %q, want %q", got, "claude-sonnet-4-5-20250929")
	}

	got = pickLatest(models, "nonexistent-", "", nil)
	if got != "" {
		t.Errorf("pickLatest = %q, want empty", got)
	}
}

func TestPickLatestExactMatch(t *testing.T) {
	models := []ModelInfo{
		{ID: "gemini-2.5-pro"},
		{ID: "gemini-2.5-flash"},
	}
	got := pickLatest(models, "gemini-2.5-pro", "", nil)
	if got != "gemini-2.5-pro" {
		t.Errorf("pickLatest exact = %q, want %q", got, "gemini-2.5-pro")
	}
}

func TestPickLatestWithSuffix(t *testing.T) {
	models := []ModelInfo{
		{ID: "gpt-4o"},
		{ID: "gpt-4o-mini"},
		{ID: "gpt-5.2-codex"},
		{ID: "gpt-5.3-codex"},
		{ID: "gpt-5.3"},
	}
	got := pickLatest(models, "gpt-", "-codex", nil)
	if got != "gpt-5.3-codex" {
		t.Errorf("pickLatest with suffix = %q, want %q", got, "gpt-5.3-codex")
	}
	got = pickLatest(models, "gpt-", "", nil)
	if got != "gpt-5.3-codex" {
		t.Errorf("pickLatest no suffix = %q, want %q", got, "gpt-5.3-codex")
	}
}

func TestApplyResolutions(t *testing.T) {
	aliases := map[string]string{
		"opus":   "claude-opus-4-5",
		"sonnet": "claude-sonnet-4-5-20250929",
	}
	resolutions := []Resolution{
		{Alias: "opus", Resolved: "claude-opus-4-6", Changed: true},
		{Alias: "sonnet", Resolved: "claude-sonnet-4-5-20250929"},
		{Alias: "haiku", Resolved: "", Error: "no models"},
	}
	n := ApplyResolutions(aliases, resolutions)
	if n != 1 {
		t.Errorf("ApplyResolutions = %d, want 1", n)
	}
	if aliases["opus"] != "claude-opus-4-6" {
		t.Errorf("opus = %q, want claude-opus-4-6", aliases["opus"])
	}
}

func TestResolveBackendNotAvailable(t *testing.T) {
	results := Resolve(context.Background(), map[string]ModelLister{}, nil, []Rule{
		{Alias: "opus", Prefix: "claude-opus-", Backend: "anthropic"},
	})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error for missing backend")
	}
}

func TestDefaultRules(t *testing.T) {
	rules := DefaultRules()
	if len(rules) == 0 {
		t.Fatal("expected non-empty default rules")
	}
	for _, r := range rules {
		if r.Alias == "" || r.Prefix == "" || r.Backend == "" {
			t.Errorf("incomplete rule: %+v", r)
		}
	}
}
