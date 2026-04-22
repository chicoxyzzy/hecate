package policy

import (
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestEvaluateDenyMatchesTenantAndRouteCost(t *testing.T) {
	t.Parallel()

	rules := FromConfig([]config.PolicyRuleConfig{
		{
			ID:                     "tenant-cloud-cost-cap",
			Action:                 ActionDeny,
			Reason:                 "team-a cannot spill to expensive cloud routes",
			Tenants:                []string{"team-a"},
			ProviderKinds:          []string{"cloud"},
			MinEstimatedCostMicros: 100,
		},
	})

	subject := BuildRouteSubject(types.ChatRequest{
		Model: "gpt-4o-mini",
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

	err := EvaluateDeny(rules, subject)
	if err == nil {
		t.Fatal("EvaluateDeny() error = nil, want match")
	}
	if err.Evaluation.RuleID != "tenant-cloud-cost-cap" {
		t.Fatalf("rule_id = %q, want tenant-cloud-cost-cap", err.Evaluation.RuleID)
	}
}

func TestEvaluateRewriteRewritesModelForTenant(t *testing.T) {
	t.Parallel()

	rules := FromConfig([]config.PolicyRuleConfig{
		{
			ID:             "free-tier-local-default",
			Action:         ActionRewriteModel,
			Tenants:        []string{"team-a"},
			Models:         []string{"gpt-4o"},
			RewriteModelTo: "gpt-4o-mini",
		},
	})

	eval, rewritten, ok := EvaluateRewrite(rules, BuildRequestSubject(types.ChatRequest{
		Model: "gpt-4o",
		Scope: types.RequestScope{
			Tenant: "team-a",
		},
	}))
	if !ok {
		t.Fatal("EvaluateRewrite() ok = false, want true")
	}
	if eval.RuleID != "free-tier-local-default" {
		t.Fatalf("rule_id = %q, want free-tier-local-default", eval.RuleID)
	}
	if rewritten != "gpt-4o-mini" {
		t.Fatalf("rewritten model = %q, want gpt-4o-mini", rewritten)
	}
}
