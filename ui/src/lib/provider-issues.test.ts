import { describe, expect, it } from "vitest";

import { buildLocalProviderIssue } from "./provider-issues";

describe("buildLocalProviderIssue", () => {
  it("returns an ollama pull hint when the default local model is missing", () => {
    const issue = buildLocalProviderIssue({
      name: "ollama",
      kind: "local",
      healthy: true,
      status: "healthy",
      default_model: "llama3.1:8b",
      models: ["qwen2.5:7b"],
    });

    expect(issue).toEqual(
      expect.objectContaining({
        provider: "ollama",
        model: "llama3.1:8b",
        command: "ollama pull llama3.1:8b",
      }),
    );
  });

  it("returns null when the default model is already present", () => {
    const issue = buildLocalProviderIssue({
      name: "ollama",
      kind: "local",
      healthy: true,
      status: "healthy",
      default_model: "llama3.1:8b",
      models: ["llama3.1:8b"],
    });

    expect(issue).toBeNull();
  });
});
