// Package router provides model-to-harness routing. It delegates model
// matching and alias expansion to individual harnesses, with optional
// user-level overrides.
package router

import (
	"context"
	"strings"
	"sync"

	"godex/pkg/harness"
)

// Config configures user-level routing overrides.
type Config struct {
	// UserAliases are override aliases that take priority over harness defaults.
	UserAliases map[string]string

	// UserPatterns are override patterns: map[harnessName][]prefix.
	UserPatterns map[string][]string
}

// Router selects the appropriate harness based on model name.
type Router struct {
	harnesses []registeredHarness // ordered
	config    Config
	mu        sync.RWMutex
}

type registeredHarness struct {
	name    string
	harness harness.Harness
}

// New creates a new router with the given configuration.
func New(cfg Config) *Router {
	return &Router{
		config: cfg,
	}
}

// Register adds a harness to the router under the given name.
func (r *Router) Register(name string, h harness.Harness) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.harnesses = append(r.harnesses, registeredHarness{name: name, harness: h})
}

// ExpandAlias expands a model alias to its full name.
// Checks user aliases first, then asks each harness.
func (r *Router) ExpandAlias(model string) string {
	if r.config.UserAliases != nil {
		if full, ok := r.config.UserAliases[strings.ToLower(model)]; ok {
			return full
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rh := range r.harnesses {
		expanded := rh.harness.ExpandAlias(model)
		if expanded != model {
			return expanded
		}
	}
	return model
}

// HarnessFor returns the appropriate harness for the given model.
// Checks user patterns first, then asks each harness MatchesModel().
func (r *Router) HarnessFor(model string) harness.Harness {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(model)

	// Check user pattern overrides first
	if r.config.UserPatterns != nil {
		for harnessName, patterns := range r.config.UserPatterns {
			for _, pattern := range patterns {
				pattern = strings.ToLower(pattern)
				if lower == pattern || strings.HasPrefix(lower, pattern) {
					for _, rh := range r.harnesses {
						if rh.name == harnessName {
							return rh.harness
						}
					}
				}
			}
		}
	}

	// Ask each harness
	for _, rh := range r.harnesses {
		if rh.harness.MatchesModel(model) {
			return rh.harness
		}
	}

	// Fallback to first registered harness
	if len(r.harnesses) > 0 {
		return r.harnesses[0].harness
	}

	return nil
}

// Get returns a harness by name.
func (r *Router) Get(name string) harness.Harness {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rh := range r.harnesses {
		if rh.name == name {
			return rh.harness
		}
	}
	return nil
}

// List returns all registered harness names.
func (r *Router) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, len(r.harnesses))
	for i, rh := range r.harnesses {
		names[i] = rh.name
	}
	return names
}

// ListAllModels queries all registered harnesses for their available models.
func (r *Router) ListAllModels(ctx context.Context) map[string][]harness.ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]harness.ModelInfo)
	for _, rh := range r.harnesses {
		models, err := rh.harness.ListModels(ctx)
		if err == nil && len(models) > 0 {
			result[rh.name] = models
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
