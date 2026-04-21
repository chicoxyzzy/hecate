package gateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

type sequenceProvider struct {
	name      string
	kind      providers.Kind
	responses []providerResponse
	callCount int
}

type providerResponse struct {
	response *types.ChatResponse
	err      error
}

func (p *sequenceProvider) Name() string { return p.name }

func (p *sequenceProvider) Kind() providers.Kind { return p.kind }

func (p *sequenceProvider) DefaultModel() string { return "model-a" }

func (p *sequenceProvider) Capabilities(context.Context) (providers.Capabilities, error) {
	return providers.Capabilities{
		Name:         p.name,
		Kind:         p.kind,
		DefaultModel: p.DefaultModel(),
		Models:       []string{"model-a", "model-b"},
	}, nil
}

func (p *sequenceProvider) Chat(_ context.Context, _ types.ChatRequest) (*types.ChatResponse, error) {
	if p.callCount >= len(p.responses) {
		return nil, errors.New("unexpected call")
	}
	item := p.responses[p.callCount]
	p.callCount++
	return item.response, item.err
}

func (p *sequenceProvider) Supports(model string) bool {
	return model == "model-a" || model == "model-b"
}

type staticFallbackRouter struct {
	fallbacks []types.RouteDecision
}

func (r staticFallbackRouter) Route(context.Context, types.ChatRequest) (types.RouteDecision, error) {
	return types.RouteDecision{}, errors.New("not used")
}

func (r staticFallbackRouter) Fallbacks(context.Context, types.ChatRequest, types.RouteDecision) []types.RouteDecision {
	return append([]types.RouteDecision(nil), r.fallbacks...)
}

func TestResilientExecutorRetriesRetryableError(t *testing.T) {
	t.Parallel()

	provider := &sequenceProvider{
		name: "openai",
		kind: providers.KindCloud,
		responses: []providerResponse{
			{err: &providers.UpstreamError{StatusCode: http.StatusTooManyRequests}},
			{response: &types.ChatResponse{Model: "model-a"}},
		},
	}
	registry := providers.NewRegistry(provider)
	store := governor.NewMemoryBudgetStore()
	executor := NewResilientExecutor(
		staticFallbackRouter{},
		governor.NewStaticGovernor(config.GovernorConfig{}, store, store),
		registry,
		nil,
		billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{
					Name:                            "openai",
					Kind:                            "cloud",
					DefaultModel:                    "model-a",
					Models:                          []string{"model-a"},
					InputMicrosUSDPerMillionTokens:  100_000,
					OutputMicrosUSDPerMillionTokens: 200_000,
				},
			},
		}),
		ResilienceOptions{MaxAttempts: 2, RetryBackoff: time.Millisecond},
	)
	executor.sleep = func(context.Context, time.Duration) error { return nil }

	trace := profiler.NewTrace("req-retry", nil)
	defer trace.Finalize()

	result, err := executor.Execute(context.Background(), trace, types.ChatRequest{
		Model: "model-a",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}, types.RouteDecision{Provider: "openai", Model: "model-a", Reason: "test"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.AttemptCount != 2 {
		t.Fatalf("attempt_count = %d, want 2", result.AttemptCount)
	}
	if result.RetryCount != 1 {
		t.Fatalf("retry_count = %d, want 1", result.RetryCount)
	}
	if provider.callCount != 2 {
		t.Fatalf("provider call_count = %d, want 2", provider.callCount)
	}
}

func TestResilientExecutorFailsOverAfterRetryableFailure(t *testing.T) {
	t.Parallel()

	primary := &sequenceProvider{
		name: "openai",
		kind: providers.KindCloud,
		responses: []providerResponse{
			{err: &providers.UpstreamError{StatusCode: http.StatusServiceUnavailable}},
		},
	}
	fallback := &sequenceProvider{
		name: "ollama",
		kind: providers.KindLocal,
		responses: []providerResponse{
			{response: &types.ChatResponse{Model: "model-b"}},
		},
	}
	registry := providers.NewRegistry(primary, fallback)
	store := governor.NewMemoryBudgetStore()
	executor := NewResilientExecutor(
		staticFallbackRouter{
			fallbacks: []types.RouteDecision{
				{Provider: "ollama", Model: "model-b", Reason: "test_failover"},
			},
		},
		governor.NewStaticGovernor(config.GovernorConfig{}, store, store),
		registry,
		nil,
		billing.NewStaticPricebook(config.ProvidersConfig{
			OpenAICompatible: []config.OpenAICompatibleProviderConfig{
				{
					Name:                            "openai",
					Kind:                            "cloud",
					DefaultModel:                    "model-a",
					Models:                          []string{"model-a"},
					InputMicrosUSDPerMillionTokens:  100_000,
					OutputMicrosUSDPerMillionTokens: 200_000,
				},
				{
					Name:         "ollama",
					Kind:         "local",
					DefaultModel: "model-b",
					Models:       []string{"model-b"},
				},
			},
		}),
		ResilienceOptions{MaxAttempts: 1, RetryBackoff: time.Millisecond, FailoverEnabled: true},
	)
	executor.sleep = func(context.Context, time.Duration) error { return nil }

	trace := profiler.NewTrace("req-failover", nil)
	defer trace.Finalize()

	result, err := executor.Execute(context.Background(), trace, types.ChatRequest{
		Model: "model-a",
		Messages: []types.Message{
			{Role: "user", Content: "hello"},
		},
	}, types.RouteDecision{Provider: "openai", Model: "model-a", Reason: "test"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Decision.Provider != "ollama" {
		t.Fatalf("provider = %q, want ollama", result.Decision.Provider)
	}
	if result.FallbackFromProvider != "openai" {
		t.Fatalf("fallback_from_provider = %q, want openai", result.FallbackFromProvider)
	}
	if primary.callCount != 1 {
		t.Fatalf("primary call_count = %d, want 1", primary.callCount)
	}
	if fallback.callCount != 1 {
		t.Fatalf("fallback call_count = %d, want 1", fallback.callCount)
	}
}
