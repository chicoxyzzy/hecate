package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/models"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Dependencies struct {
	Logger          *slog.Logger
	Cache           cache.Store
	Semantic        cache.SemanticStore
	SemanticOptions SemanticOptions
	Resilience      ResilienceOptions
	Router          router.Router
	Governor        governor.Governor
	Providers       providers.Registry
	HealthTracker   providers.HealthTracker
	Pricebook       billing.Pricebook
	Tracer          profiler.Tracer
	Metrics         *telemetry.Metrics
}

type SemanticOptions struct {
	Enabled       bool
	MinSimilarity float64
	MaxTextChars  int
}

type ResilienceOptions struct {
	MaxAttempts     int
	RetryBackoff    time.Duration
	FailoverEnabled bool
}

type Service struct {
	logger          *slog.Logger
	cache           cache.Store
	semantic        cache.SemanticStore
	semanticOptions SemanticOptions
	resilience      ResilienceOptions
	keyBuilder      cache.KeyBuilder
	router          router.Router
	governor        governor.Governor
	providers       providers.Registry
	healthTracker   providers.HealthTracker
	pricebook       billing.Pricebook
	tracer          profiler.Tracer
	metrics         *telemetry.Metrics
}

type ChatResult struct {
	Response *types.ChatResponse
	Metadata ResponseMetadata
	Trace    *profiler.Trace
}

type ModelsResult struct {
	Models []types.ModelInfo
}

type ProviderStatusResult struct {
	Providers []types.ProviderStatus
}

type BudgetStatusResult struct {
	Status types.BudgetStatus
}

type TraceResult struct {
	RequestID string
	TraceID   string
	StartedAt time.Time
	Spans     []types.TraceSpan
}

type ResponseMetadata struct {
	RequestID               string
	Provider                string
	ProviderKind            string
	RouteReason             string
	RequestedModel          string
	CanonicalRequestedModel string
	Model                   string
	CanonicalResolvedModel  string
	CacheHit                bool
	CacheType               string
	SemanticStrategy        string
	SemanticIndexType       string
	SemanticSimilarity      float64
	PromptTokens            int
	CompletionTokens        int
	TotalTokens             int
	CostMicrosUSD           int64
	AttemptCount            int
	RetryCount              int
	FallbackFromProvider    string
	TraceID                 string
	SpanID                  string
}

func NewService(deps Dependencies) *Service {
	return &Service{
		logger:          deps.Logger,
		cache:           deps.Cache,
		semantic:        deps.Semantic,
		semanticOptions: deps.SemanticOptions,
		resilience:      normalizeResilienceOptions(deps.Resilience),
		keyBuilder:      cache.StableKeyBuilder{},
		router:          deps.Router,
		governor:        deps.Governor,
		providers:       deps.Providers,
		healthTracker:   deps.HealthTracker,
		pricebook:       deps.Pricebook,
		tracer:          deps.Tracer,
		metrics:         deps.Metrics,
	}
}

