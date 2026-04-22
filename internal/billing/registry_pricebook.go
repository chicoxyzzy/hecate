package billing

import (
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

type RegistryAwarePricebook struct {
	base     Pricebook
	registry providers.Registry
}

func NewRegistryAwarePricebook(base Pricebook, registry providers.Registry) *RegistryAwarePricebook {
	return &RegistryAwarePricebook{
		base:     base,
		registry: registry,
	}
}

func (p *RegistryAwarePricebook) Lookup(provider, model string) (Price, bool) {
	return p.base.Lookup(provider, model)
}

func (p *RegistryAwarePricebook) Estimate(provider, model string, usage types.Usage) (types.CostBreakdown, error) {
	cost, err := p.base.Estimate(provider, model, usage)
	if err == nil {
		return cost, nil
	}
	if !IsPriceNotFound(err) {
		return types.CostBreakdown{}, err
	}
	item, ok := p.registry.Get(provider)
	if ok && item.Kind() == providers.KindLocal {
		return types.CostBreakdown{Currency: "USD"}, nil
	}
	return types.CostBreakdown{}, err
}
