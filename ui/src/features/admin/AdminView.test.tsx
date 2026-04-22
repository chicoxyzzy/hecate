import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AdminView } from "./AdminView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

describe("AdminView", () => {
  it("shows budget warnings and lifecycle history", () => {
    render(
      <AdminView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          budget: {
            key: "global:tenant:team-a:provider:ollama",
            scope: "tenant_provider",
            provider: "ollama",
            tenant: "team-a",
            backend: "memory",
            limit_source: "store",
            spent_micros_usd: 1_850_000,
            spent_usd: "1.850000",
            current_micros_usd: 1_850_000,
            current_usd: "1.850000",
            max_micros_usd: 2_000_000,
            max_usd: "2.000000",
            remaining_micros_usd: 150_000,
            remaining_usd: "0.150000",
            enforced: true,
            warnings: [
              {
                threshold_percent: 50,
                threshold_micros_usd: 1_000_000,
                current_micros_usd: 1_850_000,
                remaining_micros_usd: 150_000,
                triggered: true,
              },
            ],
            history: [
              {
                type: "usage",
                scope: "tenant_provider",
                provider: "ollama",
                tenant: "team-a",
                model: "llama3.1:8b",
                request_id: "req-123",
                amount_micros_usd: 1_850_000,
                amount_usd: "1.850000",
                balance_micros_usd: 1_850_000,
                balance_usd: "1.850000",
                limit_micros_usd: 2_000_000,
                limit_usd: "2.000000",
                timestamp: "2026-04-21T10:00:00Z",
              },
            ],
          },
        })}
      />,
    );

    expect(screen.getByText("Warning thresholds")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText("Budget history")).toBeInTheDocument();
    expect(screen.getByText("Usage")).toBeInTheDocument();
    expect(screen.getByText("req-123")).toBeInTheDocument();
  });

  it("shows retention run results and recent session history", () => {
    render(
      <AdminView
        actions={createRuntimeConsoleActions()}
        state={createRuntimeConsoleFixture({
          retentionLastRun: {
            started_at: "2026-04-22T10:00:00Z",
            finished_at: "2026-04-22T10:00:05Z",
            trigger: "manual",
            results: [
              { name: "trace_snapshots", deleted: 12, max_age: "24h", max_count: 2000 },
              { name: "semantic_cache", deleted: 3, max_age: "168h", max_count: 10000 },
            ],
          },
          retentionRuns: [
            {
              started_at: "2026-04-22T10:00:00Z",
              finished_at: "2026-04-22T10:00:05Z",
              trigger: "manual",
              results: [
                { name: "trace_snapshots", deleted: 12, max_age: "24h", max_count: 2000 },
                { name: "semantic_cache", deleted: 3, max_age: "168h", max_count: 10000 },
              ],
            },
          ],
        })}
      />,
    );

    expect(screen.getAllByText("Retention").length).toBeGreaterThan(0);
    expect(screen.getByText("Run retention")).toBeInTheDocument();
    expect(screen.getByText("Recent retention runs")).toBeInTheDocument();
    expect(screen.getAllByText("manual").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/trace_snapshots/i).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/semantic_cache/i).length).toBeGreaterThan(0);
  });
});