func (s *Service) HandleChat(ctx context.Context, req types.ChatRequest) (*ChatResult, error) {
	trace := s.tracer.Start(req.RequestID)
	defer trace.Finalize()
	ctx = telemetry.WithTraceIDs(ctx, trace.TraceID, trace.RootSpanID())
	requestedIdentity := models.BuildIdentity(req.Model, "")
	trace.Record("request.received", map[string]any{
		"gen_ai.request.message_count": len(req.Messages),
		"gen_ai.request.model":         req.Model,
		"hecate.model.canonical":       requestedIdentity.CanonicalRequested,
	})

	if err := validate(req); err != nil {
		trace.Record("request.invalid", map[string]any{
			"error.message": err.Error(),
			"hecate.phase":  "request",
		})
		return nil, fmt.Errorf("%w: %v", errClient, err)
	}

	if err := s.governor.Check(ctx, req); err != nil {
		trace.Record("governor.denied", map[string]any{
			"error.message":          err.Error(),
			"hecate.governor.result": "denied",
		})
		return nil, fmt.Errorf("%w: %v", errDenied, err)
	}
	trace.Record("governor.allowed", map[string]any{
		"hecate.governor.result": "allowed",
	})

	rewrittenReq := s.governor.Rewrite(req)
	if rewrittenReq.Model != req.Model {
		trace.Record("governor.model_rewrite", map[string]any{
			"gen_ai.request.model.original":  req.Model,
			"gen_ai.request.model.rewritten": rewrittenReq.Model,
		})
	}

	cacheKey, err := s.keyBuilder.Key(rewrittenReq)
	if err != nil {
		return nil, fmt.Errorf("build cache key: %w", err)
	}

	if cached, ok := s.cache.Get(ctx, cacheKey); ok {
		trace.Record("cache.hit", map[string]any{
			"hecate.cache.hit":  true,
			"hecate.cache.type": "exact",
			"hecate.cache.key":  cacheKey,
		})
		identity := models.BuildIdentity(req.Model, cached.Model)
		providerKind := ""
		if provider, ok := s.providers.Get(cached.Route.Provider); ok {
			providerKind = string(provider.Kind())
		}
		if err := s.governor.CheckRoute(ctx, rewrittenReq, cached.Route, providerKind, 0); err != nil {
			trace.Record("governor.route_denied", map[string]any{
				"error.message":                err.Error(),
				"gen_ai.provider.name":         cached.Route.Provider,
				"hecate.provider.kind":         providerKind,
				"hecate.cost.estimated_micros": 0,
				"hecate.governor.route_result": "denied",
				"hecate.cache.type":            "exact",
			})
			return nil, fmt.Errorf("%w: %v", errDenied, err)
		}
		trace.Record("governor.route_allowed", map[string]any{
			"gen_ai.provider.name":         cached.Route.Provider,
			"hecate.provider.kind":         providerKind,
			"hecate.cost.estimated_micros": 0,
			"hecate.governor.route_result": "allowed",
			"hecate.cache.type":            "exact",
		})
		metadata := ResponseMetadata{
			RequestID:               req.RequestID,
			Provider:                cached.Route.Provider,
			ProviderKind:            providerKind,
			RouteReason:             cached.Route.Reason,
			RequestedModel:          identity.Requested,
			CanonicalRequestedModel: identity.CanonicalRequested,
			Model:                   identity.Resolved,
			CanonicalResolvedModel:  identity.CanonicalResolved,
			CacheHit:                true,
			CacheType:               "exact",
			PromptTokens:            cached.Usage.PromptTokens,
			CompletionTokens:        cached.Usage.CompletionTokens,
			TotalTokens:             cached.Usage.TotalTokens,
			CostMicrosUSD:           cached.Cost.TotalMicrosUSD,
			TraceID:                 trace.TraceID,
			SpanID:                  trace.RootSpanID(),
		}
		s.recordMetrics(metadata)
		s.logRequestSummary(ctx, metadata)
		return &ChatResult{
			Response: cached,
			Metadata: metadata,
			Trace:    trace,
		}, nil
	}
	trace.Record("cache.miss", map[string]any{
		"hecate.cache.hit":  false,
		"hecate.cache.type": "exact",
		"hecate.cache.key":  cacheKey,
	})

	decision, err := s.router.Route(ctx, rewrittenReq)
	if err != nil {
		trace.Record("router.failed", map[string]any{
			"error.message": err.Error(),
			"hecate.phase":  "routing",
		})
		return nil, fmt.Errorf("route request: %w", err)
	}
	trace.Record("router.selected", map[string]any{
		"gen_ai.provider.name": decision.Provider,
		"gen_ai.request.model": decision.Model,
		"hecate.route.reason":  decision.Reason,
	})

	provider, ok := s.providers.Get(decision.Provider)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", decision.Provider)
	}

	estimatedUsage := estimateUsage(rewrittenReq)
	estimatedCost, err := s.pricebook.Estimate(decision.Provider, decision.Model, estimatedUsage)
	if err != nil {
		trace.Record("governor.budget_estimate_failed", map[string]any{
			"error.message": err.Error(),
			"hecate.phase":  "governor",
		})
		return nil, fmt.Errorf("estimate preflight cost: %w", err)
	}
	if err := s.governor.CheckRoute(ctx, rewrittenReq, decision, string(provider.Kind()), estimatedCost.TotalMicrosUSD); err != nil {
		trace.Record("governor.route_denied", map[string]any{
			"error.message":                err.Error(),
			"gen_ai.provider.name":         decision.Provider,
			"hecate.provider.kind":         string(provider.Kind()),
			"hecate.cost.estimated_micros": estimatedCost.TotalMicrosUSD,
			"hecate.governor.route_result": "denied",
		})
		return nil, fmt.Errorf("%w: %v", errDenied, err)
	}
	trace.Record("governor.route_allowed", map[string]any{
		"gen_ai.provider.name":         decision.Provider,
		"hecate.provider.kind":         string(provider.Kind()),
		"hecate.cost.estimated_micros": estimatedCost.TotalMicrosUSD,
		"hecate.governor.route_result": "allowed",
	})

	if s.semantic != nil && s.semanticOptions.Enabled && cache.EligibleForSemanticCache(rewrittenReq, s.semanticOptions.MaxTextChars) {
		namespace := cache.BuildSemanticNamespace(rewrittenReq, decision)
		query := cache.SemanticQuery{
			Namespace:     namespace,
			Text:          cache.BuildSemanticText(rewrittenReq, s.semanticOptions.MaxTextChars),
			MinSimilarity: s.semanticOptions.MinSimilarity,
			MaxTextChars:  s.semanticOptions.MaxTextChars,
		}
		trace.Record("semantic_cache.lookup_started", map[string]any{
			"hecate.cache.type":      "semantic",
			"hecate.semantic.lookup": true,
			"hecate.semantic.scope":  namespace,
		})
		if match, ok := s.semantic.Search(ctx, query); ok {
			trace.Record("semantic_cache.hit", map[string]any{
				"hecate.cache.hit":           true,
				"hecate.cache.type":          "semantic",
				"hecate.semantic.scope":      namespace,
				"hecate.semantic.similarity": match.Similarity,
				"hecate.semantic.strategy":   match.Strategy,
				"hecate.semantic.index_type": match.IndexType,
			})
			identity := models.BuildIdentity(req.Model, match.Response.Model)
			metadata := ResponseMetadata{
				RequestID:               req.RequestID,
				Provider:                decision.Provider,
				ProviderKind:            string(provider.Kind()),
				RouteReason:             decision.Reason,
				RequestedModel:          identity.Requested,
				CanonicalRequestedModel: identity.CanonicalRequested,
				Model:                   identity.Resolved,
				CanonicalResolvedModel:  identity.CanonicalResolved,
				CacheHit:                true,
				CacheType:               "semantic",
				SemanticStrategy:        match.Strategy,
				SemanticIndexType:       match.IndexType,
				SemanticSimilarity:      match.Similarity,
				PromptTokens:            match.Response.Usage.PromptTokens,
				CompletionTokens:        match.Response.Usage.CompletionTokens,
				TotalTokens:             match.Response.Usage.TotalTokens,
				CostMicrosUSD:           match.Response.Cost.TotalMicrosUSD,
				TraceID:                 trace.TraceID,
				SpanID:                  trace.RootSpanID(),
			}
			s.recordMetrics(metadata)
			s.logRequestSummary(ctx, metadata)
			return &ChatResult{
				Response: match.Response,
				Metadata: metadata,
				Trace:    trace,
			}, nil
		}
		trace.Record("semantic_cache.miss", map[string]any{
			"hecate.cache.hit":      false,
			"hecate.cache.type":     "semantic",
			"hecate.semantic.scope": namespace,
		})
	}

	callResult, err := s.executeWithResilience(ctx, trace, rewrittenReq, decision)
	if err != nil {
		return nil, err
	}
	resp := callResult.Response
	decision = callResult.Decision
	provider = callResult.Provider

	resp.Route = decision
	trace.Record("usage.normalized", map[string]any{
		"gen_ai.usage.input_tokens":  resp.Usage.PromptTokens,
		"gen_ai.usage.output_tokens": resp.Usage.CompletionTokens,
		"gen_ai.usage.total_tokens":  resp.Usage.TotalTokens,
	})

	cost, err := s.pricebook.Estimate(decision.Provider, resp.Model, resp.Usage)
	if err != nil {
		return nil, fmt.Errorf("estimate cost: %w", err)
	}
	resp.Cost = cost
	trace.Record("cost.calculated", map[string]any{
		"hecate.cost.total_micros_usd":  cost.TotalMicrosUSD,
		"hecate.cost.input_micros_usd":  cost.InputMicrosUSD,
		"hecate.cost.output_micros_usd": cost.OutputMicrosUSD,
		"hecate.cost.cached_micros_usd": cost.CachedInputMicrosUSD,
	})
	if err := s.governor.RecordUsage(ctx, rewrittenReq, decision, cost.TotalMicrosUSD); err != nil {
		telemetry.Warn(s.logger, ctx, "gateway.budget.usage_record.failed",
			slog.String("event.name", "gateway.budget.usage_record.failed"),
			slog.Any("error", err),
		)
		trace.Record("governor.usage_record_failed", map[string]any{
			"error.message": err.Error(),
			"hecate.phase":  "governor",
		})
	}

	if err := s.cache.Set(ctx, cacheKey, resp); err != nil {
		telemetry.Warn(s.logger, ctx, "gateway.cache.store.failed",
			slog.String("event.name", "gateway.cache.store.failed"),
			slog.String("hecate.cache.type", "exact"),
			slog.Any("error", err),
		)
	}

	identity := models.BuildIdentity(req.Model, resp.Model)
	trace.Record("response.returned", map[string]any{
		"gen_ai.provider.name":             decision.Provider,
		"gen_ai.response.model":            resp.Model,
		"gen_ai.request.model":             identity.Requested,
		"hecate.model.requested_canonical": identity.CanonicalRequested,
		"hecate.model.resolved_canonical":  identity.CanonicalResolved,
	})

	metadata := ResponseMetadata{
		RequestID:               req.RequestID,
		Provider:                decision.Provider,
		ProviderKind:            string(provider.Kind()),
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
	s.recordMetrics(metadata)
	s.logRequestSummary(ctx, metadata)

	if s.semantic != nil && s.semanticOptions.Enabled && cache.EligibleForSemanticCache(rewrittenReq, s.semanticOptions.MaxTextChars) {
		namespace := cache.BuildSemanticNamespace(rewrittenReq, decision)
		if err := s.semantic.Set(ctx, cache.SemanticEntry{
			Namespace: namespace,
			Text:      cache.BuildSemanticText(rewrittenReq, s.semanticOptions.MaxTextChars),
			Response:  resp,
		}); err != nil {
			telemetry.Warn(s.logger, ctx, "gateway.cache.semantic.store.failed",
				slog.String("event.name", "gateway.cache.semantic.store.failed"),
				slog.String("hecate.cache.type", "semantic"),
				slog.String("hecate.semantic.scope", namespace),
				slog.Any("error", err),
			)
			trace.Record("semantic_cache.store_failed", map[string]any{
				"error.message":         err.Error(),
				"hecate.cache.type":     "semantic",
				"hecate.semantic.scope": namespace,
			})
		} else {
			trace.Record("semantic_cache.store_finished", map[string]any{
				"hecate.cache.type":     "semantic",
				"hecate.semantic.scope": namespace,
			})
		}
	}

	return &ChatResult{
		Response: resp,
		Metadata: metadata,
		Trace:    trace,
	}, nil
}

