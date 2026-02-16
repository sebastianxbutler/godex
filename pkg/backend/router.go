package backend

import (
	"context"
	"strings"
	"sync"
)

// RouterConfig configures model-to-backend routing.
type RouterConfig struct {
	// Patterns maps backend names to model prefixes/aliases.
	// Example: {"anthropic": ["claude-", "sonnet", "opus"], "codex": ["gpt-", "o1-"]}
	Patterns map[string][]string

	// Aliases maps short names to full model names.
	// Example: {"sonnet": "claude-sonnet-4-5-20250929"}
	Aliases map[string]string

	// Default is the fallback backend name when no pattern matches.
	Default string
}

// Router selects the appropriate backend based on model name.
type Router struct {
	backends map[string]Backend
	config   RouterConfig
	mu       sync.RWMutex
}

// NewRouter creates a new router with the given backends and configuration.
func NewRouter(config RouterConfig) *Router {
	return &Router{
		backends: make(map[string]Backend),
		config:   config,
	}
}

// Register adds a backend to the router.
func (r *Router) Register(name string, b Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[name] = b
}

// ExpandAlias expands a model alias to its full name.
// Returns the original model if no alias exists.
func (r *Router) ExpandAlias(model string) string {
	if full, ok := r.config.Aliases[strings.ToLower(model)]; ok {
		return full
	}
	return model
}

// BackendFor returns the appropriate backend for the given model.
// Returns nil if no matching backend is found.
func (r *Router) BackendFor(model string) Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()

	model = strings.ToLower(model)

	// Check each backend's patterns
	for backendName, patterns := range r.config.Patterns {
		for _, pattern := range patterns {
			pattern = strings.ToLower(pattern)
			// Exact match or prefix match
			if model == pattern || strings.HasPrefix(model, pattern) {
				if b, ok := r.backends[backendName]; ok {
					return b
				}
			}
		}
	}

	// Fall back to default
	if r.config.Default != "" {
		if b, ok := r.backends[r.config.Default]; ok {
			return b
		}
	}

	return nil
}

// Get returns a backend by name.
func (r *Router) Get(name string) Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.backends[name]
}

// List returns all registered backend names.
func (r *Router) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.backends))
	for name := range r.backends {
		names = append(names, name)
	}
	return names
}

// DefaultRouterConfig returns a default routing configuration.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		Patterns: map[string][]string{
			"anthropic": {"claude-", "sonnet", "opus", "haiku"},
			"codex":     {"gpt-", "o1-", "o3-", "codex-"},
		},
		Aliases: map[string]string{
			"sonnet": "claude-sonnet-4-5-20250929",
			"opus":   "claude-opus-4-5",
			"haiku":  "claude-haiku-4-5",
		},
		Default: "codex",
	}
}

// ListAllModels queries all registered backends for their available models.
// Returns a map of backend name to model list.
func (r *Router) ListAllModels(ctx context.Context) map[string][]ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]ModelInfo)
	for name, b := range r.backends {
		models, err := b.ListModels(ctx)
		if err == nil && len(models) > 0 {
			result[name] = models
		}
	}
	return result
}

// AllModels returns a flat list of all models from all backends.
func (r *Router) AllModels(ctx context.Context) []ModelInfo {
	modelMap := r.ListAllModels(ctx)
	var all []ModelInfo
	for _, models := range modelMap {
		all = append(all, models...)
	}
	return all
}
