package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/cache"
	"github.com/hecate/agent-runtime/internal/catalog"
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
	CacheRuntime    CacheRuntime
	Finalizer       ResponseFinalizer
	Preflight       RoutePreflight
	Semantic        cache.SemanticStore
	SemanticOptions SemanticOptions
	Resilience      ResilienceOptions
	Executor        ProviderExecutor
	Router          router.Router
	Catalog         catalog.Catalog
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
	cacheRuntime    CacheRuntime
	finalizer       ResponseFinalizer
	preflight       RoutePreflight
	semantic        cache.SemanticStore
	semanticOptions SemanticOptions
	keyBuilder      cache.KeyBuilder
	executor        ProviderExecutor
	router          router.Router
	catalog         catalog.Catalog
	governor        governor.Governor
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
	Route     types.RouteDecisionReport
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

type ExecutionPlan struct {
	OriginalRequest  types.ChatRequest
	Request          types.ChatRequest
	CacheKey         string
	Route            types.RouteDecision
	ProviderKind     string
	SemanticEligible bool
	SemanticQuery    cache.SemanticQuery
	SemanticScope    string
}

func NewService(deps Dependencies) *Service {
	cat := deps.Catalog
	if cat == nil {
		cat = catalog.NewRegistryCatalog(deps.Providers, deps.HealthTracker)
	}

	preflight := deps.Preflight
	if preflight == nil {
		preflight = NewDefaultRoutePreflight(deps.Governor, deps.Providers, deps.Pricebook)
	}

	executor := deps.Executor
	if executor == nil {
		executor = NewResilientExecutor(
			deps.Router,
			preflight,
			deps.Providers,
			deps.HealthTracker,
			deps.Resilience,
		)
	}

	cacheRuntime := deps.CacheRuntime
	if cacheRuntime == nil {
		cacheRuntime = NewGatewayCacheRuntime(
			deps.Logger,
			deps.Cache,
			deps.Semantic,
			deps.SemanticOptions,
			deps.Governor,
			deps.Providers,
		)
	}

	finalizer := deps.Finalizer
	if finalizer == nil {
		finalizer = NewDefaultResponseFinalizer(
			deps.Logger,
			deps.Governor,
			deps.Pricebook,
			deps.Metrics,
		)
	}

	return &Service{
		cacheRuntime:    cacheRuntime,
		finalizer:       finalizer,
		preflight:       preflight,
		semantic:        deps.Semantic,
		semanticOptions: deps.SemanticOptions,
		keyBuilder:      cache.StableKeyBuilder{},
		executor:        executor,
		router:          deps.Router,
		catalog:         cat,
		governor:        deps.Governor,
		tracer:          deps.Tracer,
		metrics:         deps.Metrics,
	}
}

func (s *Service) HandleChat(ctx context.Context, req types.ChatRequest) (result *ChatResult, err error) {
	startedAt := time.Now()
	defer s.recordRequestOutcome(ctx, err, time.Since(startedAt))

	trace := s.tracer.Start(req.RequestID)
	defer trace.Finalize()
	ctx = telemetry.WithTraceIDs(ctx, trace.TraceID, trace.RootSpanID())

	plan, err := s.buildExecutionPlan(ctx, trace, req)
	if err != nil {
		return nil, err
	}

	if cached, ok, err := s.cacheRuntime.Lookup(ctx, trace, plan); err != nil {
		return nil, err
	} else if ok {
		return s.finalizer.FinalizeCache(ctx, trace, plan.OriginalRequest, cached), nil
	}

	return s.executePlan(ctx, trace, plan)
}

func (s *Service) recordRequestOutcome(ctx context.Context, err error, duration time.Duration) {
	if s.metrics == nil {
		return
	}
	result := telemetry.ResultSuccess
	if err != nil {
		result = telemetry.ResultError
		if IsDeniedError(err) {
			result = telemetry.ResultDenied
		}
	}
	s.metrics.RecordRequestOutcome(ctx, result, duration)
}

