package gateway

import (
	"context"
	"testing"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestDefaultRoutePreflightEvaluateReturnsResult(t *testing.T) {
	t.Parallel()

	store := governor.NewMemoryBudgetStore()
	preflight := NewDefaultRoutePreflight(
		governor.NewStaticGovernor(config.GovernorConfig{}, store, store),
		providers.NewRegistry(&sequenceProvider{name: "openai", kind: providers.KindCloud}),
		billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{
					Name:         "openai",
					Kind:         "cloud",
					DefaultModel: "model-a",
				},
			},
		}, config.PricebookConfig{
			Entries: []config.ModelPriceConfig{
				{Provider: "openai", Model: "model-a", InputMicrosUSDPerMillionTokens: 100_000, OutputMicrosUSDPerMillionTokens: 200_000},
			},
		}),
	)

	result, err := preflight.Evaluate(context.Background(), types.ChatRequest{
		Model:     "model-a",
		Messages:  []types.Message{{Role: "user", Content: "hello hello hello hello hello hello hello hello hello hello"}},
		MaxTokens: 4000,
	}, types.RouteDecision{Provider: "openai", Model: "model-a"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if result.ProviderKind != "cloud" {
		t.Fatalf("provider_kind = %q, want cloud", result.ProviderKind)
	}
	if result.EstimatedCost.TotalMicrosUSD == 0 {
		t.Fatal("estimated_cost = 0, want non-zero")
	}
}

func TestDefaultRoutePreflightEvaluateDenied(t *testing.T) {
	t.Parallel()

	store := governor.NewMemoryBudgetStore()
	preflight := NewDefaultRoutePreflight(
		governor.NewStaticGovernor(config.GovernorConfig{
			DeniedProviders: []string{"openai"},
		}, store, store),
		providers.NewRegistry(&sequenceProvider{name: "openai", kind: providers.KindCloud}),
		billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{
					Name:         "openai",
					Kind:         "cloud",
					DefaultModel: "model-a",
				},
			},
		}, config.PricebookConfig{
			Entries: []config.ModelPriceConfig{
				{Provider: "openai", Model: "model-a", InputMicrosUSDPerMillionTokens: 100_000, OutputMicrosUSDPerMillionTokens: 200_000},
			},
		}),
	)

	_, err := preflight.Evaluate(context.Background(), types.ChatRequest{
		Model:    "model-a",
		Messages: []types.Message{{Role: "user", Content: "hello"}},
	}, types.RouteDecision{Provider: "openai", Model: "model-a"})
	if err == nil {
		t.Fatal("Evaluate() error = nil, want denial")
	}

	preflightErr, ok := AsRoutePreflightError(err)
	if !ok {
		t.Fatalf("Evaluate() error = %v, want RoutePreflightError", err)
	}
	if preflightErr.Kind != RoutePreflightRouteDenied {
		t.Fatalf("kind = %q, want %q", preflightErr.Kind, RoutePreflightRouteDenied)
	}
}
