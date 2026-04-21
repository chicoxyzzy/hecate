package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestDefaultResponseFinalizerFinalizeExecution(t *testing.T) {
	t.Parallel()

	store := governor.NewMemoryBudgetStore()
	finalizer := NewDefaultResponseFinalizer(
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		governor.NewStaticGovernor(config.GovernorConfig{BudgetKey: "global"}, store, store),
		billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{
					Name:                            "openai",
					Kind:                            "cloud",
					DefaultModel:                    "gpt-4o-mini",
					Models:                          []string{"gpt-4o-mini"},
					InputMicrosUSDPerMillionTokens:  150_000,
					OutputMicrosUSDPerMillionTokens: 600_000,
				},
			},
		}),
		telemetry.NewMetrics(),
	)

	trace := profiler.NewTrace("finalize-exec", nil)
	defer trace.Finalize()

	result, err := finalizer.FinalizeExecution(context.Background(), trace, &ExecutionPlan{
		OriginalRequest: types.ChatRequest{
			RequestID: "req-1",
			Model:     "gpt-4o-mini",
			Messages:  []types.Message{{Role: "user", Content: "hello"}},
		},
		Request: types.ChatRequest{
			RequestID: "req-1",
			Model:     "gpt-4o-mini",
			Messages:  []types.Message{{Role: "user", Content: "hello"}},
		},
	}, &providerCallResult{
		Response: &types.ChatResponse{
			Model: "gpt-4o-mini",
			Usage: types.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		},
		Decision:     types.RouteDecision{Provider: "openai", Model: "gpt-4o-mini", Reason: "test"},
		ProviderKind: string(providers.KindCloud),
	})
	if err != nil {
		t.Fatalf("FinalizeExecution() error = %v", err)
	}
	if result.Metadata.CacheType != "miss" {
		t.Fatalf("cache_type = %q, want miss", result.Metadata.CacheType)
	}
	if result.Metadata.CostMicrosUSD == 0 {
		t.Fatal("cost_micros_usd = 0, want non-zero")
	}

	status, err := store.Current(context.Background(), "global")
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if status == 0 {
		t.Fatal("budget usage not recorded")
	}
}

func TestDefaultResponseFinalizerFinalizeCache(t *testing.T) {
	t.Parallel()

	finalizer := NewDefaultResponseFinalizer(
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		governor.NewStaticGovernor(config.GovernorConfig{}, governor.NewMemoryBudgetStore(), governor.NewMemoryBudgetStore()),
		billing.NewStaticPricebook(config.ProvidersConfig{}),
		telemetry.NewMetrics(),
	)

	trace := profiler.NewTrace("finalize-cache", nil)
	defer trace.Finalize()

	result := finalizer.FinalizeCache(context.Background(), trace, types.ChatRequest{
		RequestID: "req-2",
		Model:     "llama3.1:8b",
	}, &CacheLookupResult{
		Response: &types.ChatResponse{
			Model: "llama3.1:8b",
			Usage: types.Usage{
				PromptTokens:     12,
				CompletionTokens: 6,
				TotalTokens:      18,
			},
			Cost: types.CostBreakdown{TotalMicrosUSD: 0},
		},
		Route:        types.RouteDecision{Provider: "ollama", Model: "llama3.1:8b", Reason: "semantic"},
		ProviderKind: "local",
		CacheType:    "semantic",
	})

	if !result.Metadata.CacheHit {
		t.Fatal("cache_hit = false, want true")
	}
	if result.Metadata.Provider != "ollama" {
		t.Fatalf("provider = %q, want ollama", result.Metadata.Provider)
	}
}