func (s *Service) buildExecutionPlan(ctx context.Context, trace *profiler.Trace, req types.ChatRequest) (*ExecutionPlan, error) {
	requestedIdentity := models.BuildIdentity(req.Model, "")
	trace.Record("request.received", map[string]any{
		"gen_ai.request.message_count": len(req.Messages),
		"gen_ai.request.model":         req.Model,
		"hecate.model.canonical":       requestedIdentity.CanonicalRequested,
	})

	if err := validate(req); err != nil {
		recordTraceError(trace, "request.invalid", "request", errorKindInvalidRequest, err, nil)
		return nil, fmt.Errorf("%w: %v", errClient, err)
	}

	if err := s.governor.Check(ctx, req); err != nil {
		recordTraceError(trace, "governor.denied", "governor", errorKindRequestDenied, err, map[string]any{
			"hecate.governor.result": "denied",
		})
		return nil, fmt.Errorf("%w: %v", errDenied, err)
	}
	recordTrace(trace, "governor.allowed", "governor", map[string]any{
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

	decision, err := s.router.Route(ctx, rewrittenReq)
	if err != nil {
		recordTraceError(trace, "router.failed", "routing", errorKindRouterFailed, err, nil)
		return nil, fmt.Errorf("route request: %w", err)
	}
	recordTrace(trace, "router.selected", "routing", map[string]any{
		"gen_ai.provider.name": decision.Provider,
		"gen_ai.request.model": decision.Model,
		"hecate.route.reason":  decision.Reason,
		"hecate.provider.kind": decision.ProviderKind,
	})

	preflight, err := s.preflight.Evaluate(ctx, rewrittenReq, decision)
	if err != nil {
		if preflightErr, ok := AsRoutePreflightError(err); ok {
			switch preflightErr.Kind {
			case RoutePreflightCostEstimate:
				recordTraceError(trace, "governor.budget_estimate_failed", "governor", errorKindBudgetEstimateFailed, preflightErr, nil)
				return nil, preflightErr
			case RoutePreflightRouteDenied:
				recordTraceError(trace, "governor.route_denied", "governor", errorKindRouteDenied, preflightErr, map[string]any{
					"gen_ai.provider.name":         decision.Provider,
					"hecate.provider.kind":         preflightErr.ProviderKind,
					"hecate.cost.estimated_micros": preflightErr.EstimatedCostMicros,
					"hecate.governor.route_result": "denied",
				})
				return nil, fmt.Errorf("%w: %v", errDenied, preflightErr.Err)
			}
		}
		return nil, err
	}
	recordTrace(trace, "governor.route_allowed", "governor", map[string]any{
		"gen_ai.provider.name":         decision.Provider,
		"hecate.provider.kind":         preflight.ProviderKind,
		"hecate.cost.estimated_micros": preflight.EstimatedCost.TotalMicrosUSD,
		"hecate.governor.route_result": "allowed",
	})

	plan := &ExecutionPlan{
		OriginalRequest: req,
		Request:         rewrittenReq,
		CacheKey:        cacheKey,
		Route:           decision,
		ProviderKind:    preflight.ProviderKind,
	}

	if s.semantic != nil && s.semanticOptions.Enabled && cache.EligibleForSemanticCache(rewrittenReq, s.semanticOptions.MaxTextChars) {
		plan.SemanticEligible = true
		plan.SemanticScope = cache.BuildSemanticNamespace(rewrittenReq, decision)
		plan.SemanticQuery = cache.SemanticQuery{
			Namespace:     plan.SemanticScope,
			Text:          cache.BuildSemanticText(rewrittenReq, s.semanticOptions.MaxTextChars),
			MinSimilarity: s.semanticOptions.MinSimilarity,
			MaxTextChars:  s.semanticOptions.MaxTextChars,
		}
	}

	return plan, nil
}

func (s *Service) executePlan(ctx context.Context, trace *profiler.Trace, plan *ExecutionPlan) (*ChatResult, error) {
	callResult, err := s.executor.Execute(ctx, trace, plan.Request, plan.Route)
	if err != nil {
		return nil, err
	}
	result, err := s.finalizer.FinalizeExecution(ctx, trace, plan, callResult)
	if err != nil {
		return nil, err
	}
	s.cacheRuntime.Store(ctx, trace, plan, callResult.Decision, result.Response)
	return result, nil
}

type providerCallResult struct {
	Response             *types.ChatResponse
	Decision             types.RouteDecision
	ProviderKind         string
	AttemptCount         int
	RetryCount           int
	FallbackFromProvider string
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

func (s *Service) ListModels(ctx context.Context) (*ModelsResult, error) {
	seen := make(map[string]struct{})
	modelsOut := make([]types.ModelInfo, 0, 16)

	for _, entry := range s.catalog.Snapshot(ctx) {
		for _, modelID := range entry.Models {
			key := entry.Name + "/" + modelID
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			modelsOut = append(modelsOut, types.ModelInfo{
				ID:              modelID,
				Provider:        entry.Name,
				Kind:            string(entry.Kind),
				OwnedBy:         entry.Name,
				Default:         modelID == entry.DefaultModel,
				DiscoverySource: entry.DiscoverySource,
			})
		}
	}

	return &ModelsResult{Models: modelsOut}, nil
}

func (s *Service) ProviderStatus(ctx context.Context) (*ProviderStatusResult, error) {
	entries := s.catalog.Snapshot(ctx)
	statuses := make([]types.ProviderStatus, 0, len(entries))
	for _, entry := range entries {
		status := types.ProviderStatus{
			Name:            entry.Name,
			Kind:            string(entry.Kind),
			Healthy:         entry.Healthy,
			Status:          entry.Status,
			DefaultModel:    entry.DefaultModel,
			Models:          append([]string(nil), entry.Models...),
			DiscoverySource: entry.DiscoverySource,
			Error:           entry.Error,
		}
		if entry.RefreshedAt != "" {
			if ts, err := time.Parse(time.RFC3339, entry.RefreshedAt); err == nil {
				status.RefreshedAt = ts
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

	spans := trace.Spans()
	return &TraceResult{
		RequestID: trace.RequestID,
		TraceID:   trace.TraceID,
		StartedAt: trace.StartedAt,
		Spans:     spans,
		Route:     buildRouteDecisionReport(spans),
	}, nil
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
