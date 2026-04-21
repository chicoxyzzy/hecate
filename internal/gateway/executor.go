package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/pkg/types"
)

type ProviderExecutor interface {
	Execute(ctx context.Context, trace *profiler.Trace, req types.ChatRequest, initial types.RouteDecision) (*providerCallResult, error)
}

type ResilientExecutor struct {
	router        router.Router
	governor      governor.Governor
	providers     providers.Registry
	healthTracker providers.HealthTracker
	pricebook     billing.Pricebook
	options       ResilienceOptions
	sleep         func(context.Context, time.Duration) error
}

func NewResilientExecutor(
	router router.Router,
	governor governor.Governor,
	providers providers.Registry,
	healthTracker providers.HealthTracker,
	pricebook billing.Pricebook,
	options ResilienceOptions,
) *ResilientExecutor {
	return &ResilientExecutor{
		router:        router,
		governor:      governor,
		providers:     providers,
		healthTracker: healthTracker,
		pricebook:     pricebook,
		options:       normalizeResilienceOptions(options),
		sleep:         sleepContext,
	}
}

func (e *ResilientExecutor) Execute(ctx context.Context, trace *profiler.Trace, req types.ChatRequest, initial types.RouteDecision) (*providerCallResult, error) {
	candidates := []types.RouteDecision{initial}
	if e.options.FailoverEnabled {
		candidates = append(candidates, e.router.Fallbacks(ctx, req, initial)...)
	}

	totalAttempts := 0
	totalRetries := 0
	var lastErr error

	for index, candidate := range candidates {
		provider, ok := e.providers.Get(candidate.Provider)
		if !ok {
			lastErr = fmt.Errorf("provider %q not found", candidate.Provider)
			continue
		}

		estimatedUsage := estimateUsage(withResolvedModel(req, candidate.Model))
		estimatedCost, err := e.pricebook.Estimate(candidate.Provider, candidate.Model, estimatedUsage)
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
			if err := e.governor.CheckRoute(ctx, req, candidate, string(provider.Kind()), estimatedCost.TotalMicrosUSD); err != nil {
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
		for attempt := 1; attempt <= e.options.MaxAttempts; attempt++ {
			totalAttempts++
			trace.Record("provider.call.started", map[string]any{
				"gen_ai.provider.name":      candidate.Provider,
				"gen_ai.request.model":      candidate.Model,
				"hecate.retry.attempt":      attempt,
				"hecate.provider.index":     index,
				"hecate.retry.max_attempts": e.options.MaxAttempts,
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
				if e.healthTracker != nil {
					e.healthTracker.RecordSuccess(candidate.Provider)
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
			if attempt >= e.options.MaxAttempts {
				break
			}

			totalRetries++
			backoff := e.retryDelay(attempt)
			trace.Record("provider.retry.scheduled", map[string]any{
				"gen_ai.provider.name":      candidate.Provider,
				"gen_ai.request.model":      candidate.Model,
				"hecate.retry.attempt":      attempt,
				"hecate.retry.next_attempt": attempt + 1,
				"hecate.retry.backoff_ms":   backoff.Milliseconds(),
			})
			if err := e.sleep(ctx, backoff); err != nil {
				return nil, fmt.Errorf("wait for retry backoff: %w", err)
			}
		}

		if e.healthTracker != nil && providers.IsRetryableError(lastErr) {
			e.healthTracker.RecordFailure(candidate.Provider, lastErr)
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

func (e *ResilientExecutor) retryDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return e.options.RetryBackoff
	}
	return time.Duration(attempt) * e.options.RetryBackoff
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
