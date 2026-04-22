import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PlaygroundView } from "./PlaygroundView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

describe("PlaygroundView", () => {
  it("groups provider options by local and cloud routes", () => {
    render(
      <PlaygroundView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          localProviders: [{ name: "ollama", kind: "local", healthy: true, status: "healthy", default_model: "llama3.1:8b" }],
          cloudProviders: [{ name: "openai", kind: "cloud", healthy: true, status: "healthy", default_model: "gpt-4o-mini" }],
          localModels: [
            {
              id: "llama3.1:8b",
              owned_by: "ollama",
              metadata: { provider: "ollama", provider_kind: "local" },
            },
          ],
          cloudModels: [
            {
              id: "gpt-4o-mini",
              owned_by: "openai",
              metadata: { provider: "openai", provider_kind: "cloud" },
            },
          ],
          providerScopedModels: [],
        })}
      />,
    );

    expect(screen.getByRole("option", { name: "ollama" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "openai" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "llama3.1:8b" })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "gpt-4o-mini" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Run through Hecate/i })).toBeInTheDocument();
  });

  it("shows trace events when a trace has been loaded", () => {
    render(
      <PlaygroundView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          providers: [{ name: "openai", kind: "cloud", healthy: true, status: "healthy", default_model: "gpt-4o-mini" }],
          runtimeHeaders: {
            requestId: "req-1",
            traceId: "trace-1",
            spanId: "span-1",
            provider: "openai",
            providerKind: "cloud",
            routeReason: "default_model_local_first_failover",
            requestedModel: "gpt-4o-mini",
            resolvedModel: "gpt-4o-mini",
            cache: "false",
            cacheType: "false",
            semanticStrategy: "postgres_pgvector",
            semanticIndex: "hnsw",
            semanticSimilarity: "",
            attempts: "2",
            retries: "1",
            fallbackFrom: "ollama",
            costUsd: "0.000012",
          },
          traceRoute: {
            final_provider: "openai",
            final_provider_kind: "cloud",
            final_model: "gpt-4o-mini",
            final_reason: "default_model_local_first_failover",
            fallback_from: "ollama",
            candidates: [
              {
                provider: "ollama",
                provider_kind: "local",
                model: "llama3.1:8b",
                reason: "default_model_local_first",
                outcome: "denied",
                skip_reason: "route_denied",
                health_status: "open",
                estimated_usd: "0.000000",
              },
              {
                provider: "openai",
                provider_kind: "cloud",
                model: "gpt-4o-mini",
                reason: "default_model_local_first_failover",
                outcome: "selected",
                health_status: "half_open",
                estimated_usd: "0.000012",
                retry_count: 1,
                attempt: 2,
                latency_ms: 220,
                failover_from: "ollama",
              },
            ],
            failovers: [
              {
                from_provider: "ollama",
                from_model: "llama3.1:8b",
                to_provider: "openai",
                to_model: "gpt-4o-mini",
                reason: "provider_retry_exhausted",
                timestamp: "2026-04-21T10:00:00Z",
              },
            ],
          },
          traceSpans: [
            {
              trace_id: "trace-1",
              span_id: "span-1",
              name: "gateway.request",
              status_code: "ok",
              events: [
                { name: "router.selected", timestamp: "2026-04-21T10:00:00Z", attributes: { "gen_ai.provider.name": "openai" } },
                { name: "semantic_cache.lookup_started", timestamp: "2026-04-21T10:00:00Z", attributes: { "hecate.semantic.scope": "tenant:team-a/model:gpt-4o-mini" } },
                { name: "semantic_cache.miss", timestamp: "2026-04-21T10:00:00Z", attributes: { "hecate.semantic.scope": "tenant:team-a/model:gpt-4o-mini" } },
                { name: "semantic_cache.store_finished", timestamp: "2026-04-21T10:00:01Z" },
              ],
            },
          ],
        })}
      />,
    );

    expect(screen.getByText("gateway.request")).toBeInTheDocument();
    expect(screen.getAllByText("router.selected").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Default Model local first failover").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Recovered via half-open provider probe").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Recovery probe").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Circuit open").length).toBeGreaterThan(0);
    expect(screen.getByText("Route decision tree")).toBeInTheDocument();
    expect(screen.getByText("Failover chain")).toBeInTheDocument();
    expect(screen.getByText("Semantic cache")).toBeInTheDocument();
    expect(screen.getAllByText("Semantic lookup miss").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Writeback stored").length).toBeGreaterThan(0);
    expect(screen.getByText("route_denied")).toBeInTheDocument();
    expect(screen.getByText("Provider Retry Exhausted")).toBeInTheDocument();
    expect(screen.getByText("Provider path")).toBeInTheDocument();
  });
});
