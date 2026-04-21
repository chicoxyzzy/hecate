package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type CacheRuntime interface {
	Lookup(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan) (*CacheLookupResult, bool, error)
	Store(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan, decision types.RouteDecision, resp *types.ChatResponse)
}

type CacheLookupResult struct {
	Response     *types.ChatResponse
	Route        types.RouteDecision
	ProviderKind string
	CacheType    string
	Semantic     *cache.SemanticMatch
}

type GatewayCacheRuntime struct {
	logger          *slog.Logger
	exact           cache.Store
	semantic        cache.SemanticStore
	semanticOptions SemanticOptions
	governor        governor.Governor
	providers       providers.Registry
}

func NewGatewayCacheRuntime(
	logger *slog.Logger,
	exact cache.Store,
	semantic cache.SemanticStore,
	semanticOptions SemanticOptions,
	governor governor.Governor,
	providers providers.Registry,
) *GatewayCacheRuntime {
	return &GatewayCacheRuntime{
		logger:          logger,
		exact:           exact,
		semantic:        semantic,
		semanticOptions: semanticOptions,
		governor:        governor,
		providers:       providers,
	}
}

func (r *GatewayCacheRuntime) Lookup(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan) (*CacheLookupResult, bool, error) {
	if result, ok, err := r.lookupExact(ctx, trace, plan); err != nil {
		return nil, false, err
	} else if ok {
		return result, true, nil
	}

	return r.lookupSemantic(ctx, trace, plan)
}

func (r *GatewayCacheRuntime) Store(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan, decision types.RouteDecision, resp *types.ChatResponse) {
	if err := r.exact.Set(ctx, plan.CacheKey, resp); err != nil {
		telemetry.Warn(r.logger, ctx, "gateway.cache.store.failed",
			slog.String("event.name", "gateway.cache.store.failed"),
			slog.String("hecate.cache.type", "exact"),
			slog.Any("error", err),
		)
	}

	if !plan.SemanticEligible {
		return
	}

	namespace := cache.BuildSemanticNamespace(plan.Request, decision)
	if err := r.semantic.Set(ctx, cache.SemanticEntry{
		Namespace: namespace,
		Text:      cache.BuildSemanticText(plan.Request, r.semanticOptions.MaxTextChars),
		Response:  resp,
	}); err != nil {
		telemetry.Warn(r.logger, ctx, "gateway.cache.semantic.store.failed",
			slog.String("event.name", "gateway.cache.semantic.store.failed"),
			slog.String("hecate.cache.type", "semantic"),
			slog.String("hecate.semantic.scope", namespace),
			slog.Any("error", err),
		)
		recordTraceError(trace, "semantic_cache.store_failed", "cache", errorKindSemanticCacheStore, err, map[string]any{
			telemetry.AttrHecateCacheType:     "semantic",
			telemetry.AttrHecateSemanticScope: namespace,
		})
		return
	}

	recordTrace(trace, "semantic_cache.store_finished", "cache", map[string]any{
		telemetry.AttrHecateCacheType:     "semantic",
		telemetry.AttrHecateSemanticScope: namespace,
	})
}

func (r *GatewayCacheRuntime) lookupExact(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan) (*CacheLookupResult, bool, error) {
	cached, ok := r.exact.Get(ctx, plan.CacheKey)
	if !ok {
		recordTrace(trace, "cache.miss", "cache", map[string]any{
			telemetry.AttrHecateCacheHit:  false,
			telemetry.AttrHecateCacheType: "exact",
			telemetry.AttrHecateCacheKey:  plan.CacheKey,
		})
		return nil, false, nil
	}

	recordTrace(trace, "cache.hit", "cache", map[string]any{
		telemetry.AttrHecateCacheHit:  true,
		telemetry.AttrHecateCacheType: "exact",
		telemetry.AttrHecateCacheKey:  plan.CacheKey,
	})

	providerKind := ""
	if provider, ok := r.providers.Get(cached.Route.Provider); ok {
		providerKind = string(provider.Kind())
	}
	if err := r.governor.CheckRoute(ctx, plan.Request, cached.Route, providerKind, 0); err != nil {
		recordTraceError(trace, "governor.route_denied", "governor", errorKindRouteDenied, err, map[string]any{
			telemetry.AttrGenAIProviderName:            cached.Route.Provider,
			telemetry.AttrHecateProviderKind:           providerKind,
			telemetry.AttrHecateCostEstimatedMicrosUSD: 0,
			telemetry.AttrHecateGovernorRouteResult:    telemetry.ResultDenied,
			telemetry.AttrHecateCacheType:              "exact",
		})
		return nil, false, fmt.Errorf("%w: %v", errDenied, err)
	}
	recordTrace(trace, "governor.route_allowed", "governor", map[string]any{
		telemetry.AttrGenAIProviderName:            cached.Route.Provider,
		telemetry.AttrHecateProviderKind:           providerKind,
		telemetry.AttrHecateCostEstimatedMicrosUSD: 0,
		telemetry.AttrHecateGovernorRouteResult:    telemetry.ResultSuccess,
		telemetry.AttrHecateCacheType:              "exact",
	})

	return &CacheLookupResult{
		Response:     cached,
		Route:        cached.Route,
		ProviderKind: providerKind,
		CacheType:    "exact",
	}, true, nil
}

func (r *GatewayCacheRuntime) lookupSemantic(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan) (*CacheLookupResult, bool, error) {
	if !plan.SemanticEligible {
		return nil, false, nil
	}

	recordTrace(trace, "semantic_cache.lookup_started", "cache", map[string]any{
		telemetry.AttrHecateCacheType:      "semantic",
		telemetry.AttrHecateSemanticLookup: true,
		telemetry.AttrHecateSemanticScope:  plan.SemanticScope,
	})
	match, ok := r.semantic.Search(ctx, plan.SemanticQuery)
	if !ok {
		recordTrace(trace, "semantic_cache.miss", "cache", map[string]any{
			telemetry.AttrHecateCacheHit:      false,
			telemetry.AttrHecateCacheType:     "semantic",
			telemetry.AttrHecateSemanticScope: plan.SemanticScope,
		})
		return nil, false, nil
	}

	recordTrace(trace, "semantic_cache.hit", "cache", map[string]any{
		telemetry.AttrHecateCacheHit:           true,
		telemetry.AttrHecateCacheType:          "semantic",
		telemetry.AttrHecateSemanticScope:      plan.SemanticScope,
		telemetry.AttrHecateSemanticSimilarity: match.Similarity,
		telemetry.AttrHecateSemanticStrategy:   match.Strategy,
		telemetry.AttrHecateSemanticIndexType:  match.IndexType,
	})

	return &CacheLookupResult{
		Response:     match.Response,
		Route:        plan.Route,
		ProviderKind: plan.ProviderKind,
		CacheType:    "semantic",
		Semantic:     match,
	}, true, nil
}