type providerCallResult struct {
	Response             *types.ChatResponse
	Decision             types.RouteDecision
	Provider             providers.Provider
	AttemptCount         int
	RetryCount           int
	FallbackFromProvider string
}

func (s *Service) executeWithResilience(ctx context.Context, trace *profiler.Trace, req types.ChatRequest, initial types.RouteDecision) (*providerCallResult, error) {
	candidates := []types.RouteDecision{initial}
	if s.resilience.FailoverEnabled {
		candidates = append(candidates, s.router.Fallbacks(ctx, req, initial)...)
	}

	totalAttempts := 0
	totalRetries := 0
	lastErr := error(nil)

	for index, candidate := range candidates {
		provider, ok := s.providers.Get(candidate.Provider)
		if !ok {
			lastErr = fmt.Errorf("provider %q not found", candidate.Provider)
			continue
		}

		estimatedUsage := estimateUsage(withResolvedModel(req, candidate.Model))
		estimatedCost, err := s.pricebook.Estimate(candidate.Provider, candidate.Model, estimatedUsage)
		if err != nil {
			lastErr = fmt.Errorf("estimate preflight cost: %w", err)
			if index == 0 {
				trace.Record("governor.budget_estimate_failed", map[string]any{
					"error.message": err.Error(),
					"hecate.phase":  "governor",
				})
				return nil, lastErr
			}
			trace.Record("provider.failover.skipped", map[string]any{
				"gen_ai.provider.name":   candidate.Provider,
				"gen_ai.request.model":   candidate.Model,
				"hecate.failover.reason": "cost_estimate_failed",
				"error.message":          err.Error(),
			})
			continue
		}

		if index > 0 {
			if err := s.governor.CheckRoute(ctx, req, candidate, string(provider.Kind()), estimatedCost.TotalMicrosUSD); err != nil {
				lastErr = fmt.Errorf("%w: %v", errDenied, err)
				trace.Record("provider.failover.skipped", map[string]any{
					"gen_ai.provider.name":         candidate.Provider,
					"gen_ai.request.model":         candidate.Model,
					"hecate.failover.reason":       "route_denied",
					"error.message":                err.Error(),
					"hecate.cost.estimated_micros": estimatedCost.TotalMicrosUSD,
				})
				continue
			}
			trace.Record("provider.failover.selected", map[string]any{
				"gen_ai.provider.name":          candidate.Provider,
				"gen_ai.request.model":          candidate.Model,
				"hecate.provider.kind":          string(provider.Kind()),
				"hecate.failover.from_provider": initial.Provider,
				"hecate.failover.to_provider":   candidate.Provider,
				"hecate.cost.estimated_micros":  estimatedCost.TotalMicrosUSD,
			})
		}

		attemptReq := withResolvedModel(req, candidate.Model)
		for attempt := 1; attempt <= s.resilience.MaxAttempts; attempt++ {
			totalAttempts++
			trace.Record("provider.call.started", map[string]any{
				"gen_ai.provider.name":      candidate.Provider,
				"gen_ai.request.model":      candidate.Model,
				"hecate.retry.attempt":      attempt,
				"hecate.provider.index":     index,
				"hecate.retry.max_attempts": s.resilience.MaxAttempts,
				"hecate.failover.active":    index > 0,
			})

			start := time.Now()
			resp, err := provider.Chat(ctx, attemptReq)
			if err == nil {
				trace.Record("provider.call.finished", map[string]any{
					"gen_ai.provider.name":       candidate.Provider,
					"gen_ai.request.model":       candidate.Model,
					"hecate.retry.attempt":       attempt,
					"hecate.provider.index":      index,
					"hecate.provider.latency_ms": time.Since(start).Milliseconds(),
				})
				if s.healthTracker != nil {
					s.healthTracker.RecordSuccess(candidate.Provider)
				}
				return &providerCallResult{
					Response:             resp,
					Decision:             candidate,
					Provider:             provider,
					AttemptCount:         totalAttempts,
					RetryCount:           totalRetries,
					FallbackFromProvider: fallbackFrom(initial.Provider, candidate.Provider),
				}, nil
			}

			lastErr = fmt.Errorf("provider %s call failed: %w", candidate.Provider, err)
			trace.Record("provider.call.failed", map[string]any{
				"gen_ai.provider.name":   candidate.Provider,
				"gen_ai.request.model":   candidate.Model,
				"hecate.retry.attempt":   attempt,
				"hecate.provider.index":  index,
				"error.message":          err.Error(),
				"hecate.retry.retryable": providers.IsRetryableError(err),
			})

			if !providers.IsRetryableError(err) {
				break
			}
			if attempt >= s.resilience.MaxAttempts {
				break
			}

			totalRetries++
			backoff := s.retryDelay(attempt)
			trace.Record("provider.retry.scheduled", map[string]any{
				"gen_ai.provider.name":      candidate.Provider,
				"gen_ai.request.model":      candidate.Model,
				"hecate.retry.attempt":      attempt,
				"hecate.retry.next_attempt": attempt + 1,
				"hecate.retry.backoff_ms":   backoff.Milliseconds(),
			})
			if err := sleepContext(ctx, backoff); err != nil {
				return nil, fmt.Errorf("wait for retry backoff: %w", err)
			}
		}

		if s.healthTracker != nil && providers.IsRetryableError(lastErr) {
			s.healthTracker.RecordFailure(candidate.Provider, lastErr)
			trace.Record("provider.health.degraded", map[string]any{
				"gen_ai.provider.name":         candidate.Provider,
				"error.message":                lastErr.Error(),
				"hecate.provider.health_state": "degraded",
			})
		}

		if index < len(candidates)-1 && providers.IsRetryableError(lastErr) {
			trace.Record("provider.failover.triggered", map[string]any{
				"gen_ai.provider.name":          candidate.Provider,
				"gen_ai.request.model":          candidate.Model,
				"hecate.failover.from_provider": candidate.Provider,
				"error.message":                 lastErr.Error(),
			})
			continue
		}
		break
	}

	if lastErr == nil {
		lastErr = errors.New("provider call failed")
	}
	return nil, lastErr
}

