package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type ResponseFinalizer interface {
	FinalizeCache(ctx context.Context, trace *profiler.Trace, req types.ChatRequest, cached *CacheLookupResult) *ChatResult
	FinalizeExecution(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan, callResult *providerCallResult) (*ChatResult, error)
}

type DefaultResponseFinalizer struct {
	logger    *slog.Logger
	governor  governor.Governor
	pricebook billing.Pricebook
	metrics   *telemetry.Metrics
}

func NewDefaultResponseFinalizer(
	logger *slog.Logger,
	governor governor.Governor,
	pricebook billing.Pricebook,
	metrics *telemetry.Metrics,
) *DefaultResponseFinalizer {
	return &DefaultResponseFinalizer{
		logger:    logger,
		governor:  governor,
		pricebook: pricebook,
		metrics:   metrics,
	}
}

func (f *DefaultResponseFinalizer) FinalizeCache(ctx context.Context, trace *profiler.Trace, req types.ChatRequest, cached *CacheLookupResult) *ChatResult {
	metadata := f.buildCacheMetadata(req, cached.Response, cached.Route, cached.ProviderKind, cached.CacheType, cached.Semantic, trace)
	return f.completeResult(ctx, trace, cached.Response, metadata)
}

func (f *DefaultResponseFinalizer) FinalizeExecution(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan, callResult *providerCallResult) (*ChatResult, error) {
	resp := callResult.Response
	decision := callResult.Decision

	resp.Route = decision
	recordTrace(trace, "usage.normalized", "response", map[string]any{
		"gen_ai.usage.input_tokens":  resp.Usage.PromptTokens,
		"gen_ai.usage.output_tokens": resp.Usage.CompletionTokens,
		"gen_ai.usage.total_tokens":  resp.Usage.TotalTokens,
	})

	cost, err := f.pricebook.Estimate(decision.Provider, resp.Model, resp.Usage)
	if err != nil {
		return nil, fmt.Errorf("estimate cost: %w", err)
	}
	resp.Cost = cost
	recordTrace(trace, "cost.calculated", "response", map[string]any{
		"hecate.cost.total_micros_usd":  cost.TotalMicrosUSD,
		"hecate.cost.input_micros_usd":  cost.InputMicrosUSD,
		"hecate.cost.output_micros_usd": cost.OutputMicrosUSD,
		"hecate.cost.cached_micros_usd": cost.CachedInputMicrosUSD,
	})

	if err := f.governor.RecordUsage(ctx, plan.Request, decision, cost.TotalMicrosUSD); err != nil {
		telemetry.Warn(f.logger, ctx, "gateway.budget.usage_record.failed",
			slog.String("event.name", "gateway.budget.usage_record.failed"),
			slog.Any("error", err),
		)
		recordTraceError(trace, "governor.usage_record_failed", "governor", errorKindUsageRecordFailed, err, nil)
	}

	identity := models.BuildIdentity(plan.OriginalRequest.Model, resp.Model)
	recordTrace(trace, "response.returned", "response", map[string]any{
		"gen_ai.provider.name":             decision.Provider,
		"gen_ai.response.model":            resp.Model,
		"gen_ai.request.model":             identity.Requested,
		"hecate.model.requested_canonical": identity.CanonicalRequested,
		"hecate.model.resolved_canonical":  identity.CanonicalResolved,
	})

	metadata := ResponseMetadata{
		RequestID:               plan.OriginalRequest.RequestID,
		Provider:                decision.Provider,
		ProviderKind:            callResult.ProviderKind,
		RouteReason:             decision.Reason,
		RequestedModel:          identity.Requested,
		CanonicalRequestedModel: identity.CanonicalRequested,
		Model:                   identity.Resolved,
		CanonicalResolvedModel:  identity.CanonicalResolved,
		CacheHit:                false,
		CacheType:               "miss",
		PromptTokens:            resp.Usage.PromptTokens,
		CompletionTokens:        resp.Usage.CompletionTokens,
		TotalTokens:             resp.Usage.TotalTokens,
		CostMicrosUSD:           cost.TotalMicrosUSD,
		AttemptCount:            callResult.AttemptCount,
		RetryCount:              callResult.RetryCount,
		FallbackFromProvider:    callResult.FallbackFromProvider,
		TraceID:                 trace.TraceID,
		SpanID:                  trace.RootSpanID(),
	}

	return f.completeResult(ctx, trace, resp, metadata), nil
}

