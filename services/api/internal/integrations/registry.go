package integrations

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("%w: missing implementation", ErrNotConfigured)
	}
	key := normalizeName(provider.Name())
	if key == "" {
		return fmt.Errorf("%w: a provider name is required", ErrNotConfigured)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[key]; exists {
		return fmt.Errorf("%w: %s", ErrProviderExists, key)
	}
	r.providers[key] = provider
	return nil
}

func (r *Registry) Lookup(name string) (Provider, error) {
	key := normalizeName(name)
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.providers[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, key)
	}
	return provider, nil
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

func (r *Registry) DeliverTo(ctx context.Context, name string, delivery Delivery) error {
	if err := delivery.Validate(); err != nil {
		return err
	}
	provider, err := r.Lookup(name)
	if err != nil {
		return err
	}
	return provider.Deliver(ctx, delivery)
}
