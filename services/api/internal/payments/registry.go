package payments

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu          sync.RWMutex
	providers   map[string]PaymentProvider
	defaultName string
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]PaymentProvider{}}
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (r *Registry) Register(name string, provider PaymentProvider) error {
	key := normalizeName(name)
	if key == "" {
		return fmt.Errorf("%w: a provider name is required", ErrNotConfigured)
	}
	if provider == nil {
		return fmt.Errorf("%w: %s has no implementation", ErrNotConfigured, key)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[key]; exists {
		return fmt.Errorf("%w: %s", ErrProviderRegistered, key)
	}
	r.providers[key] = provider
	if r.defaultName == "" {
		r.defaultName = key
	}
	return nil
}

func (r *Registry) Lookup(name string) (PaymentProvider, error) {
	key := normalizeName(name)

	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderUnknown, key)
	}
	return provider, nil
}

func (r *Registry) SetDefault(name string) error {
	key := normalizeName(name)

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.providers[key]; !ok {
		return fmt.Errorf("%w: %s", ErrProviderUnknown, key)
	}
	r.defaultName = key
	return nil
}

func (r *Registry) Default() (string, PaymentProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultName == "" {
		return "", nil, fmt.Errorf("%w: no payment provider is registered", ErrProviderUnknown)
	}
	return r.defaultName, r.providers[r.defaultName], nil
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
