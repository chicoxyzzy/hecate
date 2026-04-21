package governor

import (
	"context"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestStaticGovernorCheckRoutePolicy(t *testing.T) {
	t.Parallel()

	store := NewMemoryBudgetStore()
	gov := NewStaticGovernor(config.GovernorConfig{
		RouteMode:        "local_only",
		AllowedProviders: []string{"ollama"},
		DeniedModels:     []string{"gpt-4o-mini"},
	}, store, store)

	err := gov.CheckRoute(context.Background(), types.ChatRequest{}, types.RouteDecision{
		Provider: "openai",
		Model:    "gpt-4o-mini",
	}, "cloud", 0)
	if err == nil {
		t.Fatal("CheckRoute() error = nil, want policy denial")
	}
}

func TestStaticGovernorBudgetTracking(t *testing.T) {
	t.Parallel()

	store := NewMemoryBudgetStore()
	gov := NewStaticGovernor(config.GovernorConfig{
		MaxTotalBudgetMicros: 100,
		BudgetKey:            "global",
	}, store, store)

	if err := gov.RecordUsage(context.Background(), types.ChatRequest{}, types.RouteDecision{Provider: "openai"}, 60); err != nil {
		t.Fatalf("RecordUsage() error = %v", err)
	}

	err := gov.CheckRoute(context.Background(), types.ChatRequest{}, types.RouteDecision{
		Provider: "openai",
		Model:    "gpt-4o-mini",
	}, "cloud", 50)
	if err == nil {
		t.Fatal("CheckRoute() error = nil, want budget denial")
	}
}

func TestStaticGovernorBudgetTrackingByTenantProvider(t *testing.T) {
	t.Parallel()

	store := NewMemoryBudgetStore()
	gov := NewStaticGovernor(config.GovernorConfig{
		MaxTotalBudgetMicros: 100,
		BudgetKey:            "global",
		BudgetScope:          "tenant_provider",
		BudgetTenantFallback: "anonymous",
	}, store, store)

	req := types.ChatRequest{
		Scope: types.RequestScope{
			User: "team-a",
		},
	}
	decision := types.RouteDecision{Provider: "openai", Model: "gpt-4o-mini"}

	if err := gov.CheckRoute(context.Background(), req, decision, "cloud", 60); err != nil {
		t.Fatalf("CheckRoute() unexpected error = %v", err)
	}
	if err := gov.RecordUsage(context.Background(), req, decision, 60); err != nil {
		t.Fatalf("RecordUsage() error = %v", err)
	}

	status, err := gov.BudgetStatus(context.Background(), BudgetFilter{
		Scope:    "tenant_provider",
		Provider: "openai",
		Tenant:   "team-a",
	})
	if err != nil {
		t.Fatalf("BudgetStatus() error = %v", err)
	}
	if status.Key != "global:tenant:team-a:provider:openai" {
		t.Fatalf("status key = %q, want tenant/provider segmented key", status.Key)
	}
	if status.CurrentMicrosUSD != 60 {
		t.Fatalf("current_micros_usd = %d, want 60", status.CurrentMicrosUSD)
	}
}

func TestStaticGovernorBudgetTopUpOverridesConfigLimit(t *testing.T) {
	t.Parallel()

	store := NewMemoryBudgetStore()
	gov := NewStaticGovernor(config.GovernorConfig{
		MaxTotalBudgetMicros: 100,
		BudgetKey:            "global",
	}, store, store)

	if err := gov.TopUpBudget(context.Background(), BudgetFilter{Scope: "global"}, 200); err != nil {
		t.Fatalf("TopUpBudget() error = %v", err)
	}

	if err := gov.RecordUsage(context.Background(), types.ChatRequest{}, types.RouteDecision{Provider: "openai"}, 250); err != nil {
		t.Fatalf("RecordUsage() error = %v", err)
	}

	if err := gov.CheckRoute(context.Background(), types.ChatRequest{}, types.RouteDecision{
		Provider: "openai",
		Model:    "gpt-4o-mini",
	}, "cloud", 40); err == nil {
		t.Fatal("CheckRoute() error = nil, want limit denial after top-up-adjusted budget")
	}

	status, err := gov.BudgetStatus(context.Background(), BudgetFilter{Scope: "global"})
	if err != nil {
		t.Fatalf("BudgetStatus() error = %v", err)
	}
	if status.MaxMicrosUSD != 200 {
		t.Fatalf("max_micros_usd = %d, want 200", status.MaxMicrosUSD)
	}
	if status.LimitSource != "store" {
		t.Fatalf("limit_source = %q, want store", status.LimitSource)
	}
}
