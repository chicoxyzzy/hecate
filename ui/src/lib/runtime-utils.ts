import type { BudgetRecord, ModelFilter, ModelRecord, ProviderFilter, ProviderRecord, TraceSpanRecord } from "../types/runtime";

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

export function describeRouteReason(reason?: string): string {
  if (!reason) {
    return "No route reason";
  }

  return reason
    .split("_")
    .map((part) => {
      switch (part) {
        case "local":
          return "local";
        case "first":
          return "first";
        case "failover":
          return "failover";
        default:
          return part.charAt(0).toUpperCase() + part.slice(1);
      }
    })
    .join(" ");
}

export function providerStatusTone(provider?: ProviderRecord): "healthy" | "warning" | "danger" | "neutral" {
  if (!provider) {
    return "neutral";
  }
  if (provider.status === "open") {
    return "danger";
  }
  if (provider.status === "degraded" || provider.status === "half_open" || !provider.healthy) {
    return "warning";
  }
  return "healthy";
}

export function findProvider(providers: ProviderRecord[], providerName?: string): ProviderRecord | null {
  if (!providerName) {
    return null;
  }
  return providers.find((provider) => provider.name === providerName) ?? null;
}

export function budgetConsumedPercent(budget?: BudgetRecord | null): number {
  if (!budget || budget.max_micros_usd <= 0) {
    return 0;
  }
  return Math.max(0, Math.min(100, Math.round((budget.current_micros_usd / budget.max_micros_usd) * 100)));
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
