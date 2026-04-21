package router

import (
	"context"
	"fmt"

	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Router interface {
	Route(ctx context.Context, req types.ChatRequest) (types.RouteDecision, error)
	Fallbacks(ctx context.Context, req types.ChatRequest, current types.RouteDecision) []types.RouteDecision
}

type RuleRouter struct {
	defaultModel     string
	defaultProvider  string
	fallbackProvider string
	strategy         string
	providers        providers.Registry
	healthTracker    providers.HealthTracker
}

func NewRuleRouter(defaultProvider, defaultModel, strategy, fallbackProvider string, registry providers.Registry) *RuleRouter {
	return &RuleRouter{
		defaultModel:     defaultModel,
		defaultProvider:  defaultProvider,
		fallbackProvider: fallbackProvider,
		strategy:         strategy,
		providers:        registry,
	}
}

func (r *RuleRouter) SetHealthTracker(tracker providers.HealthTracker) {
	r.healthTracker = tracker
}

func (r *RuleRouter) Route(ctx context.Context, req types.ChatRequest) (types.RouteDecision, error) {
	explicitProvider := req.Metadata["provider"]
	model := req.Model
	reason := "explicit_model"
	if model == "" {
		model = r.defaultModel
		reason = "default_model"
	}
	if model == "" {
		return types.RouteDecision{}, fmt.Errorf("no model available for routing")
	}

	if explicitProvider != "" {
		provider, ok := r.providers.Get(explicitProvider)
		if !ok {
			return types.RouteDecision{}, fmt.Errorf("provider %q not found", explicitProvider)
		}

		routedModel := model
		explicitReason := "explicit_provider"
		if req.Model != "" {
			explicitReason = "explicit_provider_model"
			if !r.providerSupportsModel(ctx, provider, model) {
				return types.RouteDecision{}, fmt.Errorf("provider %q does not support explicit model %q", explicitProvider, model)
			}
		} else {
			routedModel = r.providerDefaultModel(ctx, provider)
			if routedModel == "" {
				return types.RouteDecision{}, fmt.Errorf("provider %q has no default model for routing", explicitProvider)
			}
		}

		return types.RouteDecision{
			Provider: provider.Name(),
			Model:    routedModel,
			Reason:   explicitReason,
		}, nil
	}

	if reason == "explicit_model" {
		if provider, ok := r.selectExplicitModelProvider(ctx, model); ok {
			return types.RouteDecision{
				Provider: provider.Name(),
				Model:    model,
				Reason:   r.strategyReason(reason, provider),
			}, nil
		}
		return types.RouteDecision{}, fmt.Errorf("no provider supports explicit model %q", model)
	}

	provider, routedModel, reasonLabel, err := r.selectDefaultProviderAndModel(ctx, model)
	if err != nil {
		return types.RouteDecision{}, err
	}
	return types.RouteDecision{
		Provider: provider.Name(),
		Model:    routedModel,
		Reason:   reasonLabel,
	}, nil
}

func (r *RuleRouter) Fallbacks(ctx context.Context, req types.ChatRequest, current types.RouteDecision) []types.RouteDecision {
	if req.Metadata["provider"] != "" {
		return nil
	}

	explicitModel := req.Model != ""
	seen := map[string]struct{}{
		current.Provider + "/" + current.Model: {},
	}
	ordered := r.orderedFallbackProviders()
	out := make([]types.RouteDecision, 0, len(ordered))

	for _, provider := range ordered {
		if provider.Name() == current.Provider {
			continue
		}
		if !r.providerHealthyForAutoRouting(ctx, provider) {
			continue
		}

		model := ""
		if explicitModel {
			if !r.providerSupportsModel(ctx, provider, req.Model) {
				continue
			}
			model = req.Model
		} else {
			model = r.providerDefaultModel(ctx, provider)
			if model == "" {
				continue
			}
		}

		key := provider.Name() + "/" + model
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, types.RouteDecision{
			Provider: provider.Name(),
			Model:    model,
			Reason:   current.Reason + "_failover",
		})
	}

	return out
}

func (r *RuleRouter) selectExplicitModelProvider(ctx context.Context, model string) (providers.Provider, bool) {
	if r.strategy == "local_first" {
		for _, provider := range r.providers.All() {
			if provider.Kind() == providers.KindLocal && r.providerHealthyForAutoRouting(ctx, provider) && r.providerSupportsModel(ctx, provider, model) {
				return provider, true
			}
		}
		if r.fallbackProvider != "" {
			if provider, ok := r.providers.Get(r.fallbackProvider); ok && r.providerHealthyForAutoRouting(ctx, provider) && r.providerSupportsModel(ctx, provider, model) {
				return provider, true
			}
		}
		for _, provider := range r.providers.All() {
			if provider.Kind() == providers.KindCloud && r.providerHealthyForAutoRouting(ctx, provider) && r.providerSupportsModel(ctx, provider, model) {
				return provider, true
			}
		}
	}

	if r.defaultProvider != "" {
		if provider, ok := r.providers.Get(r.defaultProvider); ok && r.providerHealthyForAutoRouting(ctx, provider) && r.providerSupportsModel(ctx, provider, model) {
			return provider, true
		}
	}
	for _, provider := range r.providers.All() {
		if r.providerHealthyForAutoRouting(ctx, provider) && r.providerSupportsModel(ctx, provider, model) {
			return provider, true
		}
	}
	return nil, false
}

