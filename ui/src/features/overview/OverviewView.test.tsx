import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { OverviewView } from "./OverviewView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

describe("OverviewView", () => {
  it("shows the latest request summary when runtime headers are present", () => {
    render(
      <OverviewView
        actions={createRuntimeConsoleActions()}
        onOpenWorkspace={vi.fn()}
        state={createRuntimeConsoleFixture({
          providers: [{ name: "openai", kind: "cloud", healthy: true, status: "healthy" }],
          healthyProviders: 1,
          runtimeHeaders: {
            requestId: "req-123",
            traceId: "trace-123",
            spanId: "span-123",
            provider: "openai",
            providerKind: "cloud",
            routeReason: "explicit_or_default",
            requestedModel: "gpt-4o-mini",
            resolvedModel: "gpt-4o-mini-2024-07-18",
            cache: "false",
            cacheType: "miss",
            semanticStrategy: "",
            semanticIndex: "",
            semanticSimilarity: "",
            attempts: "1",
            retries: "0",
            fallbackFrom: "",
            costUsd: "0.000123",
          },
          chatResult: {
            id: "chatcmpl-1",
            model: "gpt-4o-mini-2024-07-18",
            choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "Hello from Hecate." } }],
            usage: { prompt_tokens: 10, completion_tokens: 4, total_tokens: 14 },
          },
        })}
      />,
    );

    expect(screen.getByText("req-123")).toBeInTheDocument();
    expect(screen.getByText("Hello from Hecate.")).toBeInTheDocument();
    expect(screen.getByText(/Estimated cost/i)).toBeInTheDocument();
  });
});
