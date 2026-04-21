package router

import (
	"context"
	"fmt"

	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/requestscope"
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

type routeCandidate struct {
	Provider providers.Provider
	Model    string
	Reason   string
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
	scope := requestscope.FromChatRequest(req)
	model := req.Model
	if model == "" {
		model = r.defaultModel
	}
	if model == "" {
		return types.RouteDecision{}, fmt.Errorf("no model available for routing")
	}

	if scope.ProviderHint != "" {
		return r.routeExplicitProvider(ctx, req, scope.ProviderHint, model)
	}

	var (
		candidate routeCandidate
		ok        bool
	)
	if req.Model != "" {
		candidate, ok = r.selectCandidate(r.explicitModelCandidates(ctx, model))
		if !ok {
			return types.RouteDecision{}, fmt.Errorf("no provider supports explicit model %q", model)
		}
	} else {
		candidate, ok = r.selectCandidate(r.defaultCandidates(ctx, model))
		if !ok {
			return types.RouteDecision{}, fmt.Errorf("no provider available for default routing")
		}
	}

	return types.RouteDecision{
		Provider: candidate.Provider.Name(),
		Model:    candidate.Model,
		Reason:   candidate.Reason,
	}, nil
}

func (r *RuleRouter) Fallbacks(ctx context.Context, req types.ChatRequest, current types.RouteDecision) []types.RouteDecision {
	if requestscope.FromChatRequest(req).ProviderHint != "" {
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

func (r *RuleRouter) routeExplicitProvider(ctx context.Context, req types.ChatRequest, explicitProvider, model string) (types.RouteDecision, error) {
	provider, ok := r.providers.Get(explicitProvider)
	if !ok {
		return types.RouteDecision{}, fmt.Errorf("provider %q not found", explicitProvider)
	}

	routedModel := model
	reason := "explicit_provider"
	if req.Model != "" {
		reason = "explicit_provider_model"
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
		Reason:   reason,
	}, nil
}

func (r *RuleRouter) explicitModelCandidates(ctx context.Context, model string) []routeCandidate {
	candidates := make([]routeCandidate, 0, len(r.providers.All())+2)

	if r.strategy == "local_first" {
		for _, provider := range r.providers.All() {
			if provider.Kind() != providers.KindLocal {
				continue
			}
			if !r.providerHealthyForAutoRouting(ctx, provider) || !r.providerSupportsModel(ctx, provider, model) {
				continue
			}
			candidates = append(candidates, routeCandidate{
				Provider: provider,
				Model:    model,
				Reason:   "explicit_model_local_first",
			})
		}
		if provider, ok := r.namedSupportingProvider(ctx, r.fallbackProvider, model); ok {
			candidates = append(candidates, routeCandidate{
				Provider: provider,
				Model:    model,
				Reason:   "explicit_model_fallback",
			})
		}
		for _, provider := range r.providers.All() {
			if provider.Kind() != providers.KindCloud {
				continue
			}
			if !r.providerHealthyForAutoRouting(ctx, provider) || !r.providerSupportsModel(ctx, provider, model) {
				continue
			}
			candidates = append(candidates, routeCandidate{
				Provider: provider,
				Model:    model,
				Reason:   "explicit_model_fallback",
			})
		}
		return dedupeCandidates(candidates)
	}

	if provider, ok := r.namedSupportingProvider(ctx, r.defaultProvider, model); ok {
		candidates = append(candidates, routeCandidate{
			Provider: provider,
			Model:    model,
			Reason:   "explicit_model",
		})
	}
	for _, provider := range r.providers.All() {
		if !r.providerHealthyForAutoRouting(ctx, provider) || !r.providerSupportsModel(ctx, provider, model) {
			continue
		}
		candidates = append(candidates, routeCandidate{
			Provider: provider,
			Model:    model,
			Reason:   "explicit_model",
		})
	}
	return dedupeCandidates(candidates)
}

func (r *RuleRouter) defaultCandidates(ctx context.Context, model string) []routeCandidate {
	candidates := make([]routeCandidate, 0, len(r.providers.All())+2)

	if r.strategy == "local_first" {
		skippedUnhealthyLocal := false
		for _, provider := range r.providers.All() {
			if provider.Kind() != providers.KindLocal {
				continue
			}
			if !r.providerHealthyForAutoRouting(ctx, provider) {
				skippedUnhealthyLocal = true
				continue
			}

			if localModel := r.providerDefaultModel(ctx, provider); localModel != "" {
				candidates = append(candidates, routeCandidate{
					Provider: provider,
					Model:    localModel,
					Reason:   "default_model_local_first",
				})
				continue
			}
			if r.providerSupportsModel(ctx, provider, model) {
				candidates = append(candidates, routeCandidate{
					Provider: provider,
					Model:    model,
					Reason:   "default_model_local_first",
				})
			}
		}
		if provider, ok := r.namedProvider(ctx, r.fallbackProvider); ok {
			reason := "default_model_fallback"
			if skippedUnhealthyLocal {
				reason = "default_model_fallback_unhealthy_local"
			}
			routedModel := r.providerDefaultModel(ctx, provider)
			if routedModel == "" {
				routedModel = model
			}
			candidates = append(candidates, routeCandidate{
				Provider: provider,
				Model:    routedModel,
				Reason:   reason,
			})
		}
	}

	skippedDegraded := false
	if provider, ok := r.namedProvider(ctx, r.defaultProvider); ok {
		if !r.providerHealthyForAutoRouting(ctx, provider) {
			skippedDegraded = true
		} else {
			routedModel := r.providerDefaultModel(ctx, provider)
			if routedModel == "" {
				routedModel = model
			}
			candidates = append(candidates, routeCandidate{
				Provider: provider,
				Model:    routedModel,
				Reason:   "default_model",
			})
		}
	}
	for _, provider := range r.providers.All() {
		if !r.providerHealthyForAutoRouting(ctx, provider) {
			skippedDegraded = true
			continue
		}
		routedModel := r.providerDefaultModel(ctx, provider)
		if routedModel == "" {
			continue
		}
		reason := "default_model"
		if skippedDegraded {
			reason = "default_model_fallback_degraded_provider"
		}
		candidates = append(candidates, routeCandidate{
			Provider: provider,
			Model:    routedModel,
			Reason:   reason,
		})
	}

	return dedupeCandidates(candidates)
}

func (r *RuleRouter) selectCandidate(candidates []routeCandidate) (routeCandidate, bool) {
	if len(candidates) == 0 {
		return routeCandidate{}, false
	}
	return candidates[0], true
}

func dedupeCandidates(candidates []routeCandidate) []routeCandidate {
	seen := make(map[string]struct{}, len(candidates))
	out := make([]routeCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider == nil || candidate.Model == "" {
			continue
		}
		key := candidate.Provider.Name() + "/" + candidate.Model
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func (r *RuleRouter) namedProvider(ctx context.Context, name string) (providers.Provider, bool) {
	if name == "" {
		return nil, false
	}
	provider, ok := r.providers.Get(name)
	if !ok || !r.providerHealthyForAutoRouting(ctx, provider) {
		return nil, false
	}
	return provider, true
}

func (r *RuleRouter) namedSupportingProvider(ctx context.Context, name, model string) (providers.Provider, bool) {
	provider, ok := r.namedProvider(ctx, name)
	if !ok || !r.providerSupportsModel(ctx, provider, model) {
		return nil, false
	}
	return provider, true
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
