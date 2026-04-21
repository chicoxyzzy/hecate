package billing

import (
	"errors"
	"fmt"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

const microsPerDollar = 1_000_000

var errPriceNotFound = errors.New("price not found")

type Pricebook interface {
	Lookup(provider, model string) (Price, bool)
	Estimate(provider, model string, usage types.Usage) (types.CostBreakdown, error)
}

func IsPriceNotFound(err error) bool {
	return errors.Is(err, errPriceNotFound)
}

type Price struct {
	InputMicrosUSDPerMillionTokens       int64
	OutputMicrosUSDPerMillionTokens      int64
	CachedInputMicrosUSDPerMillionTokens int64
}

type StaticPricebook struct {
	prices        map[string]Price
	providerKinds map[string]providers.Kind
	unknownPolicy string
}

func NewStaticPricebook(providersCfg config.ProvidersConfig, priceCfg config.PricebookConfig) *StaticPricebook {
	book := &StaticPricebook{
		prices:        make(map[string]Price),
		providerKinds: make(map[string]providers.Kind),
		unknownPolicy: normalizeUnknownModelPolicy(priceCfg.UnknownModelPolicy),
	}
	for _, providerCfg := range providersCfg.OpenAICompatible {
		kind := providers.KindCloud
		if providerCfg.Kind == string(providers.KindLocal) {
			kind = providers.KindLocal
		}
		book.providerKinds[providerCfg.Name] = kind
	}

	for _, entry := range priceCfg.Entries {
		if entry.Provider == "" || entry.Model == "" {
			continue
		}
		book.prices[entry.Provider+"/"+entry.Model] = Price{
			InputMicrosUSDPerMillionTokens:       entry.InputMicrosUSDPerMillionTokens,
			OutputMicrosUSDPerMillionTokens:      entry.OutputMicrosUSDPerMillionTokens,
			CachedInputMicrosUSDPerMillionTokens: entry.CachedInputMicrosUSDPerMillionTokens,
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
		if p.unknownPolicy == "zero" {
			return types.CostBreakdown{Currency: "USD"}, nil
		}
		return types.CostBreakdown{}, fmt.Errorf("%w for provider=%s model=%s", errPriceNotFound, provider, model)
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

func normalizeUnknownModelPolicy(policy string) string {
	switch policy {
	case "zero":
		return "zero"
	default:
		return "error"
	}
}

func scaleMicros(microsPerMillion int64, tokens int) int64 {
	return microsPerMillion * int64(tokens) / 1_000_000
}
