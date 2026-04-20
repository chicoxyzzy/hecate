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
	Logger    *slog.Logger
	Cache     cache.Store
	Router    router.Router
	Governor  governor.Governor
	Providers providers.Registry
	Pricebook billing.Pricebook
	Tracer    profiler.Tracer
	Metrics   *telemetry.Metrics
}

type Service struct {
	logger     *slog.Logger
	cache      cache.Store
	keyBuilder cache.KeyBuilder
	router     router.Router
	governor   governor.Governor
	providers  providers.Registry
	pricebook  billing.Pricebook
	tracer     profiler.Tracer
	metrics    *telemetry.Metrics
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
	PromptTokens            int
	CompletionTokens        int
	TotalTokens             int
	CostMicrosUSD           int64
}

func NewService(deps Dependencies) *Service {
	return &Service{
		logger:     deps.Logger,
		cache:      deps.Cache,
		keyBuilder: cache.StableKeyBuilder{},
		router:     deps.Router,
		governor:   deps.Governor,
		providers:  deps.Providers,
		pricebook:  deps.Pricebook,
		tracer:     deps.Tracer,
		metrics:    deps.Metrics,
	}
}

func (s *Service) HandleChat(ctx context.Context, req types.ChatRequest) (*ChatResult, error) {
	trace := s.tracer.Start(req.RequestID)
	requestedIdentity := models.BuildIdentity(req.Model, "")
	trace.Record("request.received", map[string]any{
		"message_count":   len(req.Messages),
		"model":           req.Model,
		"canonical_model": requestedIdentity.CanonicalRequested,
	})

	if err := validate(req); err != nil {
		trace.Record("request.invalid", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("%w: %v", errClient, err)
	}

	if err := s.governor.Check(ctx, req); err != nil {
		trace.Record("governor.denied", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("%w: %v", errDenied, err)
	}
	trace.Record("governor.allowed", nil)

	rewrittenReq := s.governor.Rewrite(req)
	if rewrittenReq.Model != req.Model {
		trace.Record("governor.model_rewrite", map[string]any{
			"from": req.Model,
			"to":   rewrittenReq.Model,
		})
	}

	cacheKey, err := s.keyBuilder.Key(rewrittenReq)
	if err != nil {
		return nil, fmt.Errorf("build cache key: %w", err)
	}

	if cached, ok := s.cache.Get(ctx, cacheKey); ok {
		trace.Record("cache.hit", map[string]any{"key": cacheKey})
		identity := models.BuildIdentity(req.Model, cached.Model)
		providerKind := ""
		if provider, ok := s.providers.Get(cached.Route.Provider); ok {
			providerKind = string(provider.Kind())
		}
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
			PromptTokens:            cached.Usage.PromptTokens,
			CompletionTokens:        cached.Usage.CompletionTokens,
			TotalTokens:             cached.Usage.TotalTokens,
			CostMicrosUSD:           cached.Cost.TotalMicrosUSD,
		}
		s.recordMetrics(metadata)
		s.logRequestSummary(metadata)
		return &ChatResult{
			Response: cached,
			Metadata: metadata,
			Trace:    trace,
		}, nil
	}
	trace.Record("cache.miss", map[string]any{"key": cacheKey})

	decision, err := s.router.Route(ctx, rewrittenReq)
	if err != nil {
		trace.Record("router.failed", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("route request: %w", err)
	}
	trace.Record("router.selected", map[string]any{
		"provider": decision.Provider,
		"model":    decision.Model,
		"reason":   decision.Reason,
	})

	provider, ok := s.providers.Get(decision.Provider)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", decision.Provider)
	}

	estimatedUsage := estimateUsage(rewrittenReq)
	estimatedCost, err := s.pricebook.Estimate(decision.Provider, decision.Model, estimatedUsage)
	if err != nil {
		trace.Record("governor.budget_estimate_failed", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("estimate preflight cost: %w", err)
	}
	if err := s.governor.CheckRoute(ctx, rewrittenReq, decision, string(provider.Kind()), estimatedCost.TotalMicrosUSD); err != nil {
		trace.Record("governor.route_denied", map[string]any{
			"error":                err.Error(),
			"provider":             decision.Provider,
			"provider_kind":        string(provider.Kind()),
			"estimated_micros_usd": estimatedCost.TotalMicrosUSD,
		})
		return nil, fmt.Errorf("%w: %v", errDenied, err)
	}
	trace.Record("governor.route_allowed", map[string]any{
		"provider":             decision.Provider,
		"provider_kind":        string(provider.Kind()),
		"estimated_micros_usd": estimatedCost.TotalMicrosUSD,
	})

	rewrittenReq.Model = decision.Model
	trace.Record("provider.call.started", map[string]any{
		"provider": decision.Provider,
		"model":    decision.Model,
	})

	start := time.Now()
	resp, err := provider.Chat(ctx, rewrittenReq)
	if err != nil {
		trace.Record("provider.call.failed", map[string]any{"error": err.Error()})
		return nil, fmt.Errorf("provider call failed: %w", err)
	}
	trace.Record("provider.call.finished", map[string]any{
		"latency_ms": time.Since(start).Milliseconds(),
	})

	resp.Route = decision
	trace.Record("usage.normalized", map[string]any{
		"prompt_tokens":     resp.Usage.PromptTokens,
		"completion_tokens": resp.Usage.CompletionTokens,
		"total_tokens":      resp.Usage.TotalTokens,
	})

	cost, err := s.pricebook.Estimate(decision.Provider, resp.Model, resp.Usage)
	if err != nil {
		return nil, fmt.Errorf("estimate cost: %w", err)
	}
	resp.Cost = cost
	trace.Record("cost.calculated", map[string]any{
		"total_micros_usd": cost.TotalMicrosUSD,
	})
	if err := s.governor.RecordUsage(ctx, rewrittenReq, decision, cost.TotalMicrosUSD); err != nil {
		s.logger.Warn("budget usage record failed", slog.Any("error", err))
		trace.Record("governor.usage_record_failed", map[string]any{"error": err.Error()})
	}

	if err := s.cache.Set(ctx, cacheKey, resp); err != nil {
		s.logger.Warn("cache set failed", slog.Any("error", err))
	}

	identity := models.BuildIdentity(req.Model, resp.Model)
	trace.Record("response.returned", map[string]any{
		"provider":                  decision.Provider,
		"model":                     resp.Model,
		"canonical_requested_model": identity.CanonicalRequested,
		"canonical_resolved_model":  identity.CanonicalResolved,
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
		PromptTokens:            resp.Usage.PromptTokens,
		CompletionTokens:        resp.Usage.CompletionTokens,
		TotalTokens:             resp.Usage.TotalTokens,
		CostMicrosUSD:           cost.TotalMicrosUSD,
	}
	s.recordMetrics(metadata)
	s.logRequestSummary(metadata)

	return &ChatResult{
		Response: resp,
		Metadata: metadata,
		Trace:    trace,
	}, nil
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
			s.logger.Warn("provider capabilities unavailable",
				slog.String("provider", provider.Name()),
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
			s.logger.Warn("provider health degraded",
				slog.String("provider", provider.Name()),
				slog.Any("error", err),
			)
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

func (s *Service) logRequestSummary(metadata ResponseMetadata) {
	s.logger.Info("gateway_chat_request",
		slog.String("request_id", metadata.RequestID),
		slog.String("provider", metadata.Provider),
		slog.String("provider_kind", metadata.ProviderKind),
		slog.String("route_reason", metadata.RouteReason),
		slog.String("requested_model", metadata.RequestedModel),
		slog.String("canonical_requested_model", metadata.CanonicalRequestedModel),
		slog.String("resolved_model", metadata.Model),
		slog.String("canonical_resolved_model", metadata.CanonicalResolvedModel),
		slog.Bool("cache_hit", metadata.CacheHit),
		slog.Int("prompt_tokens", metadata.PromptTokens),
		slog.Int("completion_tokens", metadata.CompletionTokens),
		slog.Int("total_tokens", metadata.TotalTokens),
		slog.Int64("cost_micros_usd", metadata.CostMicrosUSD),
	)
}

func (s *Service) recordMetrics(metadata ResponseMetadata) {
	if s.metrics == nil {
		return
	}
	s.metrics.RecordChat(metadata.Provider, metadata.ProviderKind, metadata.CacheHit, metadata.CostMicrosUSD)
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