func (f *DefaultResponseFinalizer) buildCacheMetadata(req types.ChatRequest, resp *types.ChatResponse, route types.RouteDecision, providerKind, cacheType string, semantic *cache.SemanticMatch, trace *profiler.Trace) ResponseMetadata {
	identity := models.BuildIdentity(req.Model, resp.Model)
	metadata := ResponseMetadata{
		RequestID:               req.RequestID,
		Provider:                route.Provider,
		ProviderKind:            providerKind,
		RouteReason:             route.Reason,
		RequestedModel:          identity.Requested,
		CanonicalRequestedModel: identity.CanonicalRequested,
		Model:                   identity.Resolved,
		CanonicalResolvedModel:  identity.CanonicalResolved,
		CacheHit:                true,
		CacheType:               cacheType,
		PromptTokens:            resp.Usage.PromptTokens,
		CompletionTokens:        resp.Usage.CompletionTokens,
		TotalTokens:             resp.Usage.TotalTokens,
		CostMicrosUSD:           resp.Cost.TotalMicrosUSD,
		TraceID:                 trace.TraceID,
		SpanID:                  trace.RootSpanID(),
	}
	if semantic != nil {
		metadata.SemanticStrategy = semantic.Strategy
		metadata.SemanticIndexType = semantic.IndexType
		metadata.SemanticSimilarity = semantic.Similarity
	}
	return metadata
}

func (f *DefaultResponseFinalizer) completeResult(ctx context.Context, trace *profiler.Trace, resp *types.ChatResponse, metadata ResponseMetadata) *ChatResult {
	if f.metrics != nil {
		f.metrics.RecordChat(ctx, telemetry.ChatMetricsRecord{
			Provider:             metadata.Provider,
			ProviderKind:         metadata.ProviderKind,
			RequestedModel:       metadata.RequestedModel,
			ResponseModel:        metadata.Model,
			CacheHit:             metadata.CacheHit,
			CacheType:            metadata.CacheType,
			SemanticStrategy:     metadata.SemanticStrategy,
			SemanticIndexType:    metadata.SemanticIndexType,
			CostMicrosUSD:        metadata.CostMicrosUSD,
			PromptTokens:         int64(metadata.PromptTokens),
			CompletionTokens:     int64(metadata.CompletionTokens),
			TotalTokens:          int64(metadata.TotalTokens),
			RetryCount:           metadata.RetryCount,
			FallbackFromProvider: metadata.FallbackFromProvider,
		})
	}

	telemetry.Info(f.logger, ctx, "gen_ai.gateway.request",
		slog.String("event.name", "gen_ai.gateway.request"),
		slog.String(telemetry.AttrHecateResult, telemetry.ResultSuccess),
		slog.String(telemetry.AttrGenAIProviderName, metadata.Provider),
		slog.String(telemetry.AttrHecateProviderKind, metadata.ProviderKind),
		slog.String(telemetry.AttrHecateRouteReason, metadata.RouteReason),
		slog.String(telemetry.AttrGenAIRequestModel, metadata.RequestedModel),
		slog.String("hecate.model.requested_canonical", metadata.CanonicalRequestedModel),
		slog.String(telemetry.AttrGenAIResponseModel, metadata.Model),
		slog.String("hecate.model.resolved_canonical", metadata.CanonicalResolvedModel),
		slog.Bool(telemetry.AttrHecateCacheHit, metadata.CacheHit),
		slog.String(telemetry.AttrHecateCacheType, metadata.CacheType),
		slog.String(telemetry.AttrHecateSemanticStrategy, metadata.SemanticStrategy),
		slog.String(telemetry.AttrHecateSemanticIndexType, metadata.SemanticIndexType),
		slog.Float64(telemetry.AttrHecateSemanticSimilarity, metadata.SemanticSimilarity),
		slog.Int(telemetry.AttrGenAIUsageInputTokens, metadata.PromptTokens),
		slog.Int(telemetry.AttrGenAIUsageOutputTokens, metadata.CompletionTokens),
		slog.Int(telemetry.AttrGenAIUsageTotalTokens, metadata.TotalTokens),
		slog.Int64(telemetry.AttrHecateCostTotalMicrosUSD, metadata.CostMicrosUSD),
		slog.Int(telemetry.AttrHecateRetryAttemptCount, metadata.AttemptCount),
		slog.Int(telemetry.AttrHecateRetryCount, metadata.RetryCount),
		slog.String(telemetry.AttrHecateFailoverFromProvider, metadata.FallbackFromProvider),
	)

	return &ChatResult{
		Response: resp,
		Metadata: metadata,
		Trace:    trace,
	}
}
