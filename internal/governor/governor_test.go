package governor

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
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

	if err := gov.RecordUsage(context.Background(), types.ChatRequest{}, types.RouteDecision{Provider: "openai"}, types.Usage{TotalTokens: 10}, 60); err != nil {
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
	if err := gov.RecordUsage(context.Background(), req, decision, types.Usage{PromptTokens: 8, CompletionTokens: 2, TotalTokens: 10}, 60); err != nil {
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
	if status.BalanceMicrosUSD != 40 {
		t.Fatalf("balance_micros_usd = %d, want 40", status.BalanceMicrosUSD)
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

	if err := gov.RecordUsage(context.Background(), types.ChatRequest{}, types.RouteDecision{Provider: "openai"}, types.Usage{TotalTokens: 20}, 250); err != nil {
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
	if status.CreditedMicrosUSD != 200 {
		t.Fatalf("credited_micros_usd = %d, want 200", status.CreditedMicrosUSD)
	}
	if status.BalanceSource != "store" {
		t.Fatalf("balance_source = %q, want store", status.BalanceSource)
	}
	if len(status.History) != 2 {
		t.Fatalf("history length = %d, want 2", len(status.History))
	}
	if status.History[0].Type != "debit" {
		t.Fatalf("latest history type = %q, want debit", status.History[0].Type)
	}
	if status.History[1].Type != "top_up" {
		t.Fatalf("older history type = %q, want top_up", status.History[1].Type)
	}
}

func TestStaticGovernorBudgetWarningsAndHistory(t *testing.T) {
	t.Parallel()

	store := NewMemoryBudgetStore()
	gov := NewStaticGovernor(config.GovernorConfig{
		MaxTotalBudgetMicros:    1_000,
		BudgetKey:               "global",
		BudgetWarningThresholds: []int{50, 90},
		BudgetHistoryLimit:      5,
	}, store, store)

	req := types.ChatRequest{
		RequestID: "req_123",
		Scope: types.RequestScope{
			Tenant: "team-a",
		},
	}
	decision := types.RouteDecision{Provider: "openai", Model: "gpt-4o-mini"}

	if err := gov.TopUpBudget(context.Background(), BudgetFilter{Scope: "global"}, 2_000); err != nil {
		t.Fatalf("TopUpBudget() error = %v", err)
	}
	if err := gov.RecordUsage(context.Background(), req, decision, types.Usage{PromptTokens: 100, CompletionTokens: 25, TotalTokens: 125}, 1_850); err != nil {
		t.Fatalf("RecordUsage() error = %v", err)
	}

	status, err := gov.BudgetStatus(context.Background(), BudgetFilter{Scope: "global"})
	if err != nil {
		t.Fatalf("BudgetStatus() error = %v", err)
	}
	if len(status.Warnings) != 2 {
		t.Fatalf("warnings length = %d, want 2", len(status.Warnings))
	}
	if !status.Warnings[0].Triggered || !status.Warnings[1].Triggered {
		t.Fatalf("warnings = %#v, want both thresholds triggered", status.Warnings)
	}
	if len(status.History) != 2 {
		t.Fatalf("history length = %d, want 2", len(status.History))
	}
	if status.History[0].Type != "debit" {
		t.Fatalf("latest history type = %q, want debit", status.History[0].Type)
	}
	if status.History[0].RequestID != "req_123" {
		t.Fatalf("history request_id = %q, want req_123", status.History[0].RequestID)
	}
	if status.History[0].TotalTokens != 125 {
		t.Fatalf("history total_tokens = %d, want 125", status.History[0].TotalTokens)
	}
	if status.History[0].Timestamp.IsZero() {
		t.Fatal("history timestamp is zero")
	}
	if time.Since(status.History[0].Timestamp) > time.Minute {
		t.Fatalf("history timestamp = %v, looks stale", status.History[0].Timestamp)
	}
}

func TestStaticGovernorRequestPolicyRewrite(t *testing.T) {
	t.Parallel()

	gov := NewStaticGovernor(config.GovernorConfig{
		PolicyRules: []config.PolicyRuleConfig{
			{
				ID:             "tenant-default-downgrade",
				Action:         "rewrite_model",
				Tenants:        []string{"team-a"},
				Models:         []string{"gpt-4o"},
				RewriteModelTo: "gpt-4o-mini",
			},
		},
	}, NewMemoryBudgetStore(), NewMemoryBudgetStore())

	rewritten := gov.Rewrite(types.ChatRequest{
		Model: "gpt-4o",
		Scope: types.RequestScope{
			Tenant: "team-a",
		},
	})
	if rewritten.Model != "gpt-4o-mini" {
		t.Fatalf("rewritten model = %q, want gpt-4o-mini", rewritten.Model)
	}
}

func TestStaticGovernorRoutePolicyDenyByTenantAndProviderKind(t *testing.T) {
	t.Parallel()

	gov := NewStaticGovernor(config.GovernorConfig{
		PolicyRules: []config.PolicyRuleConfig{
			{
				ID:                     "team-a-cloud-spillover-cap",
				Action:                 "deny",
				Reason:                 "team-a cannot use expensive cloud spillover",
				Tenants:                []string{"team-a"},
				ProviderKinds:          []string{"cloud"},
				MinEstimatedCostMicros: 100,
			},
		},
	}, NewMemoryBudgetStore(), NewMemoryBudgetStore())

	err := gov.CheckRoute(context.Background(), types.ChatRequest{
		Scope: types.RequestScope{
			Tenant: "team-a",
			Principal: types.PrincipalContext{
				Role: "tenant",
			},
		},
	}, types.RouteDecision{
		Provider: "openai",
		Model:    "gpt-4o-mini",
		Reason:   "fallback",
	}, "cloud", 250)
	if err == nil {
		t.Fatal("CheckRoute() error = nil, want policy denial")
	}
	if err.Error() != "team-a cannot use expensive cloud spillover" {
		t.Fatalf("error = %q, want policy reason", err.Error())
	}
}

func TestControlPlaneGovernorUsesPersistedPolicyRule(t *testing.T) {
	t.Parallel()

	store, err := controlplane.NewFileStore(t.TempDir() + "/control-plane.json")
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	if _, err := store.UpsertPolicyRule(context.Background(), config.PolicyRuleConfig{
		ID:            "deny-cloud",
		Action:        "deny",
		Reason:        "cloud denied from control plane",
		ProviderKinds: []string{"cloud"},
	}); err != nil {
		t.Fatalf("UpsertPolicyRule() error = %v", err)
	}

	gov := NewControlPlaneGovernor(config.GovernorConfig{}, NewMemoryBudgetStore(), NewMemoryBudgetStore(), store)
	err = gov.CheckRoute(context.Background(), types.ChatRequest{}, types.RouteDecision{
		Provider: "openai",
		Model:    "gpt-4o-mini",
	}, "cloud", 0)
	if err == nil {
		t.Fatal("CheckRoute() error = nil, want persisted policy denial")
	}
	if err.Error() != "cloud denied from control plane" {
		t.Fatalf("error = %q, want persisted policy reason", err.Error())
	}
}
