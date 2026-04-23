import { describe, expect, it } from "vitest";

import {
  buildSemanticCacheInsight,
  budgetConsumedPercent,
  buildTraceTimeline,
  describeCachePath,
  describeRouteReason,
  filterModelsByKind,
  filterModelsByProvider,
  formatTraceAttributeKey,
  formatTraceAttributeValue,
  findProvider,
  parseCSV,
  providerStatusTone,
  routeOutcomeTone,
  usdToMicros,
} from "./runtime-utils";
import type { ModelRecord, ProviderRecord, RuntimeHeaders, TraceSpanRecord } from "../types/runtime";

const models: ModelRecord[] = [
  { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } },
  { id: "llama3.1:8b", owned_by: "ollama", metadata: { provider: "ollama", provider_kind: "local" } },
];

const providers: ProviderRecord[] = [
  { name: "openai", kind: "cloud", healthy: true, status: "healthy", default_model: "gpt-4o-mini" },
  { name: "ollama", kind: "local", healthy: false, status: "open", default_model: "llama3.1:8b" },
];

const semanticHeaders: RuntimeHeaders = {
  requestId: "req-1",
  traceId: "trace-1",
  spanId: "span-1",
  provider: "ollama",
  providerKind: "local",
  routeReason: "provider_default_model",
  requestedModel: "llama3.1:8b",
  resolvedModel: "llama3.1:8b",
  cache: "true",
  cacheType: "semantic",
  semanticStrategy: "postgres_pgvector",
  semanticIndex: "hnsw",
  semanticSimilarity: "0.982",
  attempts: "1",
  retries: "0",
  fallbackFrom: "",
  costUsd: "0.000000",
};

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
    expect(describeRouteReason("provider_default_model_failover")).toBe("Provider default model after failover");
    expect(findProvider(providers, "ollama")).toEqual(providers[1]);
    expect(providerStatusTone(providers[1])).toBe("danger");
    expect(providerStatusTone(providers[0])).toBe("healthy");
    expect(routeOutcomeTone("failed")).toBe("danger");
    expect(formatTraceAttributeKey("hecate_retry_count")).toBe("hecate retry count");
    expect(formatTraceAttributeValue({ ok: true })).toBe('{"ok":true}');
  });

  it("describes cache path from runtime headers", () => {
    expect(
      describeCachePath(semanticHeaders),
    ).toEqual(
      expect.objectContaining({
        title: "Semantic cache hit",
        tone: "healthy",
      }),
    );
  });

  it("builds a semantic cache insight for hit and miss/writeback flows", () => {
    const hitInsight = buildSemanticCacheInsight(semanticHeaders, [
      {
        trace_id: "trace-1",
        span_id: "span-1",
        name: "gateway.request",
        events: [
          {
            name: "semantic_cache.hit",
            timestamp: "2026-04-21T10:00:00Z",
            attributes: {
              "hecate.semantic.scope": "tenant:team-a/model:llama3.1:8b",
              "hecate.semantic.strategy": "postgres_pgvector",
              "hecate.semantic.index_type": "hnsw",
              "hecate.semantic.similarity": 0.982,
            },
          },
        ],
      },
    ]);

    expect(hitInsight).toEqual(
      expect.objectContaining({
        title: "Semantic cache hit",
        similarity: "98.2%",
        writebackStatus: "Writeback not needed",
      }),
    );

    const missInsight = buildSemanticCacheInsight(
      {
        ...semanticHeaders,
        cache: "false",
        cacheType: "false",
        semanticStrategy: "",
        semanticIndex: "",
        semanticSimilarity: "",
      },
      [
        {
          trace_id: "trace-1",
          span_id: "span-1",
          name: "gateway.request",
          events: [
            {
              name: "semantic_cache.lookup_started",
              timestamp: "2026-04-21T10:00:00Z",
              attributes: { "hecate.semantic.scope": "tenant:team-a/model:llama3.1:8b" },
            },
            {
              name: "semantic_cache.miss",
              timestamp: "2026-04-21T10:00:01Z",
              attributes: { "hecate.semantic.scope": "tenant:team-a/model:llama3.1:8b" },
            },
            {
              name: "semantic_cache.store_finished",
              timestamp: "2026-04-21T10:00:02Z",
            },
          ],
        },
      ],
    );

    expect(missInsight).toEqual(
      expect.objectContaining({
        title: "Semantic lookup miss",
        writebackStatus: "Writeback stored",
      }),
    );
  });

  it("calculates budget consumption percent", () => {
    expect(
      budgetConsumedPercent({
        key: "global",
        scope: "global",
        backend: "memory",
        balance_source: "config",
        debited_micros_usd: 500_000,
        debited_usd: "0.500000",
        credited_micros_usd: 1_000_000,
        credited_usd: "1.000000",
        balance_micros_usd: 500_000,
        balance_usd: "0.500000",
        available_micros_usd: 500_000,
        available_usd: "0.500000",
        enforced: true,
      }),
    ).toBe(50);
  });
});
