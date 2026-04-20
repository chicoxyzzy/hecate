package billing

import (
	"fmt"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

const microsPerDollar = 1_000_000

type Pricebook interface {
	Lookup(provider, model string) (Price, bool)
	Estimate(provider, model string, usage types.Usage) (types.CostBreakdown, error)
}

type Price struct {
	InputMicrosUSDPerMillionTokens       int64
	OutputMicrosUSDPerMillionTokens      int64
	CachedInputMicrosUSDPerMillionTokens int64
}

type StaticPricebook struct {
	prices        map[string]Price
	providerKinds map[string]providers.Kind
}

func NewStaticPricebook(cfg config.ProvidersConfig) *StaticPricebook {
	book := &StaticPricebook{
		prices: map[string]Price{
			"openai/gpt-4.1-mini": {
				InputMicrosUSDPerMillionTokens:       400_000,
				OutputMicrosUSDPerMillionTokens:      1_600_000,
				CachedInputMicrosUSDPerMillionTokens: 100_000,
			},
			"openai/gpt-4o-mini": {
				InputMicrosUSDPerMillionTokens:       150_000,
				OutputMicrosUSDPerMillionTokens:      600_000,
				CachedInputMicrosUSDPerMillionTokens: 75_000,
			},
		},
		providerKinds: make(map[string]providers.Kind),
	}
	for _, providerCfg := range cfg.OpenAICompatible {
		kind := providers.KindCloud
		if providerCfg.Kind == string(providers.KindLocal) {
			kind = providers.KindLocal
		}
		book.providerKinds[providerCfg.Name] = kind

		price := Price{
			InputMicrosUSDPerMillionTokens:       providerCfg.InputMicrosUSDPerMillionTokens,
			OutputMicrosUSDPerMillionTokens:      providerCfg.OutputMicrosUSDPerMillionTokens,
			CachedInputMicrosUSDPerMillionTokens: providerCfg.CachedInputMicrosUSDPerMillionTokens,
		}
		for _, model := range providerCfg.Models {
			book.prices[providerCfg.Name+"/"+model] = price
		}
		if providerCfg.DefaultModel != "" {
			book.prices[providerCfg.Name+"/"+providerCfg.DefaultModel] = price
		}
	}
	return book
}

func (p *StaticPricebook) Lookup(provider, model string) (Price, bool) {
	if price, ok := p.prices[provider+"/"+model]; ok {
		return price, true
	}

	if canonical := models.Canonicalize(model); canonical != model {
		price, ok := p.prices[provider+"/"+canonical]
		return price, ok
	}

	return Price{}, false
}

func (p *StaticPricebook) Estimate(provider, model string, usage types.Usage) (types.CostBreakdown, error) {
	price, ok := p.Lookup(provider, model)
	if !ok {
		if p.providerKinds[provider] == providers.KindLocal {
			return types.CostBreakdown{Currency: "USD"}, nil
		}
		return types.CostBreakdown{}, fmt.Errorf("price not found for provider=%s model=%s", provider, model)
	}

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
	}, nil
}

func scaleMicros(microsPerMillion int64, tokens int) int64 {
	return microsPerMillion * int64(tokens) / 1_000_000
}
