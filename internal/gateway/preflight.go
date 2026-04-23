package gateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/hecate/agent-runtime/internal/billing"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

type RoutePreflight interface {
	Evaluate(ctx context.Context, req types.ChatRequest, decision types.RouteDecision) (*RoutePreflightResult, error)
}

type RoutePreflightResult struct {
	ProviderKind   string
	EstimatedUsage types.Usage
	EstimatedCost  types.CostBreakdown
}

type RoutePreflightErrorKind string

const (
	RoutePreflightProviderNotFound RoutePreflightErrorKind = "provider_not_found"
	RoutePreflightCostEstimate     RoutePreflightErrorKind = "preflight_price_missing"
	RoutePreflightRouteDenied      RoutePreflightErrorKind = "route_denied"
)

type RoutePreflightError struct {
	Kind                RoutePreflightErrorKind
	Provider            string
	Model               string
	ProviderKind        string
	EstimatedCostMicros int64
	Err                 error
}

func (e *RoutePreflightError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *RoutePreflightError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func AsRoutePreflightError(err error) (*RoutePreflightError, bool) {
	var target *RoutePreflightError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

type DefaultRoutePreflight struct {
	governor  governor.Governor
	providers providers.Registry
	pricebook billing.Pricebook
}

func NewDefaultRoutePreflight(governor governor.Governor, providers providers.Registry, pricebook billing.Pricebook) *DefaultRoutePreflight {
	return &DefaultRoutePreflight{
		governor:  governor,
		providers: providers,
		pricebook: pricebook,
	}
}

func (p *DefaultRoutePreflight) Evaluate(ctx context.Context, req types.ChatRequest, decision types.RouteDecision) (*RoutePreflightResult, error) {
	provider, ok := p.providers.Get(decision.Provider)
	if !ok {
		return nil, &RoutePreflightError{
			Kind:     RoutePreflightProviderNotFound,
			Provider: decision.Provider,
			Model:    decision.Model,
			Err:      fmt.Errorf("provider %q not found", decision.Provider),
		}
	}

	estimatedUsage := estimateUsage(withResolvedModel(req, decision.Model))
	estimatedCost, err := p.pricebook.Estimate(decision.Provider, decision.Model, estimatedUsage)
	if err != nil {
		return nil, &RoutePreflightError{
			Kind:         RoutePreflightCostEstimate,
			Provider:     decision.Provider,
			Model:        decision.Model,
			ProviderKind: string(provider.Kind()),
			Err:          fmt.Errorf("estimate preflight cost: %w", err),
		}
	}

	if err := p.governor.CheckRoute(ctx, req, decision, string(provider.Kind()), estimatedCost.TotalMicrosUSD); err != nil {
		return nil, &RoutePreflightError{
			Kind:                RoutePreflightRouteDenied,
			Provider:            decision.Provider,
			Model:               decision.Model,
			ProviderKind:        string(provider.Kind()),
			EstimatedCostMicros: estimatedCost.TotalMicrosUSD,
			Err:                 err,
		}
	}

	return &RoutePreflightResult{
		ProviderKind:   string(provider.Kind()),
		EstimatedUsage: estimatedUsage,
		EstimatedCost:  estimatedCost,
	}, nil
}
