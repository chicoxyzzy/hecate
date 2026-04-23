package billing

import (
	"context"

	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/pkg/types"
)

type ControlPlanePricebook struct {
	base  Pricebook
	store controlplane.Store
}

func NewControlPlanePricebook(base Pricebook, store controlplane.Store) *ControlPlanePricebook {
	return &ControlPlanePricebook{base: base, store: store}
}

func (p *ControlPlanePricebook) Lookup(provider, model string) (Price, bool) {
	if price, ok := p.lookupPersisted(provider, model); ok {
		return price, true
	}
	if p.base == nil {
		return Price{}, false
	}
	return p.base.Lookup(provider, model)
}

func (p *ControlPlanePricebook) Estimate(provider, model string, usage types.Usage) (types.CostBreakdown, error) {
	if price, ok := p.lookupPersisted(provider, model); ok {
		return estimateWithPrice(price, usage), nil
	}
	if p.base == nil {
		return types.CostBreakdown{}, errPriceNotFound
	}
	return p.base.Estimate(provider, model, usage)
}

func (p *ControlPlanePricebook) lookupPersisted(provider, model string) (Price, bool) {
	if p.store == nil {
		return Price{}, false
	}
	state, err := p.store.Snapshot(context.Background())
	if err != nil {
		return Price{}, false
	}
	for _, entry := range state.Pricebook {
		if entry.Provider == provider && (entry.Model == model || entry.Model == models.Canonicalize(model)) {
			return Price{
				InputMicrosUSDPerMillionTokens:       entry.InputMicrosUSDPerMillionTokens,
				OutputMicrosUSDPerMillionTokens:      entry.OutputMicrosUSDPerMillionTokens,
				CachedInputMicrosUSDPerMillionTokens: entry.CachedInputMicrosUSDPerMillionTokens,
			}, true
		}
	}
	return Price{}, false
}

func estimateWithPrice(price Price, usage types.Usage) types.CostBreakdown {
	inputMicros := scaleMicros(price.InputMicrosUSDPerMillionTokens, usage.PromptTokens)
	outputMicros := scaleMicros(price.OutputMicrosUSDPerMillionTokens, usage.CompletionTokens)
	cachedMicros := scaleMicros(price.CachedInputMicrosUSDPerMillionTokens, usage.CachedPromptTokens)
	return types.CostBreakdown{
		Currency:                  "USD",
		InputMicrosUSD:            inputMicros,
		OutputMicrosUSD:           outputMicros,
		CachedInputMicrosUSD:      cachedMicros,
		TotalMicrosUSD:            inputMicros + outputMicros + cachedMicros,
		InputMicrosUSDPerMillion:  price.InputMicrosUSDPerMillionTokens,
		OutputMicrosUSDPerMillion: price.OutputMicrosUSDPerMillionTokens,
	}
}
