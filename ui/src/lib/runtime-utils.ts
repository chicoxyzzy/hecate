import type { ModelFilter, ModelRecord, ProviderFilter } from "../types/runtime";

export function usdToMicros(value: string): number {
  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return Number.NaN;
  }
  return Math.round(parsed * 1_000_000);
}

export function parseCSV(value: string): string[] {
  return value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

export function filterModelsByKind(models: ModelRecord[], filter: ModelFilter): ModelRecord[] {
  switch (filter) {
    case "local":
      return models.filter((entry) => entry.metadata?.provider_kind === "local");
    case "cloud":
      return models.filter((entry) => entry.metadata?.provider_kind === "cloud");
    default:
      return models;
  }
}

export function filterModelsByProvider(models: ModelRecord[], provider: ProviderFilter): ModelRecord[] {
  if (provider === "auto") {
    return models;
  }
  return models.filter((entry) => entry.metadata?.provider === provider);
}
