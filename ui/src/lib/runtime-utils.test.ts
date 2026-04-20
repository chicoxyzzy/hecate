import { describe, expect, it } from "vitest";

import { filterModelsByKind, filterModelsByProvider, parseCSV, usdToMicros } from "./runtime-utils";
import type { ModelRecord } from "../types/runtime";

const models: ModelRecord[] = [
  { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } },
  { id: "llama3.1:8b", owned_by: "ollama", metadata: { provider: "ollama", provider_kind: "local" } },
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
});