func estimateUsage(req types.ChatRequest) types.Usage {
	promptTokens := 0
	for _, msg := range req.Messages {
		promptTokens += len(msg.Content) / 4
	}
	completionTokens := req.MaxTokens
	if completionTokens < 0 {
		completionTokens = 0
	}
	return types.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
}

func withResolvedModel(req types.ChatRequest, model string) types.ChatRequest {
	req.Model = model
	return req
}

func fallbackFrom(initialProvider, finalProvider string) string {
	if initialProvider == "" || initialProvider == finalProvider {
		return ""
	}
	return initialProvider
}

func normalizeResilienceOptions(options ResilienceOptions) ResilienceOptions {
	if options.MaxAttempts <= 0 {
		options.MaxAttempts = 1
	}
	if options.RetryBackoff <= 0 {
		options.RetryBackoff = 200 * time.Millisecond
	}
	return options
}

func (s *Service) retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return s.resilience.RetryBackoff
	}
	return time.Duration(attempt) * s.resilience.RetryBackoff
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) MetricsSnapshot(ctx context.Context) (telemetry.Snapshot, telemetry.ProviderHealthSnapshot, error) {
	snapshot := telemetry.Snapshot{}
	if s.metrics != nil {
		snapshot = s.metrics.Snapshot()
	}

	status, err := s.ProviderStatus(ctx)
	if err != nil {
		return snapshot, telemetry.ProviderHealthSnapshot{}, err
	}

	health := telemetry.ProviderHealthSnapshot{}
	for _, provider := range status.Providers {
		if provider.Healthy {
			health.HealthyCount++
		} else {
			health.DegradedCount++
		}
	}

	return snapshot, health, nil
}

