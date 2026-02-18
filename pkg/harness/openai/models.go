package openai

import (
	"context"
	"strings"

	"godex/pkg/harness"
)

// ExpandAlias expands a model alias to its full name.
func (h *Harness) ExpandAlias(alias string) string {
	if h.aliases == nil {
		return alias
	}
	lower := strings.ToLower(alias)
	if full, ok := h.aliases[lower]; ok {
		return full
	}
	for k, v := range h.aliases {
		if strings.ToLower(k) == lower {
			return v
		}
	}
	return alias
}

// MatchesModel returns true if this harness handles the given model.
func (h *Harness) MatchesModel(model string) bool {
	lower := strings.ToLower(model)
	if h.aliases != nil {
		if _, ok := h.aliases[lower]; ok {
			return true
		}
		for k, v := range h.aliases {
			if strings.ToLower(k) == lower || strings.ToLower(v) == lower {
				return true
			}
		}
	}
	for _, prefix := range h.prefixes {
		if strings.HasPrefix(lower, strings.ToLower(prefix)) {
			return true
		}
	}
	return false
}

// listModelsWithDiscovery tries API discovery, falls back to nil.
func (h *Harness) listModelsWithDiscovery(ctx context.Context) ([]harness.ModelInfo, error) {
	if h.client != nil {
		models, err := h.client.ListModels(ctx)
		if err == nil {
			return models, nil
		}
	}
	return []harness.ModelInfo{}, nil
}
