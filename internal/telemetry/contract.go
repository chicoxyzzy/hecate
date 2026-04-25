package telemetry

// Event name constants — the closed set of event names passed to trace.Record.
// Use these constants instead of string literals so that event names are
// typo-safe, statically searchable, and form part of the frozen signal contract.

// Gateway request lifecycle
const (
	EventRequestReceived = "request.received"
	EventRequestInvalid  = "request.invalid"
)

// Governor
const (
	EventGovernorAllowed              = "governor.allowed"
	EventGovernorDenied               = "governor.denied"
	EventGovernorModelRewrite         = "governor.model_rewrite"
	EventGovernorBudgetEstimateFailed = "governor.budget_estimate_failed"
	EventGovernorRouteDenied          = "governor.route_denied"
	EventGovernorRouteAllowed         = "governor.route_allowed"
	EventGovernorUsageRecordFailed    = "governor.usage_record_failed"
)

// Router
const (
	EventRouterFailed              = "router.failed"
	EventRouterSelected            = "router.selected"
	EventRouterCandidateConsidered = "router.candidate.considered"
	EventRouterCandidateSkipped    = "router.candidate.skipped"
	EventRouterCandidateDenied     = "router.candidate.denied"
	EventRouterCandidateSelected   = "router.candidate.selected"
)

// Exact cache
const (
	EventCacheHit  = "cache.hit"
	EventCacheMiss = "cache.miss"
)

// Semantic cache
const (
	EventSemanticCacheLookupStarted = "semantic_cache.lookup_started"
	EventSemanticCacheHit           = "semantic_cache.hit"
	EventSemanticCacheMiss          = "semantic_cache.miss"
	EventSemanticCacheStoreFinished = "semantic_cache.store_finished"
	EventSemanticCacheStoreFailed   = "semantic_cache.store_failed"
)

// Provider execution
const (
	EventProviderCallStarted        = "provider.call.started"
	EventProviderCallFinished       = "provider.call.finished"
	EventProviderCallFailed         = "provider.call.failed"
	EventProviderRetryScheduled     = "provider.retry.scheduled"
	EventProviderRetryBackoffFailed = "provider.retry.backoff_failed"
	EventProviderFailoverSelected   = "provider.failover.selected"
	EventProviderFailoverSkipped    = "provider.failover.skipped"
	EventProviderHealthDegraded     = "provider.health.degraded"
)

// Response pipeline
const (
	EventUsageNormalized      = "usage.normalized"
	EventCostCalculated       = "cost.calculated"
	EventCostEstimateUnpriced = "cost.estimate_unpriced"
	EventResponseReturned     = "response.returned"
)

// Body capture (opt-in via GATEWAY_TRACE_BODIES)
const (
	EventRequestBodyCaptured  = "request.body.captured"
	EventResponseBodyCaptured = "response.body.captured"
)

// Queue lifecycle — recorded in the runner when jobs move through the queue.
const (
	EventQueueEnqueued          = "queue.enqueued"
	EventQueueClaimed           = "queue.claimed"
	EventQueueAcked             = "queue.acked"
	EventQueueNacked            = "queue.nacked"
	EventQueueLeaseExtended     = "queue.lease_extended"
	EventQueueLeaseExtendFailed = "queue.lease_extend_failed"
)

// Orchestrator
const (
	EventOrchestratorTaskStarted       = "orchestrator.task.started"
	EventOrchestratorTaskFinished      = "orchestrator.task.finished"
	EventOrchestratorRunStarted        = "orchestrator.run.started"
	EventOrchestratorRunFailed         = "orchestrator.run.failed"
	EventOrchestratorRunFinished       = "orchestrator.run.finished"
	EventOrchestratorStepCompleted     = "orchestrator.step.completed"
	EventOrchestratorStepFailed        = "orchestrator.step.failed"
	EventOrchestratorArtifactCreated   = "orchestrator.artifact.created"
	EventOrchestratorArtifactFailed    = "orchestrator.artifact.failed"
	EventOrchestratorApprovalRequested = "orchestrator.approval.requested"
	EventOrchestratorApprovalResolved  = "orchestrator.approval.resolved"
	EventOrchestratorApprovalFailed    = "orchestrator.approval.failed"
)

// Retention
const (
	EventRetentionRunStarted        = "retention.run.started"
	EventRetentionRunFinished       = "retention.run.finished"
	EventRetentionSubsystemFailed   = "retention.subsystem.failed"
	EventRetentionSubsystemFinished = "retention.subsystem.finished"
	EventRetentionHistoryFailed     = "retention.history.failed"
	EventRetentionHistoryPersisted  = "retention.history.persisted"
)

