package gateway

import (
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestBuildRouteDecisionReport(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC)
	report := buildRouteDecisionReport([]types.TraceSpan{
		{
			Name: "gateway.router",
			Events: []types.TraceEvent{
				{
					Name:      "router.candidate.considered",
					Timestamp: now,
					Attributes: map[string]any{
						"gen_ai.provider.name":  "ollama",
						"gen_ai.request.model":  "llama3.1:8b",
						"hecate.provider.kind":  "local",
						"hecate.route.reason":   "default_model_local_first",
						"hecate.provider.index": 0,
					},
				},
				{
					Name:      "router.candidate.denied",
					Timestamp: now.Add(time.Millisecond),
					Attributes: map[string]any{
						"gen_ai.provider.name":         "ollama",
						"gen_ai.request.model":         "llama3.1:8b",
						"hecate.provider.kind":         "local",
						"hecate.route.reason":          "default_model_local_first",
						"hecate.provider.index":        0,
						"hecate.route.skip_reason":     "route_denied",
						"error.message":                "budget denied",
						"hecate.cost.estimated_micros": int64(1200),
					},
				},
				{
					Name:      "router.candidate.selected",
					Timestamp: now.Add(2 * time.Millisecond),
					Attributes: map[string]any{
						"gen_ai.provider.name":         "openai",
						"gen_ai.request.model":         "gpt-4o-mini",
						"hecate.provider.kind":         "cloud",
						"hecate.route.reason":          "default_model_fallback",
						"hecate.provider.index":        1,
						"hecate.cost.estimated_micros": int64(2400),
					},
				},
				{
					Name:      "provider.failover.selected",
					Timestamp: now.Add(3 * time.Millisecond),
					Attributes: map[string]any{
						"hecate.failover.from_provider": "ollama",
					},
				},
			},
		},
	})

	if report.FinalProvider != "openai" {
		t.Fatalf("FinalProvider = %q, want openai", report.FinalProvider)
	}
	if report.FallbackFrom != "ollama" {
		t.Fatalf("FallbackFrom = %q, want ollama", report.FallbackFrom)
	}
	if len(report.Candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(report.Candidates))
	}
	if report.Candidates[0].Outcome != "denied" {
		t.Fatalf("candidate[0].Outcome = %q, want denied", report.Candidates[0].Outcome)
	}
	if report.Candidates[0].SkipReason != "route_denied" {
		t.Fatalf("candidate[0].SkipReason = %q, want route_denied", report.Candidates[0].SkipReason)
	}
	if report.Candidates[1].Outcome != "selected" {
		t.Fatalf("candidate[1].Outcome = %q, want selected", report.Candidates[1].Outcome)
	}
}
