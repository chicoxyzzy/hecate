package gateway

import (
	"fmt"
	"sort"

	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func buildRouteDecisionReport(spans []types.TraceSpan) types.RouteDecisionReport {
	events := flattenTraceEvents(spans)
	candidateIndex := map[string]int{}
	candidates := make([]types.RouteCandidateReport, 0, 8)
	report := types.RouteDecisionReport{}

	for _, event := range events {
		switch event.Name {
		case "router.candidate.considered", "router.candidate.skipped", "router.candidate.denied", "router.candidate.selected":
			candidate := routeCandidateFromEvent(event)
			key := routeCandidateKey(candidate.Provider, candidate.Model, candidate.Index)
			index, ok := candidateIndex[key]
			if !ok {
				candidateIndex[key] = len(candidates)
				candidates = append(candidates, candidate)
				index = len(candidates) - 1
			} else {
				mergeRouteCandidate(&candidates[index], candidate)
			}
			if event.Name == "router.candidate.selected" {
				report.FinalProvider = candidate.Provider
				report.FinalProviderKind = candidate.ProviderKind
				report.FinalModel = candidate.Model
				report.FinalReason = candidate.Reason
			}
		case "provider.failover.selected":
			if report.FallbackFrom == "" {
				report.FallbackFrom = stringAttr(event.Attributes, telemetry.AttrHecateFailoverFromProvider)
			}
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Index != candidates[j].Index {
			return candidates[i].Index < candidates[j].Index
		}
		if !candidates[i].Timestamp.Equal(candidates[j].Timestamp) {
			return candidates[i].Timestamp.Before(candidates[j].Timestamp)
		}
		return candidates[i].Provider < candidates[j].Provider
	})
	report.Candidates = candidates
	return report
}

func flattenTraceEvents(spans []types.TraceSpan) []types.TraceEvent {
	events := make([]types.TraceEvent, 0, 16)
	for _, span := range spans {
		events = append(events, span.Events...)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

func routeCandidateFromEvent(event types.TraceEvent) types.RouteCandidateReport {
	return types.RouteCandidateReport{
		Provider:           stringAttr(event.Attributes, telemetry.AttrGenAIProviderName),
		ProviderKind:       stringAttr(event.Attributes, telemetry.AttrHecateProviderKind),
		Model:              stringAttr(event.Attributes, telemetry.AttrGenAIRequestModel),
		Reason:             stringAttr(event.Attributes, telemetry.AttrHecateRouteReason),
		Outcome:            routeOutcomeFromEvent(event.Name, event.Attributes),
		SkipReason:         firstNonEmptyString(stringAttr(event.Attributes, telemetry.AttrHecateRouteSkipReason), stringAttr(event.Attributes, telemetry.AttrErrorMessage)),
		HealthStatus:       stringAttr(event.Attributes, telemetry.AttrHecateProviderHealthStatus),
		EstimatedMicrosUSD: int64Attr(event.Attributes, telemetry.AttrHecateCostEstimatedMicrosUSD),
		Attempt:            int(int64Attr(event.Attributes, telemetry.AttrHecateRetryAttempt)),
		Index:              int(int64Attr(event.Attributes, telemetry.AttrHecateProviderIndex)),
		Detail:             stringAttr(event.Attributes, telemetry.AttrErrorMessage),
		Timestamp:          event.Timestamp,
	}
}

func routeOutcomeFromEvent(name string, attrs map[string]any) string {
	switch name {
	case "router.candidate.considered":
		return "considered"
	case "router.candidate.skipped":
		return "skipped"
	case "router.candidate.denied":
		return "denied"
	case "router.candidate.selected":
		return "selected"
	default:
		if value := stringAttr(attrs, telemetry.AttrHecateRouteOutcome); value != "" {
			return value
		}
		return "unknown"
	}
}

func mergeRouteCandidate(target *types.RouteCandidateReport, incoming types.RouteCandidateReport) {
	if target.Provider == "" {
		target.Provider = incoming.Provider
	}
	if target.ProviderKind == "" {
		target.ProviderKind = incoming.ProviderKind
	}
	if target.Model == "" {
		target.Model = incoming.Model
	}
	if target.Reason == "" {
		target.Reason = incoming.Reason
	}
	if incoming.Outcome != "" {
		target.Outcome = incoming.Outcome
	}
	if incoming.SkipReason != "" {
		target.SkipReason = incoming.SkipReason
	}
	if incoming.HealthStatus != "" {
		target.HealthStatus = incoming.HealthStatus
	}
	if incoming.EstimatedMicrosUSD > 0 {
		target.EstimatedMicrosUSD = incoming.EstimatedMicrosUSD
	}
	if incoming.Attempt > 0 {
		target.Attempt = incoming.Attempt
	}
	if incoming.Detail != "" {
		target.Detail = incoming.Detail
	}
	if target.Timestamp.IsZero() || (!incoming.Timestamp.IsZero() && incoming.Timestamp.After(target.Timestamp)) {
		target.Timestamp = incoming.Timestamp
	}
}

func routeCandidateKey(provider, model string, index int) string {
	return fmt.Sprintf("%d:%s:%s", index, provider, model)
}

func stringAttr(attrs map[string]any, key string) string {
	if len(attrs) == 0 {
		return ""
	}
	value, ok := attrs[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func int64Attr(attrs map[string]any, key string) int64 {
	if len(attrs) == 0 {
		return 0
	}
	value, ok := attrs[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	default:
		return 0
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
