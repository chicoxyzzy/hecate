package catalog

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/hecate/agent-runtime/internal/providers"
)

type RegistryCatalog struct {
	registry         providers.Registry
	healthTracker    providers.HealthTracker
	selfListenAddr   string
}

type baseURLer interface {
	BaseURL() string
}

func NewRegistryCatalog(registry providers.Registry, healthTracker providers.HealthTracker) *RegistryCatalog {
	return &RegistryCatalog{registry: registry, healthTracker: healthTracker}
}

func NewRegistryCatalogWithSelfAddr(registry providers.Registry, healthTracker providers.HealthTracker, selfListenAddr string) *RegistryCatalog {
	return &RegistryCatalog{registry: registry, healthTracker: healthTracker, selfListenAddr: selfListenAddr}
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
	if e, ok := provider.(providers.Enabler); ok && !e.Enabled() {
		return Entry{
			Provider:        provider,
			Name:            provider.Name(),
			Kind:            provider.Kind(),
			DiscoverySource: "control_plane",
			Healthy:         false,
			Status:          "disabled",
		}
	}

	if c.selfListenAddr != "" {
		if bup, ok := provider.(baseURLer); ok {
			if isSelfReferentialURL(c.selfListenAddr, bup.BaseURL()) {
				return Entry{
					Provider:        provider,
					Name:            provider.Name(),
					Kind:            provider.Kind(),
					DiscoverySource: "self_referential",
					Healthy:         false,
					Status:          "degraded",
					Error:           fmt.Sprintf("provider base URL %q points to the gateway's own address — run the local provider on a different port", bup.BaseURL()),
				}
			}
		}
	}

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

// isSelfReferentialURL returns true when providerBaseURL points to the same
// loopback port that the gateway is listening on.
func isSelfReferentialURL(selfListenAddr, providerBaseURL string) bool {
	if selfListenAddr == "" || providerBaseURL == "" {
		return false
	}

	_, selfPort, err := net.SplitHostPort(selfListenAddr)
	if err != nil || selfPort == "" {
		return false
	}

	u, err := url.Parse(providerBaseURL)
	if err != nil {
		return false
	}

	providerHost, providerPort, err := net.SplitHostPort(u.Host)
	if err != nil {
		return false
	}

	if providerPort != selfPort {
		return false
	}

	ip := net.ParseIP(providerHost)
	if ip != nil {
		return ip.IsLoopback()
	}
	return providerHost == "localhost"
}
