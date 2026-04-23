package governor

import (
	"context"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/pkg/types"
)

type ControlPlaneGovernor struct {
	config  config.GovernorConfig
	store   AccountStore
	history BudgetHistoryStore
	cpStore controlplane.Store
}

func NewControlPlaneGovernor(cfg config.GovernorConfig, store AccountStore, historyStore BudgetHistoryStore, cpStore controlplane.Store) *ControlPlaneGovernor {
	var history BudgetHistoryStore
	if historyStore != nil {
		history = historyStore
	} else if candidate, ok := store.(BudgetHistoryStore); ok {
		history = candidate
	}
	return &ControlPlaneGovernor{
		config:  cfg,
		store:   store,
		history: history,
		cpStore: cpStore,
	}
}

func (g *ControlPlaneGovernor) Check(ctx context.Context, req types.ChatRequest) error {
	return g.current(ctx).Check(ctx, req)
}

func (g *ControlPlaneGovernor) CheckRoute(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, providerKind string, estimatedCostMicros int64) error {
	return g.current(ctx).CheckRoute(ctx, req, decision, providerKind, estimatedCostMicros)
}

func (g *ControlPlaneGovernor) RecordUsage(ctx context.Context, req types.ChatRequest, decision types.RouteDecision, usage types.Usage, costMicros int64) error {
	return g.current(ctx).RecordUsage(ctx, req, decision, usage, costMicros)
}

func (g *ControlPlaneGovernor) BudgetStatus(ctx context.Context, filter BudgetFilter) (types.BudgetStatus, error) {
	return g.current(ctx).BudgetStatus(ctx, filter)
}

func (g *ControlPlaneGovernor) RecentBudgetHistory(ctx context.Context, limit int) ([]types.BudgetHistoryEntry, error) {
	return g.current(ctx).RecentBudgetHistory(ctx, limit)
}

func (g *ControlPlaneGovernor) TopUpBudget(ctx context.Context, filter BudgetFilter, deltaMicros int64) error {
	return g.current(ctx).TopUpBudget(ctx, filter, deltaMicros)
}

func (g *ControlPlaneGovernor) SetBudgetBalance(ctx context.Context, filter BudgetFilter, balanceMicros int64) error {
	return g.current(ctx).SetBudgetBalance(ctx, filter, balanceMicros)
}

func (g *ControlPlaneGovernor) ResetBudget(ctx context.Context, filter BudgetFilter) error {
	return g.current(ctx).ResetBudget(ctx, filter)
}

func (g *ControlPlaneGovernor) Rewrite(req types.ChatRequest) types.ChatRequest {
	return g.current(context.Background()).Rewrite(req)
}

func (g *ControlPlaneGovernor) current(ctx context.Context) *StaticGovernor {
	cfg := g.config
	if g.cpStore != nil {
		if state, err := g.cpStore.Snapshot(ctx); err == nil && len(state.PolicyRules) > 0 {
			cfg.PolicyRules = append(append([]config.PolicyRuleConfig(nil), cfg.PolicyRules...), state.PolicyRules...)
		}
	}
	return NewStaticGovernor(cfg, g.store, g.history)
}
