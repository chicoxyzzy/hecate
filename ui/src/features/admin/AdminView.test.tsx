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
          accountSummary: {
            account: {
              key: "global:tenant:team-a:provider:ollama",
              scope: "tenant_provider",
              provider: "ollama",
              tenant: "team-a",
              backend: "memory",
              balance_source: "store",
              debited_micros_usd: 1_850_000,
              debited_usd: "1.850000",
              credited_micros_usd: 2_000_000,
              credited_usd: "2.000000",
              balance_micros_usd: 150_000,
              balance_usd: "0.150000",
              available_micros_usd: 150_000,
              available_usd: "0.150000",
              enforced: true,
            },
            estimates: [
              {
                provider: "ollama",
                provider_kind: "local",
                model: "llama3.1:8b",
                priced: false,
                input_micros_usd_per_million_tokens: 0,
                output_micros_usd_per_million_tokens: 0,
                estimated_remaining_prompt_tokens: 0,
                estimated_remaining_output_tokens: 0,
              },
            ],
          },
          requestLedger: [
            {
              type: "debit",
              scope: "tenant_provider",
              provider: "ollama",
              tenant: "team-a",
              model: "llama3.1:8b",
              request_id: "req-123",
              amount_micros_usd: 1_850_000,
              amount_usd: "1.850000",
              balance_micros_usd: 150_000,
              balance_usd: "0.150000",
              credited_micros_usd: 2_000_000,
              credited_usd: "2.000000",
              debited_micros_usd: 1_850_000,
              debited_usd: "1.850000",
              prompt_tokens: 100,
              completion_tokens: 25,
              total_tokens: 125,
              timestamp: "2026-04-21T10:00:00Z",
            },
          ],
          budget: {
            key: "global:tenant:team-a:provider:ollama",
            scope: "tenant_provider",
            provider: "ollama",
            tenant: "team-a",
            backend: "memory",
            balance_source: "store",
            debited_micros_usd: 1_850_000,
            debited_usd: "1.850000",
            credited_micros_usd: 2_000_000,
            credited_usd: "2.000000",
            balance_micros_usd: 150_000,
            balance_usd: "0.150000",
            available_micros_usd: 150_000,
            available_usd: "0.150000",
            enforced: true,
            warnings: [
              {
                threshold_percent: 50,
                threshold_micros_usd: 1_000_000,
                balance_micros_usd: 150_000,
                available_micros_usd: 150_000,
                triggered: true,
              },
            ],
            history: [
              {
                type: "debit",
                scope: "tenant_provider",
                provider: "ollama",
                tenant: "team-a",
                model: "llama3.1:8b",
                request_id: "req-123",
                amount_micros_usd: 1_850_000,
                amount_usd: "1.850000",
                balance_micros_usd: 150_000,
                balance_usd: "0.150000",
                credited_micros_usd: 2_000_000,
                credited_usd: "2.000000",
                debited_micros_usd: 1_850_000,
                debited_usd: "1.850000",
                prompt_tokens: 100,
                completion_tokens: 25,
                total_tokens: 125,
                timestamp: "2026-04-21T10:00:00Z",
              },
            ],
          },
        })}
      />,
    );

    expect(screen.getByText("Warning thresholds")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    expect(screen.getByText("Recent request debits")).toBeInTheDocument();
    expect(screen.getByText("Account ledger")).toBeInTheDocument();
    expect(screen.getByText("Model balance estimates")).toBeInTheDocument();
    expect(screen.getByText("Debit")).toBeInTheDocument();
    expect(screen.getAllByText("req-123").length).toBeGreaterThan(0);
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