// ---------------------------------------------------------------------------
// Span name constants — the parent spans that events are grouped into.
// These match the mapping in profiler.spanSpecForEvent.
// ---------------------------------------------------------------------------

const (
	SpanGatewayRequest       = "gateway.request"
	SpanGatewayRequestParse  = "gateway.request.parse"
	SpanGatewayGovernor      = "gateway.governor"
	SpanGatewayCacheExact    = "gateway.cache.exact"
	SpanGatewayCacheSemantic = "gateway.cache.semantic"
	SpanGatewayRouter        = "gateway.router"
	SpanGatewayProvider      = "gateway.provider"
	SpanGatewayUsage         = "gateway.usage"
	SpanGatewayCost          = "gateway.cost"
	SpanGatewayResponse      = "gateway.response"
	SpanGatewayRuntime       = "gateway.runtime"

	SpanOrchestratorTask     = "orchestrator.task"
	SpanOrchestratorRun      = "orchestrator.run"
	SpanOrchestratorStep     = "orchestrator.step"
	SpanOrchestratorArtifact = "orchestrator.artifact"
	SpanOrchestratorApproval = "orchestrator.approval"
	SpanOrchestratorQueue    = "orchestrator.queue"

	SpanRetentionRun = "retention.run"
)

// ---------------------------------------------------------------------------
// Metric name constants — the authoritative instrument names.
// The instrument definitions in metrics.go MUST match these exactly.
// Tests in contract_test.go enforce this.
// ---------------------------------------------------------------------------

const (
	MetricGatewayRequests        = "hecate.gateway.requests"
	MetricGatewayRequestDuration = "hecate.gateway.request.duration"
	MetricChatRequestsTotal      = "gen_ai.gateway.chat.requests"
	MetricCostMicrosTotal        = "gen_ai.gateway.cost"
	MetricInputTokensTotal       = "gen_ai.client.tokens.input"
	MetricOutputTokensTotal      = "gen_ai.client.tokens.output"
	MetricTotalTokensTotal       = "gen_ai.client.tokens.total"
	MetricRetriesTotal           = "hecate.gateway.retries"
	MetricFailoversTotal         = "hecate.gateway.failovers"

	// Orchestrator metrics
	MetricOrchestratorRunsTotal            = "hecate.orchestrator.runs"
	MetricOrchestratorRunDuration          = "hecate.orchestrator.run.duration"
	MetricOrchestratorQueueWaitDuration    = "hecate.orchestrator.queue.wait_duration"
	MetricOrchestratorStepsTotal           = "hecate.orchestrator.steps"
	MetricOrchestratorStepDuration         = "hecate.orchestrator.step.duration"
	MetricOrchestratorApprovalsTotal       = "hecate.orchestrator.approvals"
	MetricOrchestratorApprovalWaitDuration = "hecate.orchestrator.approval.wait_duration"
	MetricOrchestratorLeaseExtendFailures  = "hecate.orchestrator.queue.lease_extend_failures"
)

// ---------------------------------------------------------------------------
// Error kind constants — the closed set of allowed hecate.error.kind values.
// All callers should use NormalizeErrorKind before recording this attribute.
// ---------------------------------------------------------------------------

const (
	ErrorKindInvalidRequest     = "invalid_request"
	ErrorKindRequestDenied      = "request_denied"
	ErrorKindRouterFailed       = "router_failed"
	ErrorKindBudgetEstimate     = "budget_estimate_failed"
	ErrorKindRouteDenied        = "route_denied"
	ErrorKindProviderCallFailed = "provider_call_failed"
	ErrorKindRetryBackoff       = "retry_backoff_failed"
	ErrorKindProviderHealth     = "provider_health_degraded"
	ErrorKindSemanticCache      = "semantic_cache_store_failed"
	ErrorKindUsageRecord        = "usage_record_failed"
	// ErrorKindOther is the fallback for any value not in the known set.
	ErrorKindOther = "other"
)

var knownErrorKinds = map[string]struct{}{
	ErrorKindInvalidRequest:     {},
	ErrorKindRequestDenied:      {},
	ErrorKindRouterFailed:       {},
	ErrorKindBudgetEstimate:     {},
	ErrorKindRouteDenied:        {},
	ErrorKindProviderCallFailed: {},
	ErrorKindRetryBackoff:       {},
	ErrorKindProviderHealth:     {},
	ErrorKindSemanticCache:      {},
	ErrorKindUsageRecord:        {},
	ErrorKindOther:              {},
}

