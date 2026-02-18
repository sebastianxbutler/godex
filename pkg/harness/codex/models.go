package codex

import (
	"context"
	"os"
	"strings"

	"godex/pkg/harness"
)

var defaultCodexAliases = map[string]string{
	"gpt":       "gpt-5.2-codex",
	"gpt-mini":  "gpt-5-mini-2025-08-07",
	"gpt-pro":   "gpt-5.2-pro",
	"codex":     "gpt-5.3-codex",
	"codex-mini": "gpt-5.1-codex-mini",
	"codex53":   "gpt-5.3-codex",
}

var defaultCodexPrefixes = []string{"gpt-", "o1-", "o3-", "codex-"}

var defaultCodexModels = []harness.ModelInfo{
	{ID: "gpt-5.3-codex", Name: "GPT 5.3 Codex", Provider: "codex"},
	{ID: "gpt-5.2-codex", Name: "GPT 5.2 Codex", Provider: "codex"},
	{ID: "gpt-5.2-pro", Name: "GPT 5.2 Pro", Provider: "codex"},
	{ID: "gpt-5-mini", Name: "GPT 5 Mini", Provider: "codex"},
	{ID: "o3", Name: "o3", Provider: "codex"},
	{ID: "o3-mini", Name: "o3 Mini", Provider: "codex"},
	{ID: "o1-pro", Name: "o1 Pro", Provider: "codex"},
	{ID: "o1", Name: "o1", Provider: "codex"},
	{ID: "o1-mini", Name: "o1 Mini", Provider: "codex"},
}

func (h *Harness) mergedAliases() map[string]string {
	m := make(map[string]string, len(defaultCodexAliases))
	for k, v := range defaultCodexAliases {
		m[k] = v
	}
	if h.extraAliases != nil {
		for k, v := range h.extraAliases {
			m[strings.ToLower(k)] = v
		}
	}
	return m
}

func (h *Harness) mergedPrefixes() []string {
	if len(h.extraPrefixes) == 0 {
		return defaultCodexPrefixes
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range defaultCodexPrefixes {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	for _, p := range h.extraPrefixes {
		lp := strings.ToLower(p)
		if !seen[lp] {
			seen[lp] = true
			out = append(out, lp)
		}
	}
	return out
}

// ExpandAlias expands a model alias to its full name.
func (h *Harness) ExpandAlias(alias string) string {
	lower := strings.ToLower(alias)
	aliases := h.mergedAliases()
	if full, ok := aliases[lower]; ok {
		return full
	}
	return alias
}

// MatchesModel returns true if this harness handles the given model.
func (h *Harness) MatchesModel(model string) bool {
	lower := strings.ToLower(model)
	// Check alias keys
	aliases := h.mergedAliases()
	if _, ok := aliases[lower]; ok {
		return true
	}
	// Check prefixes
	for _, prefix := range h.mergedPrefixes() {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// ListModels returns available Codex models.
// If the client is available, it tries API discovery first.
func (h *Harness) listModelsWithDiscovery(ctx context.Context) ([]harness.ModelInfo, error) {
	if h.client != nil && os.Getenv("OPENAI_API_KEY") != "" {
		models, err := h.client.ListModels(ctx)
		if err == nil && len(models) > 0 {
			return models, nil
		}
	}
	return defaultCodexModels, nil
}
