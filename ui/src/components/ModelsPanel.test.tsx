import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ModelsPanel } from "./ModelsPanel";

describe("ModelsPanel", () => {
  it("surfaces defaults first and filters by search", () => {
    render(
      <ModelsPanel
        localModels={[
          {
            id: "llama3.1:8b",
            owned_by: "ollama",
            metadata: { provider: "ollama", provider_kind: "local", default: true, discovery_source: "upstream_v1_models" },
          },
        ]}
        modelFilter="all"
        visibleModels={[
          {
            id: "gpt-4o-mini",
            owned_by: "openai",
            metadata: { provider: "openai", provider_kind: "cloud", default: true, discovery_source: "upstream_v1_models" },
          },
          {
            id: "gpt-4.1",
            owned_by: "openai",
            metadata: { provider: "openai", provider_kind: "cloud", discovery_source: "upstream_v1_models" },
          },
          {
            id: "llama3.1:8b",
            owned_by: "ollama",
            metadata: { provider: "ollama", provider_kind: "local", default: true, discovery_source: "upstream_v1_models" },
          },
        ]}
        onModelFilterChange={vi.fn()}
      />,
    );

    expect(screen.getByText("Default models")).toBeInTheDocument();
    expect(screen.getByText("gpt-4o-mini")).toBeInTheDocument();
    expect(screen.getByText("llama3.1:8b")).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText("gpt-4o-mini, ollama, upstream_v1_models..."), {
      target: { value: "ollama" },
    });

    expect(screen.getByText("llama3.1:8b")).toBeInTheDocument();
    expect(screen.queryByText("gpt-4.1")).not.toBeInTheDocument();
  });
});
