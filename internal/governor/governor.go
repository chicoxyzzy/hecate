package governor

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Governor interface {
	Check(ctx context.Context, req types.ChatRequest) error
	CheckRoute(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, providerKind string, estimatedCostMicros int64) error
	RecordUsage(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, costMicros int64) error
	BudgetStatus(ctx context.Context, filter BudgetFilter) (types.BudgetStatus, error)
	TopUpBudget(ctx context.Context, filter BudgetFilter, deltaMicros int64) error
	SetBudgetLimit(ctx context.Context, filter BudgetFilter, limitMicros int64) error
	ResetBudget(ctx context.Context, filter BudgetFilter) error
	Rewrite(req types.ChatRequest) types.ChatRequest
}

type BudgetFilter struct {
	Key      string
	Scope    string
	Provider string
	Tenant   string
}

type StaticGovernor struct {
	config config.GovernorConfig
	store  BudgetStore
}

func NewStaticGovernor(cfg config.GovernorConfig, store BudgetStore) *StaticGovernor {
	return &StaticGovernor{config: cfg, store: store}
}

func (g *StaticGovernor) Check(_ context.Context, req types.ChatRequest) error {
	if g.config.DenyAll {
		return fmt.Errorf("requests are disabled by policy")
	}

	promptEstimate := 0
	for _, msg := range req.Messages {
		promptEstimate += len(msg.Content) / 4
	}
	if promptEstimate > g.config.MaxPromptTokens {
		return fmt.Errorf("estimated prompt tokens %d exceed limit %d", promptEstimate, g.config.MaxPromptTokens)
	}

	return nil
}

func (g *StaticGovernor) CheckRoute(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, providerKind string, estimatedCostMicros int64) error {
	if allowedProviders := metadataCSV(req.Metadata, "auth_allowed_providers"); len(allowedProviders) > 0 && !slices.Contains(allowedProviders, decision.Provider) {
		return fmt.Errorf("provider %q is not allowed for this api key", decision.Provider)
	}
	if allowedModels := metadataCSV(req.Metadata, "auth_allowed_models"); len(allowedModels) > 0 && !slices.Contains(allowedModels, decision.Model) {
		return fmt.Errorf("model %q is not allowed for this api key", decision.Model)
	}

	if len(g.config.AllowedProviders) > 0 && !slices.Contains(g.config.AllowedProviders, decision.Provider) {
		return fmt.Errorf("provider %q is not allowed by policy", decision.Provider)
	}
	if slices.Contains(g.config.DeniedProviders, decision.Provider) {
		return fmt.Errorf("provider %q is denied by policy", decision.Provider)
	}

	model := decision.Model
	if len(g.config.AllowedModels) > 0 && !slices.Contains(g.config.AllowedModels, model) {
		return fmt.Errorf("model %q is not allowed by policy", model)
	}
	if slices.Contains(g.config.DeniedModels, model) {
		return fmt.Errorf("model %q is denied by policy", model)
	}

	if len(g.config.AllowedProviderKinds) > 0 && !slices.Contains(g.config.AllowedProviderKinds, providerKind) {
		return fmt.Errorf("provider kind %q is not allowed by policy", providerKind)
	}
	switch g.config.RouteMode {
	case "local_only":
		if providerKind != "local" {
			return fmt.Errorf("route mode %q denies provider kind %q", g.config.RouteMode, providerKind)
		}
	case "cloud_only":
		if providerKind != "cloud" {
			return fmt.Errorf("route mode %q denies provider kind %q", g.config.RouteMode, providerKind)
		}
	}

	budgetKey := g.budgetKeyForRequest(req, decision)
	if g.store != nil {
		limit, err := g.effectiveBudgetLimit(ctx, budgetKey)
		if err != nil {
			return fmt.Errorf("read budget limit: %w", err)
		}
		if limit <= 0 {
			_ = req
			return nil
		}

		current, err := g.store.Spent(ctx, budgetKey)
		if err != nil {
			return fmt.Errorf("read budget state: %w", err)
		}
		if current+estimatedCostMicros > limit {
			return fmt.Errorf(
				"estimated request budget %d would exceed limit %d (current=%d)",
				estimatedCostMicros,
				limit,
				current,
			)
		}
	}

	_ = req
	return nil
}

func (g *StaticGovernor) RecordUsage(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, costMicros int64) error {
	if g.store == nil || costMicros <= 0 {
		return nil
	}
	if err := g.store.AddSpent(ctx, g.budgetKeyForRequest(req, decision), costMicros); err != nil {
		return fmt.Errorf("record budget usage for provider %q: %w", decision.Provider, err)
	}
	return nil
}