func (s *Service) ListModels(ctx context.Context) (*ModelsResult, error) {
	seen := make(map[string]struct{})
	modelsOut := make([]types.ModelInfo, 0, 16)

	for _, provider := range s.providers.All() {
		caps, err := provider.Capabilities(ctx)
		if err != nil {
			telemetry.Warn(s.logger, ctx, "gateway.providers.capabilities.unavailable",
				slog.String("event.name", "gateway.providers.capabilities.unavailable"),
				slog.String("gen_ai.provider.name", provider.Name()),
				slog.Any("error", err),
			)
		}

		modelIDs := caps.Models
		if len(modelIDs) == 0 && provider.DefaultModel() != "" {
			modelIDs = []string{provider.DefaultModel()}
		}

		defaultModel := caps.DefaultModel
		if defaultModel == "" {
			defaultModel = provider.DefaultModel()
		}

		discoverySource := caps.DiscoverySource
		if discoverySource == "" {
			discoverySource = "provider_default"
		}

		for _, modelID := range modelIDs {
			key := provider.Name() + "/" + modelID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			modelsOut = append(modelsOut, types.ModelInfo{
				ID:              modelID,
				Provider:        provider.Name(),
				Kind:            string(provider.Kind()),
				OwnedBy:         provider.Name(),
				Default:         modelID == defaultModel,
				DiscoverySource: discoverySource,
			})
		}
	}

	return &ModelsResult{Models: modelsOut}, nil
}

