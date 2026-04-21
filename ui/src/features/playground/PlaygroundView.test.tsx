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
          traceSpans: [
            {
              trace_id: "trace-1",
              span_id: "span-1",
              name: "gateway.request",
              status_code: "ok",
              events: [{ name: "router.selected", timestamp: "2026-04-21T10:00:00Z" }],
            },
          ],
        })}
      />,
    );

    expect(screen.getByText("gateway.request")).toBeInTheDocument();
    expect(screen.getByText("router.selected")).toBeInTheDocument();
  });
});
