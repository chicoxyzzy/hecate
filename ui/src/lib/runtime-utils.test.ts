import { describe, expect, it } from "vitest";

import {
  budgetConsumedPercent,
  buildTraceTimeline,
  describeRouteReason,
  filterModelsByKind,
  filterModelsByProvider,
  findProvider,
  parseCSV,
  providerStatusTone,
  usdToMicros,
} from "./runtime-utils";
import type { ModelRecord, ProviderRecord, TraceSpanRecord } from "../types/runtime";

const models: ModelRecord[] = [
  { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } },
  { id: "llama3.1:8b", owned_by: "ollama", metadata: { provider: "ollama", provider_kind: "local" } },
];

const providers: ProviderRecord[] = [
  { name: "openai", kind: "cloud", healthy: true, status: "healthy", default_model: "gpt-4o-mini" },
  { name: "ollama", kind: "local", healthy: false, status: "open", default_model: "llama3.1:8b" },
];

describe("runtime-utils", () => {
  it("converts usd strings to micros", () => {
    expect(usdToMicros("1.25")).toBe(1_250_000);
    expect(Number.isNaN(usdToMicros("-1"))).toBe(true);
  });

  it("parses csv into trimmed items", () => {
    expect(parseCSV(" openai, ollama , ,localai ")).toEqual(["openai", "ollama", "localai"]);
  });

  it("filters models by kind", () => {
    expect(filterModelsByKind(models, "local")).toEqual([models[1]]);
    expect(filterModelsByKind(models, "cloud")).toEqual([models[0]]);
    expect(filterModelsByKind(models, "all")).toEqual(models);
  });

  it("filters models by provider", () => {
    expect(filterModelsByProvider(models, "ollama")).toEqual([models[1]]);
    expect(filterModelsByProvider(models, "auto")).toEqual(models);
  });

  it("builds a trace timeline with derived phases", () => {
    const spans: TraceSpanRecord[] = [
      {
        trace_id: "trace-1",
        span_id: "span-1",
        name: "gateway.request",
        start_time: "2026-04-21T10:00:00Z",
        events: [
          { name: "request.received", timestamp: "2026-04-21T10:00:00Z" },
          { name: "router.selected", timestamp: "2026-04-21T10:00:01Z" },
        ],
      },
    ];

    expect(buildTraceTimeline(spans, "2026-04-21T10:00:00Z")).toEqual([
      expect.objectContaining({ name: "request.received", phase: "request", offsetMs: 0 }),
      expect.objectContaining({ name: "router.selected", phase: "routing", offsetMs: 1000 }),
    ]);
  });

  it("formats route and provider diagnostics", () => {
    expect(describeRouteReason("default_model_local_first_failover")).toBe("Default Model local first failover");
    expect(findProvider(providers, "ollama")).toEqual(providers[1]);
    expect(providerStatusTone(providers[1])).toBe("danger");
    expect(providerStatusTone(providers[0])).toBe("healthy");
  });

  it("calculates budget consumption percent", () => {
    expect(
      budgetConsumedPercent({
        key: "global",
        scope: "global",
        backend: "memory",
        limit_source: "config",
        spent_micros_usd: 500_000,
        spent_usd: "0.500000",
        current_micros_usd: 500_000,
        current_usd: "0.500000",
        max_micros_usd: 1_000_000,
        max_usd: "1.000000",
        remaining_micros_usd: 500_000,
        remaining_usd: "0.500000",
        enforced: true,
      }),
    ).toBe(50);
  });
});
