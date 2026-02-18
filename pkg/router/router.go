// Package router provides model-to-harness routing. It maps model names
// (or aliases/prefixes) to the appropriate harness.Harness implementation.
package router

import (
	"context"
	"strings"
	"sync"

	"godex/pkg/harness"
)

// Config configures model-to-harness routing.
type Config struct {
	// Patterns maps harness names to model prefixes/aliases.
	// Example: {"claude": ["claude-", "sonnet", "opus"], "codex": ["gpt-", "o1-"]}
	Patterns map[string][]string

	// Aliases maps short names to full model names.
	// Example: {"sonnet": "claude-sonnet-4-5-20250929"}
	Aliases map[string]string

	// Default is the fallback harness name when no pattern matches.
	Default string
}

// Router selects the appropriate harness based on model name.
type Router struct {
	harnesses map[string]harness.Harness
	config    Config
	mu        sync.RWMutex
}

// New creates a new router with the given configuration.
func New(cfg Config) *Router {
	return &Router{
		harnesses: make(map[string]harness.Harness),
		config:    cfg,
	}
}

// Register adds a harness to the router under the given name.
func (r *Router) Register(name string, h harness.Harness) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.harnesses[name] = h
}

// ExpandAlias expands a model alias to its full name.
// Returns the original model if no alias exists.
func (r *Router) ExpandAlias(model string) string {
	if full, ok := r.config.Aliases[strings.ToLower(model)]; ok {
		return full
	}
	return model
}

// HarnessFor returns the appropriate harness for the given model.
// Returns nil if no matching harness is found.
func (r *Router) HarnessFor(model string) harness.Harness {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(model)

	for harnessName, patterns := range r.config.Patterns {
		for _, pattern := range patterns {
			pattern = strings.ToLower(pattern)
			if lower == pattern || strings.HasPrefix(lower, pattern) {
				if h, ok := r.harnesses[harnessName]; ok {
					return h
				}
			}
		}
	}

	if r.config.Default != "" {
		if h, ok := r.harnesses[r.config.Default]; ok {
			return h
		}
	}

	return nil
}

// Get returns a harness by name.
func (r *Router) Get(name string) harness.Harness {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.harnesses[name]
}

// List returns all registered harness names.
func (r *Router) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.harnesses))
	for name := range r.harnesses {
		names = append(names, name)
	}
	return names
}

// ListAllModels queries all registered harnesses for their available models.
func (r *Router) ListAllModels(ctx context.Context) map[string][]harness.ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]harness.ModelInfo)
	for name, h := range r.harnesses {
		models, err := h.ListModels(ctx)
		if err == nil && len(models) > 0 {
			result[name] = models
		}
	}
	return result
}

// AllModels returns a flat list of all models from all harnesses.
func (r *Router) AllModels(ctx context.Context) []harness.ModelInfo {
	modelMap := r.ListAllModels(ctx)
	var all []harness.ModelInfo
	for _, models := range modelMap {
		all = append(all, models...)
	}
	return all
}

// DefaultConfig returns a default routing configuration.
func DefaultConfig() Config {
	return Config{
		Patterns: map[string][]string{
			"claude": {"claude-", "sonnet", "opus", "haiku"},
			"codex":  {"gpt-", "o1-", "o3-", "codex-"},
		},
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5-20250929",
			"opus":   "claude-opus-4-5",
			"haiku":  "claude-haiku-4-5",
		},
		Default: "codex",
	}
}
