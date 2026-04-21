package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type ProviderExecutor interface {
	Execute(ctx context.Context, trace *profiler.Trace, req types.ChatRequest, initial types.RouteDecision) (*providerCallResult, error)
}

type ResilientExecutor struct {
	router        router.Router
	preflight     RoutePreflight
	providers     providers.Registry
	healthTracker providers.HealthTracker
	options       ResilienceOptions
	sleep         func(context.Context, time.Duration) error
}

func NewResilientExecutor(
	router router.Router,
	preflight RoutePreflight,
	providers providers.Registry,
	healthTracker providers.HealthTracker,
	options ResilienceOptions,
) *ResilientExecutor {
	return &ResilientExecutor{
		router:        router,
		preflight:     preflight,
		providers:     providers,
		healthTracker: healthTracker,
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
		recordTrace(trace, "router.candidate.considered", "routing", map[string]any{
			telemetry.AttrGenAIProviderName:          candidate.Provider,
			telemetry.AttrGenAIRequestModel:          candidate.Model,
			telemetry.AttrHecateProviderKind:         candidate.ProviderKind,
			telemetry.AttrHecateRouteReason:          candidate.Reason,
			telemetry.AttrHecateProviderIndex:        index,
			telemetry.AttrHecateRouteOutcome:         "considered",
			telemetry.AttrHecateProviderHealthStatus: healthStatus(e.healthTracker, candidate.Provider),
		})

		provider, ok := e.providers.Get(candidate.Provider)
		if !ok {
			lastErr = fmt.Errorf("provider %q not found", candidate.Provider)
			recordTraceError(trace, "router.candidate.skipped", "routing", errorKindRouterFailed, lastErr, map[string]any{
				telemetry.AttrGenAIProviderName:          candidate.Provider,
				telemetry.AttrGenAIRequestModel:          candidate.Model,
				telemetry.AttrHecateProviderKind:         candidate.ProviderKind,
				telemetry.AttrHecateRouteReason:          candidate.Reason,
				telemetry.AttrHecateProviderIndex:        index,
				telemetry.AttrHecateRouteOutcome:         "skipped",
				telemetry.AttrHecateRouteSkipReason:      string(RoutePreflightProviderNotFound),
				telemetry.AttrHecateProviderHealthStatus: healthStatus(e.healthTracker, candidate.Provider),
			})
			continue
		}

		preflight, err := e.preflight.Evaluate(ctx, req, candidate)
		if err != nil {
			lastErr = err
			if preflightErr, ok := AsRoutePreflightError(err); ok {
				if index == 0 && preflightErr.Kind == RoutePreflightCostEstimate {
					recordTraceError(trace, "governor.budget_estimate_failed", "governor", errorKindBudgetEstimateFailed, preflightErr, nil)
					return nil, preflightErr
				}
				reason := string(preflightErr.Kind)
				if preflightErr.Kind == RoutePreflightCostEstimate {
					reason = "cost_estimate_failed"
				}
				if preflightErr.Kind == RoutePreflightRouteDenied {
					reason = "route_denied"
					lastErr = fmt.Errorf("%w: %v", errDenied, preflightErr.Err)
				}
				eventName := "router.candidate.skipped"
				outcome := "skipped"
				if preflightErr.Kind == RoutePreflightRouteDenied {
					eventName = "router.candidate.denied"
					outcome = "denied"
				}
				recordTraceError(trace, eventName, "routing", reason, preflightErr, map[string]any{
					telemetry.AttrGenAIProviderName:            candidate.Provider,
					telemetry.AttrGenAIRequestModel:            candidate.Model,
					telemetry.AttrHecateProviderKind:           firstNonEmpty(preflightErr.ProviderKind, candidate.ProviderKind),
					telemetry.AttrHecateRouteReason:            candidate.Reason,
					telemetry.AttrHecateProviderIndex:          index,
					telemetry.AttrHecateRouteOutcome:           outcome,
					telemetry.AttrHecateRouteSkipReason:        reason,
					telemetry.AttrHecateProviderHealthStatus:   healthStatus(e.healthTracker, candidate.Provider),
					telemetry.AttrHecateCostEstimatedMicrosUSD: preflightErr.EstimatedCostMicros,
				})
				recordTraceError(trace, "provider.failover.skipped", "provider", reason, preflightErr, map[string]any{
					telemetry.AttrGenAIProviderName:            candidate.Provider,
					telemetry.AttrGenAIRequestModel:            candidate.Model,
					telemetry.AttrHecateFailoverReason:         reason,
					telemetry.AttrHecateCostEstimatedMicrosUSD: preflightErr.EstimatedCostMicros,
				})
				continue
			}
			if index == 0 {
				return nil, err
			}
			continue
		}

		if index > 0 {
			recordTrace(trace, "provider.failover.selected", "provider", map[string]any{
				telemetry.AttrGenAIProviderName:            candidate.Provider,
				telemetry.AttrGenAIRequestModel:            candidate.Model,
				telemetry.AttrHecateProviderKind:           preflight.ProviderKind,
				telemetry.AttrHecateFailoverFromProvider:   initial.Provider,
				telemetry.AttrHecateFailoverToProvider:     candidate.Provider,
				telemetry.AttrHecateCostEstimatedMicrosUSD: preflight.EstimatedCost.TotalMicrosUSD,
			})
		}

		recordTrace(trace, "router.candidate.selected", "routing", map[string]any{
			telemetry.AttrGenAIProviderName:            candidate.Provider,
			telemetry.AttrGenAIRequestModel:            candidate.Model,
			telemetry.AttrHecateProviderKind:           preflight.ProviderKind,
			telemetry.AttrHecateRouteReason:            candidate.Reason,
			telemetry.AttrHecateProviderIndex:          index,
			telemetry.AttrHecateRouteOutcome:           "selected",
			telemetry.AttrHecateProviderHealthStatus:   healthStatus(e.healthTracker, candidate.Provider),
			telemetry.AttrHecateCostEstimatedMicrosUSD: preflight.EstimatedCost.TotalMicrosUSD,
		})

		attemptReq := withResolvedModel(req, candidate.Model)
		for attempt := 1; attempt <= e.options.MaxAttempts; attempt++ {
			totalAttempts++
			recordTrace(trace, "provider.call.started", "provider", map[string]any{
				telemetry.AttrGenAIProviderName:      candidate.Provider,
				telemetry.AttrGenAIRequestModel:      candidate.Model,
				telemetry.AttrHecateRetryAttempt:     attempt,
				telemetry.AttrHecateProviderIndex:    index,
				telemetry.AttrHecateRetryMaxAttempts: e.options.MaxAttempts,
				telemetry.AttrHecateFailoverActive:   index > 0,
			})

			start := time.Now()
			resp, err := provider.Chat(ctx, attemptReq)
			latency := time.Since(start)
			if err == nil {
				recordTrace(trace, "provider.call.finished", "provider", map[string]any{
					telemetry.AttrGenAIProviderName:       candidate.Provider,
					telemetry.AttrGenAIRequestModel:       candidate.Model,
					telemetry.AttrHecateRetryAttempt:      attempt,
					telemetry.AttrHecateProviderIndex:     index,
					telemetry.AttrHecateProviderLatencyMS: latency.Milliseconds(),
				})
				if e.healthTracker != nil {
					e.healthTracker.Observe(candidate.Provider, providers.HealthObservation{Duration: latency})
				}
				return &providerCallResult{
					Response:             resp,
					Decision:             candidate,
					ProviderKind:         preflight.ProviderKind,
					AttemptCount:         totalAttempts,
					RetryCount:           totalRetries,
					FallbackFromProvider: fallbackFrom(initial.Provider, candidate.Provider),
				}, nil
			}

			lastErr = fmt.Errorf("provider %s call failed: %w", candidate.Provider, err)
			recordTraceError(trace, "provider.call.failed", "provider", errorKindProviderCallFailed, err, map[string]any{
				telemetry.AttrGenAIProviderName:       candidate.Provider,
				telemetry.AttrGenAIRequestModel:       candidate.Model,
				telemetry.AttrHecateRetryAttempt:      attempt,
				telemetry.AttrHecateProviderIndex:     index,
				telemetry.AttrHecateRetryRetryable:    providers.IsRetryableError(err),
				telemetry.AttrHecateProviderLatencyMS: latency.Milliseconds(),
			})
			if e.healthTracker != nil {
				e.healthTracker.Observe(candidate.Provider, providers.HealthObservation{
					Duration: latency,
					Error:    err,
				})
			}

			if !providers.IsRetryableError(err) {
				break
			}
			if attempt >= e.options.MaxAttempts {
				break
			}

			totalRetries++
			backoff := e.retryDelay(attempt)
			recordTrace(trace, "provider.retry.scheduled", "provider", map[string]any{
				telemetry.AttrGenAIProviderName:      candidate.Provider,
				telemetry.AttrGenAIRequestModel:      candidate.Model,
				telemetry.AttrHecateRetryAttempt:     attempt,
				telemetry.AttrHecateRetryNextAttempt: attempt + 1,
				telemetry.AttrHecateRetryBackoffMS:   backoff.Milliseconds(),
			})
			if err := e.sleep(ctx, backoff); err != nil {
				recordTraceError(trace, "provider.retry.backoff_failed", "provider", errorKindRetryBackoffFailed, err, map[string]any{
					telemetry.AttrGenAIProviderName:    candidate.Provider,
					telemetry.AttrGenAIRequestModel:    candidate.Model,
					telemetry.AttrHecateRetryAttempt:   attempt,
					telemetry.AttrHecateRetryBackoffMS: backoff.Milliseconds(),
				})
				return nil, fmt.Errorf("wait for retry backoff: %w", err)
			}
		}

		if e.healthTracker != nil && providers.IsRetryableError(lastErr) {
			recordTraceError(trace, "provider.health.degraded", "provider", errorKindProviderHealth, lastErr, map[string]any{
				telemetry.AttrGenAIProviderName:          candidate.Provider,
				telemetry.AttrHecateProviderHealthStatus: string(e.healthTracker.State(candidate.Provider).Status),
			})
		}

		if index < len(candidates)-1 && providers.IsRetryableError(lastErr) {
			recordTraceError(trace, "provider.failover.triggered", "provider", errorKindProviderCallFailed, lastErr, map[string]any{
				telemetry.AttrGenAIProviderName:          candidate.Provider,
				telemetry.AttrGenAIRequestModel:          candidate.Model,
				telemetry.AttrHecateFailoverFromProvider: candidate.Provider,
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

func healthStatus(tracker providers.HealthTracker, provider string) string {
	if tracker == nil {
		return ""
	}
	return string(tracker.State(provider).Status)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