func (s *Service) ProviderStatus(ctx context.Context) (*ProviderStatusResult, error) {
	statuses := make([]types.ProviderStatus, 0, len(s.providers.All()))

	for _, provider := range s.providers.All() {
		caps, err := provider.Capabilities(ctx)

		defaultModel := caps.DefaultModel
		if defaultModel == "" {
			defaultModel = provider.DefaultModel()
		}

		models := append([]string(nil), caps.Models...)
		if len(models) == 0 && defaultModel != "" {
			models = []string{defaultModel}
		}

		discoverySource := caps.DiscoverySource
		if discoverySource == "" {
			discoverySource = "provider_default"
		}

		status := types.ProviderStatus{
			Name:            provider.Name(),
			Kind:            string(provider.Kind()),
			Healthy:         err == nil,
			Status:          "healthy",
			DefaultModel:    defaultModel,
			Models:          models,
			DiscoverySource: discoverySource,
			RefreshedAt:     caps.RefreshedAt,
		}
		if err != nil {
			status.Healthy = false
			status.Status = "degraded"
			status.Error = err.Error()
			telemetry.Warn(s.logger, ctx, "gateway.providers.health.degraded",
				slog.String("event.name", "gateway.providers.health.degraded"),
				slog.String("gen_ai.provider.name", provider.Name()),
				slog.Any("error", err),
			)
		}
		if s.healthTracker != nil {
			state := s.healthTracker.State(provider.Name())
			if !state.Available {
				status.Healthy = false
				status.Status = "degraded"
				status.Error = providers.FormatHealthStateError(provider.Name(), state)
			}
		}

		statuses = append(statuses, status)
	}

	return &ProviderStatusResult{Providers: statuses}, nil
}

