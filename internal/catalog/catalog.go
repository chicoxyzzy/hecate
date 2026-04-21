package catalog

import (
	"context"

	"github.com/hecate/agent-runtime/internal/providers"
)

type Entry struct {
	Provider        providers.Provider
	Name            string
	Kind            providers.Kind
	DefaultModel    string
	Models          []string
	DiscoverySource string
	RefreshedAt     string
	Healthy         bool
	Status          string
	Error           string
}

type Catalog interface {
	Snapshot(ctx context.Context) []Entry
	Get(ctx context.Context, name string) (Entry, bool)
}
