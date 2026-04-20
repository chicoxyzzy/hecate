import { useMemo, useState } from "react";

import { Panel } from "./Panel";
import { SegmentedTabs } from "./SegmentedTabs";
import type { ModelFilter, ModelRecord } from "../types/runtime";

type ModelsPanelProps = {
  localModels: ModelRecord[];
  modelFilter: ModelFilter;
  visibleModels: ModelRecord[];
  onModelFilterChange: (value: ModelFilter) => void;
};

export function ModelsPanel(props: ModelsPanelProps) {
  const [search, setSearch] = useState("");
  const normalizedSearch = search.trim().toLowerCase();
  const filteredModels = useMemo(() => {
    const sorted = [...props.visibleModels].sort((left, right) => {
      const leftDefault = left.metadata?.default ? 0 : 1;
      const rightDefault = right.metadata?.default ? 0 : 1;
      if (leftDefault !== rightDefault) {
        return leftDefault - rightDefault;
      }

      const leftProvider = left.metadata?.provider ?? "unknown";
      const rightProvider = right.metadata?.provider ?? "unknown";
      if (leftProvider !== rightProvider) {
        return leftProvider.localeCompare(rightProvider);
      }
      return left.id.localeCompare(right.id);
    });

    if (normalizedSearch === "") {
      return sorted;
    }

    return sorted.filter((entry) => {
      const provider = entry.metadata?.provider ?? "";
      const kind = entry.metadata?.provider_kind ?? "";
      const discoverySource = entry.metadata?.discovery_source ?? "";
      return [entry.id, provider, kind, discoverySource].some((value) => value.toLowerCase().includes(normalizedSearch));
    });
  }, [normalizedSearch, props.visibleModels]);

  const defaultModels = filteredModels.filter((entry) => entry.metadata?.default);
  const groupedModels = filteredModels.reduce<Record<string, ModelRecord[]>>((groups, entry) => {
    const provider = entry.metadata?.provider ?? "unknown";
    if (!groups[provider]) {
      groups[provider] = [];
    }
    groups[provider].push(entry);
    return groups;
  }, {});
  const providerNames = Object.keys(groupedModels).sort((left, right) => left.localeCompare(right));

  return (
    <Panel eyebrow="Models" title="Discovered catalog">
      <div className="mt-4 grid gap-3">
        <SegmentedTabs
          tabs={[
            { id: "all", label: "All" },
            { id: "cloud", label: "Cloud" },
            { id: "local", label: "Local" },
          ]}
          value={props.modelFilter}
          onChange={props.onModelFilterChange}
        />

        <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_auto] md:items-center">
          <label className="block">
            <span className="mb-2 block text-sm text-slate-600">Search models, providers, or discovery source</span>
            <input
              className="w-full rounded-2xl border border-slate-200/80 bg-white/90 px-4 py-3 text-slate-900 outline-none transition focus:border-cyan-700 focus:ring-4 focus:ring-cyan-100"
              placeholder="gpt-4o-mini, ollama, upstream_v1_models..."
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <div className="rounded-2xl bg-slate-50/90 px-4 py-3 text-sm text-slate-600">
            {filteredModels.length} shown · {defaultModels.length} defaults · {providerNames.length} providers
          </div>
        </div>
      </div>

      {props.localModels.length === 0 ? (
        <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
          No local models are currently registered. In your current runtime config, this usually means the local provider is not enabled.
          Check `LOCAL_PROVIDER_ENABLED=true` and confirm a local provider base URL and model list are configured.
        </div>
      ) : null}

      {defaultModels.length > 0 ? (
        <section className="mt-4">
          <div className="mb-3 flex items-center justify-between gap-3">
            <h3 className="text-sm font-semibold uppercase tracking-[0.16em] text-slate-500">Default models</h3>
            <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800">{defaultModels.length}</span>
          </div>
          <div className="grid gap-3">
            {defaultModels.map((entry) => (
              <ModelCard entry={entry} key={`default-${entry.metadata?.provider}-${entry.id}`} />
            ))}
          </div>
        </section>
      ) : null}

      <div className="mt-4 grid gap-3">
        {providerNames.map((providerName) => {
          const providerModels = groupedModels[providerName] ?? [];
          const nonDefaultProviderModels = providerModels.filter((entry) => !entry.metadata?.default);
          if (nonDefaultProviderModels.length === 0) {
            return null;
          }

          const providerKind = providerModels[0]?.metadata?.provider_kind ?? "unknown";
          const startsOpen = normalizedSearch !== "" || providerNames.length === 1;

          return (
            <details
              className="rounded-2xl border border-slate-200/80 bg-slate-50/90 p-4 open:bg-white/90"
              key={providerName}
              open={startsOpen}
            >
              <summary className="flex cursor-pointer list-none items-center justify-between gap-3">
                <div>
                  <strong className="text-slate-900">{providerName}</strong>
                  <p className="mt-1 text-sm text-slate-500">
                    {providerKind} · {nonDefaultProviderModels.length} additional model{nonDefaultProviderModels.length === 1 ? "" : "s"}
                  </p>
                </div>
                <span className="rounded-full bg-slate-200 px-2.5 py-1 text-xs font-medium text-slate-700">{nonDefaultProviderModels.length}</span>
              </summary>
              <div className="mt-4 grid gap-3">
                {nonDefaultProviderModels.map((entry) => (
                  <ModelCard entry={entry} key={`${entry.metadata?.provider}-${entry.id}`} />
                ))}
              </div>
            </details>
          );
        })}
      </div>

      {filteredModels.length === 0 ? <p className="mt-3 text-sm text-slate-500">No models matched the current filter or search.</p> : null}
    </Panel>
  );
}

function ModelCard(props: { entry: ModelRecord }) {
  const { entry } = props;

  return (
    <article className="rounded-2xl bg-slate-50/90 p-4">
      <div className="flex items-center justify-between gap-3">
        <strong className="break-all">{entry.id}</strong>
        <div className="flex flex-wrap gap-2">
          {entry.metadata?.default ? (
            <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-700">default</span>
          ) : null}
          <span className="rounded-full bg-slate-200 px-2.5 py-1 text-xs font-medium text-slate-700">{entry.metadata?.provider_kind ?? "unknown"}</span>
        </div>
      </div>
      <p className="mt-1 text-sm text-slate-500">
        {entry.metadata?.provider ?? "unknown"} · {entry.metadata?.discovery_source ?? "n/a"}
      </p>
    </article>
  );
}