func (r *RuleRouter) selectDefaultProviderAndModel(ctx context.Context, model string) (providers.Provider, string, string, error) {
	if r.strategy == "local_first" {
		skippedUnhealthyLocal := false
		for _, provider := range r.providers.All() {
			if provider.Kind() == providers.KindLocal {
				if !r.providerHealthyForAutoRouting(ctx, provider) {
					skippedUnhealthyLocal = true
					continue
				}
				if model := r.providerDefaultModel(ctx, provider); model != "" {
					return provider, model, "default_model_local_first", nil
				}
				if r.providerSupportsModel(ctx, provider, model) {
					return provider, model, "default_model_local_first", nil
				}
			}
		}
		if r.fallbackProvider != "" {
			if provider, ok := r.providers.Get(r.fallbackProvider); ok && r.providerHealthyForAutoRouting(ctx, provider) {
				reason := "default_model_fallback"
				if skippedUnhealthyLocal {
					reason = "default_model_fallback_unhealthy_local"
				}
				if model := r.providerDefaultModel(ctx, provider); model != "" {
					return provider, model, reason, nil
				}
				return provider, model, reason, nil
			}
		}
	}

	skippedDegraded := false
	if r.defaultProvider != "" {
		if provider, ok := r.providers.Get(r.defaultProvider); ok {
			if !r.providerHealthyForAutoRouting(ctx, provider) {
				skippedDegraded = true
			} else {
				if model := r.providerDefaultModel(ctx, provider); model != "" {
					return provider, model, "default_model", nil
				}
				return provider, model, "default_model", nil
			}
		}
	}
	for _, provider := range r.providers.All() {
		if !r.providerHealthyForAutoRouting(ctx, provider) {
			skippedDegraded = true
			continue
		}
		if model := r.providerDefaultModel(ctx, provider); model != "" {
			reason := "default_model"
			if skippedDegraded {
				reason = "default_model_fallback_degraded_provider"
			}
			return provider, model, reason, nil
		}
	}
	return nil, "", "", fmt.Errorf("no provider available for default routing")
}

func (r *RuleRouter) strategyReason(base string, provider providers.Provider) string {
	if r.strategy == "local_first" && provider.Kind() == providers.KindLocal {
		return base + "_local_first"
	}
	if r.strategy == "local_first" && provider.Kind() == providers.KindCloud {
		return base + "_fallback"
	}
	return base
}

func (r *RuleRouter) providerSupportsModel(ctx context.Context, provider providers.Provider, model string) bool {
	capabilities, err := provider.Capabilities(ctx)
	if err == nil && len(capabilities.Models) > 0 {
		for _, candidate := range capabilities.Models {
			if candidate == model {
				return true
			}
		}
	}
	return provider.Supports(model)
}

func (r *RuleRouter) providerDefaultModel(ctx context.Context, provider providers.Provider) string {
	capabilities, err := provider.Capabilities(ctx)
	if err == nil && capabilities.DefaultModel != "" {
		return capabilities.DefaultModel
	}
	return provider.DefaultModel()
}

func (r *RuleRouter) providerHealthyForAutoRouting(ctx context.Context, provider providers.Provider) bool {
	if r.healthTracker != nil {
		if !r.healthTracker.State(provider.Name()).Available {
			return false
		}
	}
	if provider.Kind() == providers.KindLocal {
		_, err := provider.Capabilities(ctx)
		return err == nil
	}
	return true
}

func (r *RuleRouter) orderedFallbackProviders() []providers.Provider {
	candidates := make([]providers.Provider, 0, len(r.providers.All())+2)
	seen := make(map[string]struct{}, len(r.providers.All())+2)
	appendProvider := func(name string) {
		if name == "" {
			return
		}
		provider, ok := r.providers.Get(name)
		if !ok {
			return
		}
		if _, ok := seen[provider.Name()]; ok {
			return
		}
		seen[provider.Name()] = struct{}{}
		candidates = append(candidates, provider)
	}

	appendProvider(r.fallbackProvider)
	appendProvider(r.defaultProvider)
	for _, provider := range r.providers.All() {
		appendProvider(provider.Name())
	}

	return candidates
}
