package governor

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/policy"
	"github.com/hecate/agent-runtime/internal/requestscope"
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
	config  config.GovernorConfig
	ledger  UsageLedger
	store   BudgetStateStore
	history BudgetHistoryStore
	rules   []policy.Rule
}

func NewStaticGovernor(cfg config.GovernorConfig, ledger UsageLedger, store BudgetStateStore) *StaticGovernor {
	var history BudgetHistoryStore
	if candidate, ok := ledger.(BudgetHistoryStore); ok {
		history = candidate
	} else if candidate, ok := store.(BudgetHistoryStore); ok {
		history = candidate
	}
	return &StaticGovernor{
		config:  cfg,
		ledger:  ledger,
		store:   store,
		history: history,
		rules:   policy.FromConfig(cfg.PolicyRules),
	}
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

	if err := policy.EvaluateDeny(g.rules, policy.BuildRequestSubject(req)); err != nil {
		return err
	}

	return nil
}

func (g *StaticGovernor) CheckRoute(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, providerKind string, estimatedCostMicros int64) error {
	scope := requestscope.Normalize(req.Scope)
	if allowedProviders := scope.AllowedProviders; len(allowedProviders) > 0 && !slices.Contains(allowedProviders, decision.Provider) {
		return fmt.Errorf("provider %q is not allowed for this api key", decision.Provider)
	}
	if allowedModels := scope.AllowedModels; len(allowedModels) > 0 && !slices.Contains(allowedModels, decision.Model) {
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

	if err := policy.EvaluateDeny(g.rules, policy.BuildRouteSubject(req, decision, providerKind, estimatedCostMicros)); err != nil {
		return err
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

		current, err := g.ledger.Current(ctx, budgetKey)
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
	if g.ledger == nil || costMicros <= 0 {
		return nil
	}
	event := UsageEvent{
		BudgetKey:  g.budgetKeyForRequest(req, decision),
		RequestID:  req.RequestID,
		Tenant:     requestscope.EffectiveTenant(requestscope.Normalize(req.Scope), g.config.BudgetTenantFallback),
		Provider:   decision.Provider,
		Model:      decision.Model,
		CostMicros: costMicros,
		OccurredAt: time.Now().UTC(),
	}
	if err := g.ledger.Record(ctx, event); err != nil {
		return fmt.Errorf("record budget usage for provider %q: %w", decision.Provider, err)
	}
	if err := g.appendBudgetEvent(ctx, BudgetEvent{
		Key:             event.BudgetKey,
		Type:            "usage",
		Scope:           g.config.BudgetScope,
		Provider:        decision.Provider,
		Tenant:          event.Tenant,
		Model:           decision.Model,
		RequestID:       req.RequestID,
		AmountMicrosUSD: costMicros,
		OccurredAt:      event.OccurredAt,
	}); err != nil {
		return fmt.Errorf("append budget usage history: %w", err)
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

	spent, err := g.ledger.Current(ctx, resolved.Key)
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
	status.Warnings = g.buildWarnings(limit, spent)
	history, err := g.budgetHistory(ctx, resolved.Key, status, g.historyLimit())
	if err != nil {
		return types.BudgetStatus{}, fmt.Errorf("read budget history: %w", err)
	}
	status.History = history
	return status, nil
}

func (g *StaticGovernor) TopUpBudget(ctx context.Context, filter BudgetFilter, deltaMicros int64) error {
	if g.store == nil || deltaMicros <= 0 {
		return nil
	}
	resolved := g.resolveBudgetFilter(filter)
	if err := g.store.AddLimit(ctx, resolved.Key, deltaMicros); err != nil {
		return fmt.Errorf("top up budget limit: %w", err)
	}
	current, limit, err := g.currentAndLimit(ctx, resolved.Key)
	if err != nil {
		return fmt.Errorf("read budget after top up: %w", err)
	}
	if err := g.appendBudgetEvent(ctx, BudgetEvent{
		Key:              resolved.Key,
		Type:             "top_up",
		Scope:            resolved.Scope,
		Provider:         resolved.Provider,
		Tenant:           resolved.Tenant,
		AmountMicrosUSD:  deltaMicros,
		BalanceMicrosUSD: current,
		LimitMicrosUSD:   limit,
		OccurredAt:       time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append top-up history: %w", err)
	}
	return nil
}

func (g *StaticGovernor) SetBudgetLimit(ctx context.Context, filter BudgetFilter, limitMicros int64) error {
	if g.store == nil || limitMicros < 0 {
		return nil
	}
	resolved := g.resolveBudgetFilter(filter)
	if err := g.store.SetLimit(ctx, resolved.Key, limitMicros); err != nil {
		return fmt.Errorf("set budget limit: %w", err)
	}
	current, _, err := g.currentAndLimit(ctx, resolved.Key)
	if err != nil {
		return fmt.Errorf("read budget after limit set: %w", err)
	}
	if err := g.appendBudgetEvent(ctx, BudgetEvent{
		Key:              resolved.Key,
		Type:             "set_limit",
		Scope:            resolved.Scope,
		Provider:         resolved.Provider,
		Tenant:           resolved.Tenant,
		AmountMicrosUSD:  limitMicros,
		BalanceMicrosUSD: current,
		LimitMicrosUSD:   limitMicros,
		OccurredAt:       time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append limit history: %w", err)
	}
	return nil
}

func (g *StaticGovernor) ResetBudget(ctx context.Context, filter BudgetFilter) error {
	if g.ledger == nil {
		return nil
	}
	resolved := g.resolveBudgetFilter(filter)
	if err := g.ledger.Reset(ctx, resolved.Key); err != nil {
		return fmt.Errorf("reset budget state: %w", err)
	}
	_, limit, err := g.currentAndLimit(ctx, resolved.Key)
	if err != nil {
		return fmt.Errorf("read budget after reset: %w", err)
	}
	if err := g.appendBudgetEvent(ctx, BudgetEvent{
		Key:              resolved.Key,
		Type:             "reset",
		Scope:            resolved.Scope,
		Provider:         resolved.Provider,
		Tenant:           resolved.Tenant,
		BalanceMicrosUSD: 0,
		LimitMicrosUSD:   limit,
		OccurredAt:       time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("append reset history: %w", err)
	}
	return nil
}

func (g *StaticGovernor) Rewrite(req types.ChatRequest) types.ChatRequest {
	if _, rewritten, ok := policy.EvaluateRewrite(g.rules, policy.BuildRequestSubject(req)); ok {
		req.Model = rewritten
		return req
	}

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
		Tenant:   requestscope.EffectiveTenant(requestscope.Normalize(req.Scope), g.config.BudgetTenantFallback),
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

func (g *StaticGovernor) appendBudgetEvent(ctx context.Context, event BudgetEvent) error {
	if g.history == nil {
		return nil
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.BalanceMicrosUSD == 0 && g.ledger != nil {
		current, err := g.ledger.Current(ctx, event.Key)
		if err == nil {
			event.BalanceMicrosUSD = current
		}
	}
	if event.LimitMicrosUSD == 0 && g.store != nil {
		limit, err := g.effectiveBudgetLimit(ctx, event.Key)
		if err == nil {
			event.LimitMicrosUSD = limit
		}
	}
	return g.history.AppendEvent(ctx, event)
}

func (g *StaticGovernor) budgetHistory(ctx context.Context, key string, status types.BudgetStatus, limit int) ([]types.BudgetHistoryEntry, error) {
	if g.history == nil {
		return nil, nil
	}

	events, err := g.history.ListEvents(ctx, key, limit)
	if err != nil {
		return nil, err
	}

	out := make([]types.BudgetHistoryEntry, 0, len(events))
	for _, event := range events {
		out = append(out, types.BudgetHistoryEntry{
			Type:             event.Type,
			Scope:            event.Scope,
			Provider:         firstNonEmpty(event.Provider, status.Provider),
			Tenant:           firstNonEmpty(event.Tenant, status.Tenant),
			Model:            event.Model,
			RequestID:        event.RequestID,
			Actor:            event.Actor,
			Detail:           event.Detail,
			AmountMicrosUSD:  event.AmountMicrosUSD,
			BalanceMicrosUSD: event.BalanceMicrosUSD,
			LimitMicrosUSD:   event.LimitMicrosUSD,
			Timestamp:        event.OccurredAt,
		})
	}
	return out, nil
}

func (g *StaticGovernor) buildWarnings(limit, current int64) []types.BudgetWarning {
	if limit <= 0 || current < 0 {
		return nil
	}

	thresholds := g.config.BudgetWarningThresholds
	if len(thresholds) == 0 {
		thresholds = []int{50, 80, 95}
	}

	out := make([]types.BudgetWarning, 0, len(thresholds))
	for _, threshold := range thresholds {
		if threshold <= 0 {
			continue
		}
		thresholdMicros := (limit * int64(threshold)) / 100
		out = append(out, types.BudgetWarning{
			ThresholdPercent:   threshold,
			ThresholdMicrosUSD: thresholdMicros,
			CurrentMicrosUSD:   current,
			RemainingMicrosUSD: remainingBudget(limit, current),
			Triggered:          current >= thresholdMicros,
		})
	}
	return out
}

func (g *StaticGovernor) historyLimit() int {
	if g.config.BudgetHistoryLimit <= 0 {
		return 20
	}
	return g.config.BudgetHistoryLimit
}

func (g *StaticGovernor) currentAndLimit(ctx context.Context, key string) (int64, int64, error) {
	var current int64
	if g.ledger != nil {
		value, err := g.ledger.Current(ctx, key)
		if err != nil {
			return 0, 0, err
		}
		current = value
	}

	limit, err := g.effectiveBudgetLimit(ctx, key)
	if err != nil {
		return 0, 0, err
	}
	return current, limit, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
