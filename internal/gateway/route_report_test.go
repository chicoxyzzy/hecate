package gateway

import (
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/telemetry"
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
						telemetry.AttrGenAIProviderName:   "ollama",
						telemetry.AttrGenAIRequestModel:   "llama3.1:8b",
						telemetry.AttrHecateProviderKind:  "local",
						telemetry.AttrHecateRouteReason:   "default_model_local_first",
						telemetry.AttrHecateProviderIndex: 0,
					},
				},
				{
					Name:      "router.candidate.denied",
					Timestamp: now.Add(time.Millisecond),
					Attributes: map[string]any{
						telemetry.AttrGenAIProviderName:            "ollama",
						telemetry.AttrGenAIRequestModel:            "llama3.1:8b",
						telemetry.AttrHecateProviderKind:           "local",
						telemetry.AttrHecateRouteReason:            "default_model_local_first",
						telemetry.AttrHecateProviderIndex:          0,
						telemetry.AttrHecateRouteSkipReason:        "route_denied",
						telemetry.AttrErrorMessage:                 "budget denied",
						telemetry.AttrHecateCostEstimatedMicrosUSD: int64(1200),
					},
				},
				{
					Name:      "router.candidate.selected",
					Timestamp: now.Add(2 * time.Millisecond),
					Attributes: map[string]any{
						telemetry.AttrGenAIProviderName:            "openai",
						telemetry.AttrGenAIRequestModel:            "gpt-4o-mini",
						telemetry.AttrHecateProviderKind:           "cloud",
						telemetry.AttrHecateRouteReason:            "default_model_fallback",
						telemetry.AttrHecateProviderIndex:          1,
						telemetry.AttrHecateCostEstimatedMicrosUSD: int64(2400),
					},
				},
				{
					Name:      "provider.failover.selected",
					Timestamp: now.Add(3 * time.Millisecond),
					Attributes: map[string]any{
						telemetry.AttrHecateFailoverFromProvider: "ollama",
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
