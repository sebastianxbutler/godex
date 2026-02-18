package claude

import (
	"context"
	"strings"

	"godex/pkg/harness"
)

var defaultClaudeAliases = map[string]string{
	"sonnet":   "claude-sonnet-4-6",
	"sonnet45": "claude-sonnet-4-5",
	"opus":     "claude-opus-4-6",
	"opus45":   "claude-opus-4-5",
	"haiku":    "claude-haiku-4-5",
}

var defaultClaudePrefixes = []string{"claude-"}

// Exact names that match this harness (beyond prefix).
var defaultClaudeExactMatches = []string{"sonnet", "opus", "haiku"}

func (h *Harness) mergedAliases() map[string]string {
	m := make(map[string]string, len(defaultClaudeAliases))
	for k, v := range defaultClaudeAliases {
		m[k] = v
	}
	if h.extraAliases != nil {
		for k, v := range h.extraAliases {
			m[strings.ToLower(k)] = v
		}
	}
	return m
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
	// Check alias values
	for _, v := range aliases {
		if strings.ToLower(v) == lower {
			return true
		}
	}
	// Check prefixes
	for _, prefix := range defaultClaudePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	// Check exact matches
	for _, exact := range defaultClaudeExactMatches {
		if lower == exact {
			return true
		}
	}
	return false
}

// listModelsWithDiscovery tries API discovery, falls back to nil.
func (h *Harness) listModelsWithDiscovery(ctx context.Context) ([]harness.ModelInfo, error) {
	if h.testClient != nil {
		return h.testClient.ListModels(ctx)
	}
	if h.client != nil {
		return h.client.ListModels(ctx)
	}
	return nil, nil
}
