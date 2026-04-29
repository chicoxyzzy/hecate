package gateway

import (
	"context"

	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/router"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

const routeDiagnosticsIndexOffset = 1000

func recordRouteDiagnostics(ctx context.Context, trace *profiler.Trace, routeRouter router.Router, req types.ChatRequest, selected types.RouteDecision) {
	diagnosticRouter, ok := routeRouter.(router.DiagnosticRouter)
	if !ok {
		return
	}
	for index, candidate := range diagnosticRouter.RouteDiagnostics(ctx, req, selected) {
		recordTrace(trace, "router.candidate.skipped", "routing", map[string]any{
			telemetry.AttrGenAIProviderName:          candidate.Provider,
			telemetry.AttrGenAIRequestModel:          candidate.Model,
			telemetry.AttrHecateProviderKind:         candidate.ProviderKind,
			telemetry.AttrHecateRouteReason:          candidate.Reason,
			telemetry.AttrHecateProviderIndex:        routeDiagnosticsIndexOffset + index,
			telemetry.AttrHecateRouteOutcome:         "skipped",
			telemetry.AttrHecateRouteSkipReason:      candidate.SkipReason,
			telemetry.AttrHecateProviderHealthStatus: candidate.HealthStatus,
		})
	}
}
