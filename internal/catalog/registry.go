package catalog

import (
	"context"
	"time"

	"github.com/hecate/agent-runtime/internal/providers"
)

type RegistryCatalog struct {
	registry      providers.Registry
	healthTracker providers.HealthTracker
}

func NewRegistryCatalog(registry providers.Registry, healthTracker providers.HealthTracker) *RegistryCatalog {
	return &RegistryCatalog{registry: registry, healthTracker: healthTracker}
}

func (c *RegistryCatalog) Snapshot(ctx context.Context) []Entry {
	items := c.registry.All()
	out := make([]Entry, 0, len(items))
	for _, provider := range items {
		out = append(out, c.entryForProvider(ctx, provider))
	}
	return out
}

func (c *RegistryCatalog) Get(ctx context.Context, name string) (Entry, bool) {
	provider, ok := c.registry.Get(name)
	if !ok {
		return Entry{}, false
	}
	return c.entryForProvider(ctx, provider), true
}

func (c *RegistryCatalog) entryForProvider(ctx context.Context, provider providers.Provider) Entry {
	caps, err := provider.Capabilities(ctx)

	defaultModel := caps.DefaultModel
	if defaultModel == "" {
		defaultModel = provider.DefaultModel()
	}

	models := append([]string(nil), caps.Models...)
	if len(models) == 0 && defaultModel != "" {
		models = []string{defaultModel}
	}

	discoverySource := caps.DiscoverySource
	if discoverySource == "" {
		discoverySource = "provider_default"
	}

	entry := Entry{
		Provider:        provider,
		Name:            provider.Name(),
		Kind:            provider.Kind(),
		DefaultModel:    defaultModel,
		Models:          models,
		DiscoverySource: discoverySource,
		Healthy:         err == nil,
		Status:          "healthy",
	}
	if !caps.RefreshedAt.IsZero() {
		entry.RefreshedAt = caps.RefreshedAt.UTC().Format(time.RFC3339)
	}
	if err != nil {
		entry.Healthy = false
		entry.Status = "degraded"
		entry.Error = err.Error()
	}

	if c.healthTracker != nil {
		state := c.healthTracker.State(provider.Name())
		if !state.Available {
			entry.Healthy = false
			entry.Status = string(state.Status)
			entry.Error = providers.FormatHealthStateError(provider.Name(), state)
		} else if state.Status != "" {
			entry.Status = string(state.Status)
		}
	}

	return entry
}