func (s *Service) BudgetStatus(ctx context.Context, key string) (*BudgetStatusResult, error) {
	status, err := s.governor.BudgetStatus(ctx, governor.BudgetFilter{Key: key})
	if err != nil {
		return nil, err
	}
	return &BudgetStatusResult{Status: status}, nil
}

func (s *Service) ResetBudget(ctx context.Context, key string) (*BudgetStatusResult, error) {
	if err := s.governor.ResetBudget(ctx, governor.BudgetFilter{Key: key}); err != nil {
		return nil, err
	}
	return s.BudgetStatus(ctx, key)
}

func (s *Service) BudgetStatusWithFilter(ctx context.Context, filter governor.BudgetFilter) (*BudgetStatusResult, error) {
	status, err := s.governor.BudgetStatus(ctx, filter)
	if err != nil {
		return nil, err
	}
	return &BudgetStatusResult{Status: status}, nil
}

func (s *Service) ResetBudgetWithFilter(ctx context.Context, filter governor.BudgetFilter) (*BudgetStatusResult, error) {
	if err := s.governor.ResetBudget(ctx, filter); err != nil {
		return nil, err
	}
	return s.BudgetStatusWithFilter(ctx, filter)
}

func (s *Service) TopUpBudgetWithFilter(ctx context.Context, filter governor.BudgetFilter, deltaMicros int64) (*BudgetStatusResult, error) {
	if err := s.governor.TopUpBudget(ctx, filter, deltaMicros); err != nil {
		return nil, err
	}
	return s.BudgetStatusWithFilter(ctx, filter)
}