var knownResults = map[string]struct{}{
	ResultSuccess: {},
	ResultDenied:  {},
	ResultError:   {},
}

// NormalizeErrorKind returns kind unchanged if it belongs to the contract's
// closed error-kind set, otherwise returns ErrorKindOther. Always pass
// hecate.error.kind values through this function before recording them as
// span attributes or metric labels to prevent high-cardinality explosions.
func NormalizeErrorKind(kind string) string {
	if _, ok := knownErrorKinds[kind]; ok {
		return kind
	}
	return ErrorKindOther
}

// NormalizeResult returns result unchanged when it is one of the three defined
// values (ResultSuccess, ResultDenied, ResultError). Any other value is mapped
// to ResultError.
func NormalizeResult(result string) string {
	if _, ok := knownResults[result]; ok {
		return result
	}
	return ResultError
}

// ---------------------------------------------------------------------------
// Required attribute schema — the minimum set of attributes each event MUST
// carry. Validated by tests; use ValidateEventAttrs in test helpers.
// ---------------------------------------------------------------------------

// requiredEventAttrs maps event name → the attribute keys that must be present
// in attrs when that event is recorded. Events not listed here have no
// contract-enforced required attributes (but may still carry useful attrs).
var requiredEventAttrs = map[string][]string{
	EventRequestReceived: {
		AttrHecateRequestMessageCount,
		AttrGenAIRequestModel,
	},
	EventGovernorDenied: {
		AttrHecateGovernorResult,
		AttrHecateErrorKind,
	},
	EventGovernorAllowed: {
		AttrHecateGovernorResult,
	},
	EventRouterSelected: {
		AttrGenAIProviderName,
		AttrGenAIRequestModel,
		AttrHecateRouteReason,
	},
	EventGovernorRouteDenied: {
		AttrGenAIProviderName,
		AttrHecateErrorKind,
	},
	EventGovernorRouteAllowed: {
		AttrGenAIProviderName,
		AttrHecateCostEstimatedMicrosUSD,
	},
	EventProviderCallStarted: {
		AttrGenAIProviderName,
		AttrGenAIRequestModel,
		AttrHecateRetryAttempt,
	},
	EventProviderCallFinished: {
		AttrGenAIProviderName,
		AttrGenAIRequestModel,
		AttrHecateProviderLatencyMS,
	},
	EventProviderCallFailed: {
		AttrGenAIProviderName,
		AttrGenAIRequestModel,
		AttrHecateErrorKind,
	},
	EventUsageNormalized: {
		AttrGenAIUsageInputTokens,
		AttrGenAIUsageOutputTokens,
		AttrGenAIUsageTotalTokens,
	},
	EventCostCalculated: {
		AttrHecateCostTotalMicrosUSD,
	},
	EventResponseReturned: {
		AttrGenAIProviderName,
		AttrGenAIResponseModel,
		AttrGenAIRequestModel,
	},
	EventCacheHit: {
		AttrHecateCacheHit,
		AttrHecateCacheType,
		AttrHecateCacheKey,
	},
	EventCacheMiss: {
		AttrHecateCacheHit,
		AttrHecateCacheType,
		AttrHecateCacheKey,
	},
	EventSemanticCacheHit: {
		AttrHecateCacheHit,
		AttrHecateCacheType,
		AttrHecateSemanticScope,
	},
	EventSemanticCacheMiss: {
		AttrHecateCacheHit,
		AttrHecateCacheType,
		AttrHecateSemanticScope,
	},
}

// RequiredAttrsForEvent returns the required attribute keys for the given event
// name, or nil for event names not listed in the schema (unconstrained events).
// Use this in test helpers that verify trace output completeness.
func RequiredAttrsForEvent(eventName string) []string {
	required := requiredEventAttrs[eventName]
	if len(required) == 0 {
		return nil
	}
	out := make([]string, len(required))
	copy(out, required)
	return out
}

// ValidateEventAttrs returns the attribute keys required for eventName that are
// absent from attrs. An empty (or nil) return means the event passes the
// contract. Unknown event names always pass (nil return).
func ValidateEventAttrs(eventName string, attrs map[string]any) []string {
	required, ok := requiredEventAttrs[eventName]
	if !ok {
		return nil
	}
	var missing []string
	for _, k := range required {
		if _, present := attrs[k]; !present {
			missing = append(missing, k)
		}
	}
	return missing
}
