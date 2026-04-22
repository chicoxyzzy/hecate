package providers

import "sync"

type MutableRegistry struct {
	mu        sync.RWMutex
	providers []Provider
	byName    map[string]Provider
}

func NewMutableRegistry(items ...Provider) *MutableRegistry {
	registry := &MutableRegistry{}
	registry.Replace(items...)
	return registry
}

func (r *MutableRegistry) Replace(items ...Provider) {
	byName := make(map[string]Provider, len(items))
	providersCopy := make([]Provider, 0, len(items))
	for _, provider := range items {
		byName[provider.Name()] = provider
		providersCopy = append(providersCopy, provider)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName = byName
	r.providers = providersCopy
}

func (r *MutableRegistry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	provider, ok := r.byName[name]
	return provider, ok
}

func (r *MutableRegistry) All() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Provider, len(r.providers))
	copy(out, r.providers)
	return out
}