func (s *Service) SetBudgetLimitWithFilter(ctx context.Context, filter governor.BudgetFilter, limitMicros int64) (*BudgetStatusResult, error) {
	if err := s.governor.SetBudgetLimit(ctx, filter, limitMicros); err != nil {
		return nil, err
	}
	return s.BudgetStatusWithFilter(ctx, filter)
}

func (s *Service) Trace(ctx context.Context, requestID string) (*TraceResult, error) {
	if requestID == "" {
		return nil, fmt.Errorf("%w: request_id is required", errClient)
	}

	trace, ok := s.tracer.Get(requestID)
	if !ok {
		return nil, fmt.Errorf("trace %q not found", requestID)
	}

	return &TraceResult{
		RequestID: trace.RequestID,
		TraceID:   trace.TraceID,
		StartedAt: trace.StartedAt,
		Spans:     trace.Spans(),
	}, nil
}

func (s *Service) logRequestSummary(ctx context.Context, metadata ResponseMetadata) {
	telemetry.Info(s.logger, ctx, "gen_ai.gateway.request",
		slog.String("event.name", "gen_ai.gateway.request"),
		slog.String("gen_ai.provider.name", metadata.Provider),
		slog.String("hecate.provider.kind", metadata.ProviderKind),
		slog.String("hecate.route.reason", metadata.RouteReason),
		slog.String("gen_ai.request.model", metadata.RequestedModel),
		slog.String("hecate.model.requested_canonical", metadata.CanonicalRequestedModel),
		slog.String("gen_ai.response.model", metadata.Model),
		slog.String("hecate.model.resolved_canonical", metadata.CanonicalResolvedModel),
		slog.Bool("hecate.cache.hit", metadata.CacheHit),
		slog.String("hecate.cache.type", metadata.CacheType),
		slog.String("hecate.semantic.strategy", metadata.SemanticStrategy),
		slog.String("hecate.semantic.index_type", metadata.SemanticIndexType),
		slog.Float64("hecate.semantic.similarity", metadata.SemanticSimilarity),
		slog.Int("gen_ai.usage.input_tokens", metadata.PromptTokens),
		slog.Int("gen_ai.usage.output_tokens", metadata.CompletionTokens),
		slog.Int("gen_ai.usage.total_tokens", metadata.TotalTokens),
		slog.Int64("hecate.cost.total_micros_usd", metadata.CostMicrosUSD),
		slog.Int("hecate.retry.attempt_count", metadata.AttemptCount),
		slog.Int("hecate.retry.retry_count", metadata.RetryCount),
		slog.String("hecate.failover.from_provider", metadata.FallbackFromProvider),
	)
}

func (s *Service) recordMetrics(metadata ResponseMetadata) {
	if s.metrics == nil {
		return
	}
	s.metrics.RecordChat(
		metadata.Provider,
		metadata.ProviderKind,
		metadata.CacheHit,
		metadata.CacheType,
		metadata.SemanticStrategy,
		metadata.SemanticIndexType,
		metadata.CostMicrosUSD,
	)
}

func validate(req types.ChatRequest) error {
	if len(req.Messages) == 0 {
		return errors.New("at least one message is required")
	}
	for _, msg := range req.Messages {
		if msg.Role == "" {
			return errors.New("message role is required")
		}
	}
	return nil
}
