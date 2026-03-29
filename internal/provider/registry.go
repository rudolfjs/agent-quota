package provider

import (
	"slices"
	"sync"
)

// Registry holds all registered provider implementations.
// Providers are registered at startup in main.go and looked up at runtime.
// A RWMutex protects concurrent access in case providers are ever registered
// or queried from multiple goroutines.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds or replaces a provider. The provider's Name() is the key.
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Get returns the provider with the given name, or false if not found.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// Available returns all providers whose Available() method returns true.
func (r *Registry) Available() []Provider {
	r.mu.RLock()
	all := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		all = append(all, p)
	}
	r.mu.RUnlock()

	var result []Provider
	for _, p := range all {
		if p.Available() {
			result = append(result, p)
		}
	}
	slices.SortFunc(result, func(a, b Provider) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})
	return result
}

// All returns every registered provider regardless of availability.
func (r *Registry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	slices.SortFunc(result, func(a, b Provider) int {
		if a.Name() < b.Name() {
			return -1
		}
		if a.Name() > b.Name() {
			return 1
		}
		return 0
	})
	return result
}