func (g *StaticGovernor) BudgetStatus(ctx context.Context, filter BudgetFilter) (types.BudgetStatus, error) {
	resolved := g.resolveBudgetFilter(filter)
	status := types.BudgetStatus{
		Key:      resolved.Key,
		Scope:    resolved.Scope,
		Provider: resolved.Provider,
		Tenant:   resolved.Tenant,
		Backend:  g.config.BudgetBackend,
		Enforced: true,
	}
	if g.store == nil {
		if status.Backend == "" {
			status.Backend = "none"
		}
		status.MaxMicrosUSD = g.config.MaxTotalBudgetMicros
		status.LimitSource = "config"
		status.Enforced = status.MaxMicrosUSD > 0
		return status, nil
	}

	spent, err := g.store.Spent(ctx, resolved.Key)
	if err != nil {
		return types.BudgetStatus{}, fmt.Errorf("read budget spent: %w", err)
	}
	limit, source, err := g.budgetLimitAndSource(ctx, resolved.Key)
	if err != nil {
		return types.BudgetStatus{}, fmt.Errorf("read budget limit: %w", err)
	}
	status.SpentMicrosUSD = spent
	status.CurrentMicrosUSD = spent
	status.MaxMicrosUSD = limit
	status.LimitSource = source
	status.Enforced = limit > 0
	status.RemainingMicrosUSD = remainingBudget(limit, spent)
	return status, nil
}

func (g *StaticGovernor) TopUpBudget(ctx context.Context, filter BudgetFilter, deltaMicros int64) error {
	if g.store == nil || deltaMicros <= 0 {
		return nil
	}
	if err := g.store.AddLimit(ctx, g.resolveBudgetFilter(filter).Key, deltaMicros); err != nil {
		return fmt.Errorf("top up budget limit: %w", err)
	}
	return nil
}

func (g *StaticGovernor) SetBudgetLimit(ctx context.Context, filter BudgetFilter, limitMicros int64) error {
	if g.store == nil || limitMicros < 0 {
		return nil
	}
	if err := g.store.SetLimit(ctx, g.resolveBudgetFilter(filter).Key, limitMicros); err != nil {
		return fmt.Errorf("set budget limit: %w", err)
	}
	return nil
}

func (g *StaticGovernor) ResetBudget(ctx context.Context, filter BudgetFilter) error {
	if g.store == nil {
		return nil
	}
	if err := g.store.ResetSpent(ctx, g.resolveBudgetFilter(filter).Key); err != nil {
		return fmt.Errorf("reset budget state: %w", err)
	}
	return nil
}

func (g *StaticGovernor) Rewrite(req types.ChatRequest) types.ChatRequest {
	if g.config.ModelRewriteTo == "" {
		return req
	}
	req.Model = g.config.ModelRewriteTo
	return req
}

func remainingBudget(limit, current int64) int64 {
	if limit <= 0 {
		return 0
	}
	remaining := limit - current
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (g *StaticGovernor) budgetKeyForRequest(req types.ChatRequest, decision types.RouteDecision) string {
	return g.resolveBudgetFilter(BudgetFilter{
		Scope:    g.config.BudgetScope,
		Provider: decision.Provider,
		Tenant:   requestTenant(req, g.config.BudgetTenantFallback),
	}).Key
}

func (g *StaticGovernor) resolveBudgetFilter(filter BudgetFilter) BudgetFilter {
	if filter.Key != "" {
		if filter.Scope == "" {
			filter.Scope = "custom"
		}
		return filter
	}

	baseKey := g.config.BudgetKey
	if baseKey == "" {
		baseKey = "global"
	}

	scope := filter.Scope
	if scope == "" {
		scope = g.config.BudgetScope
	}
	if scope == "" {
		scope = "global"
	}

	provider := filter.Provider
	tenant := filter.Tenant
	if tenant == "" {
		tenant = g.config.BudgetTenantFallback
		if tenant == "" {
			tenant = "anonymous"
		}
	}

	switch scope {
	case "provider":
		filter.Key = baseKey + ":provider:" + provider
	case "tenant":
		filter.Key = baseKey + ":tenant:" + tenant
	case "tenant_provider":
		filter.Key = baseKey + ":tenant:" + tenant + ":provider:" + provider
	default:
		scope = "global"
		filter.Key = baseKey
	}

	filter.Scope = scope
	filter.Provider = provider
	filter.Tenant = tenant
	return filter
}

func requestTenant(req types.ChatRequest, fallback string) string {
	if req.Metadata["tenant"] != "" {
		return req.Metadata["tenant"]
	}
	if req.Metadata["user"] != "" {
		return req.Metadata["user"]
	}
	if fallback != "" {
		return fallback
	}
	return "anonymous"
}

func (g *StaticGovernor) effectiveBudgetLimit(ctx context.Context, key string) (int64, error) {
	limit, _, err := g.budgetLimitAndSource(ctx, key)
	return limit, err
}

func (g *StaticGovernor) budgetLimitAndSource(ctx context.Context, key string) (int64, string, error) {
	if g.store == nil {
		return g.config.MaxTotalBudgetMicros, "config", nil
	}
	limit, err := g.store.Limit(ctx, key)
	if err != nil {
		return 0, "", err
	}
	if limit > 0 {
		return limit, "store", nil
	}
	return g.config.MaxTotalBudgetMicros, "config", nil
}

func metadataCSV(metadata map[string]string, key string) []string {
	if metadata == nil {
		return nil
	}
	raw := strings.TrimSpace(metadata[key])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
