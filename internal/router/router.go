package router

import (
	"context"
	"fmt"

	"github.com/hecate/agent-runtime/internal/catalog"
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
	catalog          catalog.Catalog
}

type routeCandidate struct {
	Provider providers.Provider
	Name     string
	Kind     providers.Kind
	Model    string
	Reason   string
}

func NewRuleRouter(defaultProvider, defaultModel, strategy, fallbackProvider string, catalog catalog.Catalog) *RuleRouter {
	return &RuleRouter{
		defaultModel:     defaultModel,
		defaultProvider:  defaultProvider,
		fallbackProvider: fallbackProvider,
		strategy:         strategy,
		catalog:          catalog,
	}
}

func (r *RuleRouter) Route(ctx context.Context, req types.ChatRequest) (types.RouteDecision, error) {
	scope := requestscope.Normalize(req.Scope)
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
		Provider: candidate.Name,
		Model:    candidate.Model,
		Reason:   candidate.Reason,
	}, nil
}

func (r *RuleRouter) Fallbacks(ctx context.Context, req types.ChatRequest, current types.RouteDecision) []types.RouteDecision {
	if requestscope.Normalize(req.Scope).ProviderHint != "" {
		return nil
	}

	explicitModel := req.Model != ""
	seen := map[string]struct{}{
		current.Provider + "/" + current.Model: {},
	}
	ordered := r.orderedFallbackProviders()
	out := make([]types.RouteDecision, 0, len(ordered))

	for _, provider := range ordered {
		if provider.Name == current.Provider {
			continue
		}
		if !provider.Healthy {
			continue
		}

		model := ""
		if explicitModel {
			if !supportsModel(provider, req.Model) {
				continue
			}
			model = req.Model
		} else {
			model = provider.DefaultModel
			if model == "" {
				continue
			}
		}

		key := provider.Name + "/" + model
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, types.RouteDecision{
			Provider: provider.Name,
			Model:    model,
			Reason:   current.Reason + "_failover",
		})
	}

	return out
}

func (r *RuleRouter) routeExplicitProvider(ctx context.Context, req types.ChatRequest, explicitProvider, model string) (types.RouteDecision, error) {
	entry, ok := r.catalog.Get(ctx, explicitProvider)
	if !ok {
		return types.RouteDecision{}, fmt.Errorf("provider %q not found", explicitProvider)
	}

	routedModel := model
	reason := "explicit_provider"
	if req.Model != "" {
		reason = "explicit_provider_model"
		if !supportsModel(entry, model) {
			return types.RouteDecision{}, fmt.Errorf("provider %q does not support explicit model %q", explicitProvider, model)
		}
	} else {
		routedModel = entry.DefaultModel
		if routedModel == "" {
			return types.RouteDecision{}, fmt.Errorf("provider %q has no default model for routing", explicitProvider)
		}
	}

	return types.RouteDecision{
		Provider: entry.Name,
		Model:    routedModel,
		Reason:   reason,
	}, nil
}

func (r *RuleRouter) explicitModelCandidates(ctx context.Context, model string) []routeCandidate {
	entries := r.catalog.Snapshot(ctx)
	candidates := make([]routeCandidate, 0, len(entries)+2)

	if r.strategy == "local_first" {
		for _, entry := range entries {
			if entry.Kind != providers.KindLocal {
				continue
			}
			if !entry.Healthy || !supportsModel(entry, model) {
				continue
			}
			candidates = append(candidates, routeCandidate{
				Provider: entry.Provider,
				Name:     entry.Name,
				Kind:     entry.Kind,
				Model:    model,
				Reason:   "explicit_model_local_first",
			})
		}
		if entry, ok := r.namedSupportingProvider(ctx, r.fallbackProvider, model); ok {
			candidates = append(candidates, routeCandidate{
				Provider: entry.Provider,
				Name:     entry.Name,
				Kind:     entry.Kind,
				Model:    model,
				Reason:   "explicit_model_fallback",
			})
		}
		for _, entry := range entries {
			if entry.Kind != providers.KindCloud {
				continue
			}
			if !entry.Healthy || !supportsModel(entry, model) {
				continue
			}
			candidates = append(candidates, routeCandidate{
				Provider: entry.Provider,
				Name:     entry.Name,
				Kind:     entry.Kind,
				Model:    model,
				Reason:   "explicit_model_fallback",
			})
		}
		return dedupeCandidates(candidates)
	}

	if entry, ok := r.namedSupportingProvider(ctx, r.defaultProvider, model); ok {
		candidates = append(candidates, routeCandidate{
			Provider: entry.Provider,
			Name:     entry.Name,
			Kind:     entry.Kind,
			Model:    model,
			Reason:   "explicit_model",
		})
	}
	for _, entry := range entries {
		if !entry.Healthy || !supportsModel(entry, model) {
			continue
		}
		candidates = append(candidates, routeCandidate{
			Provider: entry.Provider,
			Name:     entry.Name,
			Kind:     entry.Kind,
			Model:    model,
			Reason:   "explicit_model",
		})
	}
	return dedupeCandidates(candidates)
}

