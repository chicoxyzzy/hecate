package catalog

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

type fakeProvider struct {
	name         string
	kind         providers.Kind
	defaultModel string
	caps         providers.Capabilities
	capsErr      error
}

func (p *fakeProvider) Name() string         { return p.name }
func (p *fakeProvider) Kind() providers.Kind { return p.kind }
func (p *fakeProvider) DefaultModel() string { return p.defaultModel }
func (p *fakeProvider) Capabilities(context.Context) (providers.Capabilities, error) {
	if p.caps.Name != "" || len(p.caps.Models) > 0 || p.caps.DefaultModel != "" || !p.caps.RefreshedAt.IsZero() {
		return p.caps, p.capsErr
	}
	return providers.Capabilities{
		Name:         p.name,
		Kind:         p.kind,
		DefaultModel: p.defaultModel,
		Models:       []string{p.defaultModel},
	}, p.capsErr
}
func (p *fakeProvider) Chat(context.Context, types.ChatRequest) (*types.ChatResponse, error) {
	return nil, nil
}
func (p *fakeProvider) Supports(string) bool { return true }

func TestRegistryCatalogSnapshotIncludesHealthAndCapabilities(t *testing.T) {
	t.Parallel()

	registry := providers.NewRegistry(
		&fakeProvider{
			name:         "openai",
			kind:         providers.KindCloud,
			defaultModel: "gpt-4o-mini",
			caps: providers.Capabilities{
				Name:            "openai",
				Kind:            providers.KindCloud,
				DefaultModel:    "gpt-4o-mini",
				Models:          []string{"gpt-4o-mini", "gpt-4.1-mini"},
				DiscoverySource: "upstream_v1_models",
				RefreshedAt:     time.Unix(100, 0).UTC(),
			},
		},
	)
	tracker := providers.NewMemoryHealthTracker(1, time.Minute)
	tracker.RecordFailure("openai", context.DeadlineExceeded)

	cat := NewRegistryCatalog(registry, tracker)
	entries := cat.Snapshot(context.Background())
	if len(entries) != 1 {
		t.Fatalf("Snapshot() len = %d, want 1", len(entries))
	}
	if entries[0].Name != "openai" {
		t.Fatalf("entry name = %q, want openai", entries[0].Name)
	}
	if entries[0].Healthy {
		t.Fatal("entry healthy = true, want false from health tracker")
	}
	if entries[0].DiscoverySource != "upstream_v1_models" {
		t.Fatalf("discovery source = %q, want upstream_v1_models", entries[0].DiscoverySource)
	}
}
