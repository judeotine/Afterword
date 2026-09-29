package integrations

import (
	"context"
	"sync"
)

type FakeProvider struct {
	name string
	mu   sync.Mutex
	sent []Delivery
	err  error
}

func NewFakeProvider(name string) *FakeProvider {
	resolved := normalizeName(name)
	if resolved == "" {
		resolved = "fake"
	}
	return &FakeProvider{name: resolved}
}

func (f *FakeProvider) Name() string {
	return f.name
}

func (f *FakeProvider) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *FakeProvider) Deliver(_ context.Context, delivery Delivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, delivery)
	return nil
}

func (f *FakeProvider) Delivered() []Delivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Delivery, len(f.sent))
	copy(out, f.sent)
	return out
}