func (r *RuleRouter) defaultCandidates(ctx context.Context, model string) []routeCandidate {
	entries := r.catalog.Snapshot(ctx)
	candidates := make([]routeCandidate, 0, len(entries)+2)

	if r.strategy == "local_first" {
		skippedUnhealthyLocal := false
		for _, entry := range entries {
			if entry.Kind != providers.KindLocal {
				continue
			}
			if !entry.Healthy {
				skippedUnhealthyLocal = true
				continue
			}

			if localModel := entry.DefaultModel; localModel != "" {
				candidates = append(candidates, routeCandidate{
					Provider: entry.Provider,
					Name:     entry.Name,
					Kind:     entry.Kind,
					Model:    localModel,
					Reason:   "default_model_local_first",
				})
				continue
			}
			if supportsModel(entry, model) {
				candidates = append(candidates, routeCandidate{
					Provider: entry.Provider,
					Name:     entry.Name,
					Kind:     entry.Kind,
					Model:    model,
					Reason:   "default_model_local_first",
				})
			}
		}
		if entry, ok := r.namedProvider(ctx, r.fallbackProvider); ok {
			reason := "default_model_fallback"
			if skippedUnhealthyLocal {
				reason = "default_model_fallback_unhealthy_local"
			}
			routedModel := entry.DefaultModel
			if routedModel == "" {
				routedModel = model
			}
			candidates = append(candidates, routeCandidate{
				Provider: entry.Provider,
				Name:     entry.Name,
				Kind:     entry.Kind,
				Model:    routedModel,
				Reason:   reason,
			})
		}
	}

	skippedDegraded := false
	if entry, ok := r.catalog.Get(ctx, r.defaultProvider); ok {
		if !entry.Healthy {
			skippedDegraded = true
		} else {
			routedModel := entry.DefaultModel
			if routedModel == "" {
				routedModel = model
			}
			candidates = append(candidates, routeCandidate{
				Provider: entry.Provider,
				Name:     entry.Name,
				Kind:     entry.Kind,
				Model:    routedModel,
				Reason:   "default_model",
			})
		}
	}
	for _, entry := range entries {
		if !entry.Healthy {
			skippedDegraded = true
			continue
		}
		routedModel := entry.DefaultModel
		if routedModel == "" {
			continue
		}
		reason := "default_model"
		if skippedDegraded {
			reason = "default_model_fallback_degraded_provider"
		}
		candidates = append(candidates, routeCandidate{
			Provider: entry.Provider,
			Name:     entry.Name,
			Kind:     entry.Kind,
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
		key := candidate.Name + "/" + candidate.Model
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

func (r *RuleRouter) namedProvider(ctx context.Context, name string) (catalog.Entry, bool) {
	if name == "" {
		return catalog.Entry{}, false
	}
	entry, ok := r.catalog.Get(ctx, name)
	if !ok || !entry.Healthy {
		return catalog.Entry{}, false
	}
	return entry, true
}

func (r *RuleRouter) namedSupportingProvider(ctx context.Context, name, model string) (catalog.Entry, bool) {
	entry, ok := r.namedProvider(ctx, name)
	if !ok || !supportsModel(entry, model) {
		return catalog.Entry{}, false
	}
	return entry, true
}

func supportsModel(entry catalog.Entry, model string) bool {
	for _, candidate := range entry.Models {
		if candidate == model {
			return true
		}
	}
	return entry.Provider != nil && entry.Provider.Supports(model)
}

func (r *RuleRouter) orderedFallbackProviders() []catalog.Entry {
	entries := r.catalog.Snapshot(context.Background())
	candidates := make([]catalog.Entry, 0, len(entries)+2)
	seen := make(map[string]struct{}, len(entries)+2)
	appendProvider := func(name string) {
		if name == "" {
			return
		}
		entry, ok := r.catalog.Get(context.Background(), name)
		if !ok {
			return
		}
		if _, ok := seen[entry.Name]; ok {
			return
		}
		seen[entry.Name] = struct{}{}
		candidates = append(candidates, entry)
	}

	appendProvider(r.fallbackProvider)
	appendProvider(r.defaultProvider)
	for _, entry := range entries {
		appendProvider(entry.Name)
	}

	return candidates
}
