import type { ConfiguredProviderRecord, ProviderPresetRecord } from "../types/runtime";

export function resolvedBaseURL(
  name: string,
  cp?: ConfiguredProviderRecord,
  presets?: ProviderPresetRecord[],
): string {
  if (cp?.base_url) return cp.base_url;
  return presets?.find(p => p.id === name)?.base_url ?? "";
}

export function buildConflictMap(
  names: string[],
  configuredByName: Map<string, ConfiguredProviderRecord>,
  presets: ProviderPresetRecord[],
): Map<string, string[]> {
  const urlToNames = new Map<string, string[]>();
  for (const name of names) {
    const url = resolvedBaseURL(name, configuredByName.get(name), presets);
    if (!url) continue;
    const list = urlToNames.get(url) ?? [];
    list.push(name);
    urlToNames.set(url, list);
  }
  const conflictMap = new Map<string, string[]>();
  for (const group of urlToNames.values()) {
    if (group.length > 1) {
      for (const name of group) {
        conflictMap.set(name, group.filter(n => n !== name));
      }
    }
  }
  return conflictMap;
}

export function providerDotColor(enabled: boolean, healthy: boolean): "green" | "amber" | "red" {
  if (!enabled) return "red";
  if (healthy) return "green";
  return "amber";
}
