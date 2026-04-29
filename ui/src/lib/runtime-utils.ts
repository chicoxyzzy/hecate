import type { BudgetRecord, ModelFilter, ModelRecord, ProviderFilter, ProviderRecord, RuntimeHeaders, TraceEventRecord, TraceResponse, TraceSpanRecord } from "../types/runtime";

export function usdToMicros(value: string): number {
  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return Number.NaN;
  }
  return Math.round(parsed * 1_000_000);
}

export function parseCSV(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function filterModelsByKind(models: ModelRecord[], filter: ModelFilter): ModelRecord[] {
  switch (filter) {
    case "local":
      return models.filter((entry) => entry.metadata?.provider_kind === "local");
    case "cloud":
      return models.filter((entry) => entry.metadata?.provider_kind === "cloud");
    default:
      return models;
  }
}

export function filterModelsByProvider(models: ModelRecord[], provider: ProviderFilter): ModelRecord[] {
  if (provider === "auto") {
    return models;
  }
  return models.filter((entry) => entry.metadata?.provider === provider);
}

export type TraceTimelineItem = {
  name: string;
  timestamp: string;
  offsetMs: number;
  offsetLabel: string;
  spanName: string;
  spanKind: string;
  phase: "request" | "routing" | "cache" | "provider" | "governor" | "usage" | "response" | "other";
  attributes?: Record<string, unknown>;
};

export function buildTraceTimeline(spans: TraceSpanRecord[], traceStartedAt?: string): TraceTimelineItem[] {
  const flattened: TraceTimelineItem[] = [];
  const startSource = traceStartedAt || spans[0]?.start_time || "";
  const startMs = Date.parse(startSource);

  for (const span of spans) {
    for (const event of span.events ?? []) {
      const currentMs = Date.parse(event.timestamp);
      const offsetMs = Number.isFinite(startMs) && Number.isFinite(currentMs) ? Math.max(0, currentMs - startMs) : 0;
      flattened.push({
        name: event.name,
        timestamp: event.timestamp,
        offsetMs,
        offsetLabel: `${offsetMs} ms`,
        spanName: span.name,
        spanKind: span.kind || "internal",
        phase: tracePhaseFromEvent(event.name),
        attributes: event.attributes,
      });
    }
  }

  flattened.sort((left, right) => Date.parse(left.timestamp) - Date.parse(right.timestamp));
  return flattened;
}

export function findModelInTrace(spans: TraceSpanRecord[], provider?: string): string {
  const normalizedProvider = provider?.trim();
  const candidates: Array<{ priority: number; timestamp: number; model: string }> = [];

  for (const span of spans) {
    for (const event of span.events ?? []) {
      const attrs = event.attributes ?? {};
      if (normalizedProvider) {
        const eventProvider = traceStringAttr(attrs, "gen_ai.provider.name");
        if (eventProvider && eventProvider !== normalizedProvider) {
          continue;
        }
      }

      const responseModel = traceStringAttr(attrs, "gen_ai.response.model");
      if (responseModel) {
        candidates.push({ priority: 3, timestamp: Date.parse(event.timestamp), model: responseModel });
      }

      const requestModel = traceStringAttr(attrs, "gen_ai.request.model");
      if (requestModel) {
        const priority = event.name === "provider.call.finished" || event.name === "router.candidate.selected" ? 2 : 1;
        candidates.push({ priority, timestamp: Date.parse(event.timestamp), model: requestModel });
      }
    }
  }

  candidates.sort((left, right) => {
    if (left.priority !== right.priority) {
      return right.priority - left.priority;
    }
    const leftTime = Number.isFinite(left.timestamp) ? left.timestamp : 0;
    const rightTime = Number.isFinite(right.timestamp) ? right.timestamp : 0;
    return rightTime - leftTime;
  });

  return candidates[0]?.model ?? "";
}

function traceStringAttr(attrs: Record<string, unknown>, key: string): string {
  const value = attrs[key];
  return typeof value === "string" ? value.trim() : "";
}

export function describeRouteReason(reason?: string): string {
  if (!reason) {
    return "No route reason";
  }

  const suffixes: string[] = [];
  let base = reason;
  if (base.endsWith("_half_open_recovery")) {
    base = base.slice(0, -"_half_open_recovery".length);
    suffixes.push("recovery probe");
  }
  if (base.endsWith("_degraded")) {
    base = base.slice(0, -"_degraded".length);
    suffixes.push("degraded provider");
  }
  if (base.endsWith("_failover")) {
    base = base.slice(0, -"_failover".length);
    suffixes.push("after failover");
  }

  const labels: Record<string, string> = {
    global_default_model: "Global default model",
    pinned_provider: "Pinned provider",
    pinned_provider_model: "Pinned provider and model",
    provider_default_model: "Provider default model",
    requested_model: "Requested model",
  };

  const label = labels[base] ?? titleizeIdentifier(base);
  if (suffixes.length === 0) {
    return label;
  }
  return `${label} ${suffixes.join(", ")}`;
}

export function describeRouteSkipReason(reason?: string): string {
  if (!reason) {
    return "";
  }
  const labels: Record<string, string> = {
    budget_denied: "Budget denied",
    policy_denied: "Policy denied",
    preflight_price_missing: "Missing price",
    provider_not_found: "Provider missing",
    route_denied: "Route denied",
    provider_retry_exhausted: "Retry exhausted",
    provider_unavailable: "Provider unavailable",
  };
  return labels[reason] ?? titleizeIdentifier(reason);
}

export function describeRoutingBlockedReason(reason?: string): string {
  if (!reason) {
    return "Routing blocked";
  }
  const labels: Record<string, string> = {
    credential_missing: "Missing credentials",
    provider_disabled: "Provider disabled",
    circuit_open: "Circuit open",
    provider_rate_limited: "Cooling down after upstream 429",
    provider_unhealthy: "Provider unhealthy",
    no_models: "No discovered models",
  };
  return labels[reason] ?? titleizeIdentifier(reason);
}

export function describeCredentialState(state?: string): string {
  switch (state) {
    case "configured":
      return "Configured";
    case "missing":
      return "Missing";
    case "not_required":
      return "Not required";
    default:
      return state ? titleizeIdentifier(state) : "Unknown";
  }
}

export function describeHealthErrorClass(kind?: string): string {
  switch (kind) {
    case "rate_limit":
      return "Upstream rate limit";
    case "timeout":
      return "Timeout";
    case "server_error":
      return "Server error";
    case "other":
      return "Other error";
    default:
      return kind ? titleizeIdentifier(kind) : "Unknown";
  }
}

function titleizeIdentifier(value: string): string {
  return value
    .split("_")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

export function routeOutcomeTone(outcome?: string): "healthy" | "warning" | "danger" | "neutral" {
  switch (outcome) {
    case "selected":
    case "completed":
      return "healthy";
    case "failed":
      return "danger";
    case "denied":
    case "skipped":
      return "warning";
    default:
      return "neutral";
  }
}

export function healthStatusTone(status?: string): "healthy" | "warning" | "danger" | "neutral" {
  switch (status) {
    case "healthy":
      return "healthy";
    case "degraded":
    case "half_open":
      return "warning";
    case "open":
    case "unhealthy":
      return "danger";
    default:
      return "neutral";
  }
}

export function describeHealthStatus(status?: string): string {
  switch (status) {
    case "half_open":
      return "Recovery probe";
    case "open":
      return "Circuit open";
    case "degraded":
      return "Degraded";
    case "healthy":
      return "Healthy";
    case "unhealthy":
      return "Unhealthy";
    default:
      return "Unknown health";
  }
}

export function formatTraceAttributeKey(value: string): string {
  return value.replaceAll("_", " ");
}

export function formatTraceAttributeValue(value: unknown): string {
  if (value === null || value === undefined) {
    return "n/a";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}

export function describeCachePath(runtimeHeaders?: RuntimeHeaders | null): { title: string; detail: string; tone: "healthy" | "warning" | "neutral" } {
  if (!runtimeHeaders) {
    return {
      title: "No runtime metadata",
      detail: "Run a request to inspect exact cache, semantic lookup, and provider execution details.",
      tone: "neutral",
    };
  }

  if (runtimeHeaders.cache === "true" && runtimeHeaders.cacheType === "semantic") {
    return {
      title: "Semantic cache hit",
      detail: runtimeHeaders.semanticStrategy
        ? `Matched via ${runtimeHeaders.semanticStrategy} with similarity ${runtimeHeaders.semanticSimilarity || "n/a"} using ${runtimeHeaders.semanticIndex || "unknown"} indexing.`
        : "A semantic match was returned for this request.",
      tone: "healthy",
    };
  }

  if (runtimeHeaders.cache === "true") {
    return {
      title: "Exact cache hit",
      detail: "The response was returned from exact request cache without an upstream provider call.",
      tone: "healthy",
    };
  }

  if (runtimeHeaders.semanticStrategy) {
    return {
      title: "Semantic lookup executed",
      detail: `No cache hit was returned, but semantic lookup metadata came back from ${runtimeHeaders.semanticStrategy}.`,
      tone: "warning",
    };
  }

  return {
    title: "Provider execution path",
    detail: "This request went through normal routing and provider execution without a cache hit.",
    tone: "neutral",
  };
}

export type TraceRouteRecord = TraceResponse["data"]["route"];
export type SemanticCacheInsight = {
  title: string;
  tone: "healthy" | "warning" | "danger" | "neutral";
  summary: string;
  detail: string;
  strategy: string;
  index: string;
  similarity: string;
  scope: string;
  writebackStatus: string;
  writebackTone: "healthy" | "warning" | "danger" | "neutral";
  writebackDetail: string;
};

function findTraceEvent(spans: TraceSpanRecord[], name: string): TraceEventRecord | null {
  for (let spanIndex = spans.length - 1; spanIndex >= 0; spanIndex -= 1) {
    const events = spans[spanIndex]?.events ?? [];
    for (let eventIndex = events.length - 1; eventIndex >= 0; eventIndex -= 1) {
      const event = events[eventIndex];
      if (event?.name === name) {
        return event;
      }
    }
  }

  return null;
}

function traceAttributeAsString(event: TraceEventRecord | null, key: string): string {
  const value = event?.attributes?.[key];
  if (value === null || value === undefined || value === "") {
    return "";
  }
  return String(value);
}

function formatSimilarity(value: string): string {
  if (!value) {
    return "n/a";
  }
  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed)) {
    return value;
  }
  return `${(parsed * 100).toFixed(1)}%`;
}

export function buildSemanticCacheInsight(
  runtimeHeaders?: RuntimeHeaders | null,
  spans: TraceSpanRecord[] = [],
): SemanticCacheInsight | null {
  const lookupStarted = findTraceEvent(spans, "semantic_cache.lookup_started");
  const miss = findTraceEvent(spans, "semantic_cache.miss");
  const hit = findTraceEvent(spans, "semantic_cache.hit");
  const storeFinished = findTraceEvent(spans, "semantic_cache.store_finished");
  const storeFailed = findTraceEvent(spans, "semantic_cache.store_failed");

  const cacheType = runtimeHeaders?.cacheType || "";
  const hasSemanticHeaders = Boolean(runtimeHeaders?.semanticStrategy || runtimeHeaders?.semanticIndex || runtimeHeaders?.semanticSimilarity);
  const hasSemanticTrace = Boolean(lookupStarted || miss || hit || storeFinished || storeFailed);
  if (cacheType !== "semantic" && !hasSemanticHeaders && !hasSemanticTrace) {
    return null;
  }

  const strategy =
    runtimeHeaders?.semanticStrategy ||
    traceAttributeAsString(hit, "hecate.semantic.strategy") ||
    "unknown";
  const index =
    runtimeHeaders?.semanticIndex ||
    traceAttributeAsString(hit, "hecate.semantic.index_type") ||
    "n/a";
  const similarityRaw =
    runtimeHeaders?.semanticSimilarity ||
    traceAttributeAsString(hit, "hecate.semantic.similarity");
  const scope =
    traceAttributeAsString(hit, "hecate.semantic.scope") ||
    traceAttributeAsString(miss, "hecate.semantic.scope") ||
    traceAttributeAsString(lookupStarted, "hecate.semantic.scope") ||
    "default";

  if (cacheType === "semantic" || hit) {
    return {
      title: "Semantic cache hit",
      tone: "healthy",
      summary: "Matched a prior response and skipped upstream execution.",
      detail: `The gateway reused a semantic match from scope ${scope}.`,
      strategy,
      index,
      similarity: formatSimilarity(similarityRaw),
      scope,
      writebackStatus: "Writeback not needed",
      writebackTone: "neutral",
      writebackDetail: "This request resolved from semantic cache, so no new semantic entry needed to be stored.",
    };
  }

  if (storeFailed) {
    return {
      title: "Semantic lookup miss",
      tone: "warning",
      summary: "Lookup missed, provider execution continued, and semantic writeback failed.",
      detail: `The request searched semantic scope ${scope} before falling through to provider execution.`,
      strategy,
      index,
      similarity: formatSimilarity(similarityRaw),
      scope,
      writebackStatus: "Writeback failed",
      writebackTone: "danger",
      writebackDetail: "The runtime attempted to persist this response for future semantic reuse, but the store operation failed.",
    };
  }

  if (storeFinished) {
    return {
      title: "Semantic lookup miss",
      tone: "warning",
      summary: "Lookup missed, provider execution continued, and the new response was stored.",
      detail: `The request searched semantic scope ${scope} before falling through to provider execution.`,
      strategy,
      index,
      similarity: formatSimilarity(similarityRaw),
      scope,
      writebackStatus: "Writeback stored",
      writebackTone: "healthy",
      writebackDetail: "The runtime persisted the final response for future semantic matches.",
    };
  }

  if (miss || lookupStarted || hasSemanticHeaders) {
    return {
      title: "Semantic lookup executed",
      tone: "warning",
      summary: "Lookup ran before normal provider execution.",
      detail: `The gateway checked semantic scope ${scope} and did not return a cached answer.`,
      strategy,
      index,
      similarity: formatSimilarity(similarityRaw),
      scope,
      writebackStatus: "No writeback signal",
      writebackTone: "neutral",
      writebackDetail: "No semantic writeback event was captured for this request.",
    };
  }

  return null;
}

export function countRouteHealthStatuses(route?: TraceRouteRecord | null): { healthy: number; warning: number; danger: number } {
  const summary = { healthy: 0, warning: 0, danger: 0 };

  for (const candidate of route?.candidates ?? []) {
    const tone = healthStatusTone(candidate.health_status);
    if (tone === "healthy") {
      summary.healthy += 1;
    } else if (tone === "warning") {
      summary.warning += 1;
    } else if (tone === "danger") {
      summary.danger += 1;
    }
  }

  return summary;
}

export function describeRouteRecovery(route?: TraceRouteRecord | null, runtimeHeaders?: RuntimeHeaders | null): string {
  const selectedCandidate = route?.candidates?.find((candidate) => candidate.outcome === "selected");
  const fallbackFrom = runtimeHeaders?.fallbackFrom || route?.fallback_from;

  if (selectedCandidate?.health_status === "half_open") {
    return "Recovered via half-open provider probe";
  }

  if (fallbackFrom) {
    return `Failed over from ${fallbackFrom}`;
  }

  if ((route?.failovers?.length ?? 0) > 0) {
    return "Recovered after one or more failover hops";
  }

  return "No recovery path needed";
}

export function providerStatusTone(provider?: ProviderRecord): "healthy" | "warning" | "danger" | "neutral" {
  if (!provider) {
    return "neutral";
  }
  if (!provider.healthy && provider.status === "healthy") {
    return "warning";
  }
  return healthStatusTone(provider.status);
}

export function findProvider(providers: ProviderRecord[], providerName?: string): ProviderRecord | null {
  if (!providerName) {
    return null;
  }
  return providers.find((provider) => provider.name === providerName) ?? null;
}

export function budgetConsumedPercent(budget?: BudgetRecord | null): number {
  if (!budget || budget.credited_micros_usd <= 0) {
    return 0;
  }
  return Math.max(0, Math.min(100, Math.round((budget.debited_micros_usd / budget.credited_micros_usd) * 100)));
}

export function tracePhaseFromEvent(name: string): TraceTimelineItem["phase"] {
  if (name.startsWith("request.")) {
    return "request";
  }
  if (name.startsWith("router.")) {
    return "routing";
  }
  if (name.startsWith("cache.") || name.startsWith("semantic.")) {
    return "cache";
  }
  if (name.startsWith("provider.")) {
    return "provider";
  }
  if (name.startsWith("governor.")) {
    return "governor";
  }
  if (name.startsWith("usage.") || name.startsWith("cost.")) {
    return "usage";
  }
  if (name.startsWith("response.")) {
    return "response";
  }
  return "other";
}

export function describeBudgetScope(budget?: BudgetRecord | null): string {
  if (!budget) {
    return "No scope";
  }

  const parts = [budget.scope];
  if (budget.tenant) {
    parts.push(`tenant ${budget.tenant}`);
  }
  if (budget.provider) {
    parts.push(`provider ${budget.provider}`);
  }
  return parts.join(" / ");
}

export function budgetWarningTone(triggered: boolean): "healthy" | "warning" | "neutral" {
  return triggered ? "warning" : "neutral";
}
