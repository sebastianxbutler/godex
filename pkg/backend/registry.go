package backend

import (
	"fmt"
	"sync"

	"godex/pkg/config"
)

// Registry manages available backends and routes requests.
type Registry struct {
	mu       sync.RWMutex
	backends map[string]Backend
	router   *Router
}

// NewRegistry creates a new backend registry from config.
// The backendFactory is called for each custom backend to create the Backend instance.
func NewRegistry(cfg config.BackendsConfig, backendFactory func(name string, bcfg config.CustomBackendConfig) (Backend, error)) (*Registry, error) {
	r := &Registry{
		backends: make(map[string]Backend),
	}

	// Register custom backends
	for name, bcfg := range cfg.Custom {
		if !bcfg.IsEnabled() {
			continue
		}
		if backendFactory == nil {
			return nil, fmt.Errorf("backend factory required for custom backends")
		}
		b, err := backendFactory(name, bcfg)
		if err != nil {
			return nil, fmt.Errorf("create backend %s: %w", name, err)
		}
		r.backends[name] = b
	}

	return r, nil
}

// Register adds a backend to the registry.
func (r *Registry) Register(name string, b Backend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[name] = b
}

// Get returns a backend by name.
func (r *Registry) Get(name string) (Backend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	return b, ok
}

// SetRouter sets the router for model-based routing.
func (r *Registry) SetRouter(router *Router) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.router = router
}

// Route finds the backend for a given model.
func (r *Registry) Route(model string) (Backend, string, error) {
	r.mu.RLock()
	router := r.router
	r.mu.RUnlock()

	if router == nil {
		return nil, "", fmt.Errorf("no router configured")
	}

	// Expand alias first
	expanded := router.ExpandAlias(model)
	
	// Find backend
	b := router.BackendFor(expanded)
	if b == nil {
		return nil, "", fmt.Errorf("no backend for model: %s", model)
	}
	
	return b, expanded, nil
}

// List returns the names of all registered backends.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	names := make([]string, 0, len(r.backends))
	for name := range r.backends {
		names = append(names, name)
	}
	return names
}

// All returns a map of all backends.
func (r *Registry) All() map[string]Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	result := make(map[string]Backend, len(r.backends))
	for k, v := range r.backends {
		result[k] = v
	}
	return result
}
