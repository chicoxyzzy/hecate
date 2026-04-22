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
      <div className="stack-md">
        <SegmentedTabs
          tabs={[
            { id: "all", label: "All" },
            { id: "cloud", label: "Cloud" },
            { id: "local", label: "Local" },
          ]}
          value={props.modelFilter}
          onChange={props.onModelFilterChange}
        />

        <div className="model-search-bar">
          <label className="field">
            <span className="field__label">Search models, providers, or discovery source</span>
            <input
              className="field__input"
              placeholder="gpt-4o-mini, ollama, upstream_v1_models..."
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
          </label>
          <div className="model-search-stats">
            {filteredModels.length} shown · {defaultModels.length} defaults · {providerNames.length} providers
          </div>
        </div>
      </div>

      {props.localModels.length === 0 ? (
        <div className="model-no-local" style={{ marginTop: "1rem" }}>
          No local models are currently registered. In your current runtime config, this usually means the local provider is not enabled.
          Check `LOCAL_PROVIDER_ENABLED=true` and confirm a local provider base URL and model list are configured.
        </div>
      ) : null}

      {defaultModels.length > 0 ? (
        <section style={{ marginTop: "1rem" }}>
          <div className="console-section__header" style={{ marginBottom: "0.75rem" }}>
            <h3 className="label-muted">Default models</h3>
            <span className="model-count model-count--default">{defaultModels.length}</span>
          </div>
          <div className="stack-sm">
            {defaultModels.map((entry) => (
              <ModelCard entry={entry} key={`default-${entry.metadata?.provider}-${entry.id}`} />
            ))}
          </div>
        </section>
      ) : null}

      <div className="stack-sm" style={{ marginTop: "1rem" }}>
        {providerNames.map((providerName) => {
          const providerModels = groupedModels[providerName] ?? [];
          const nonDefaultProviderModels = providerModels.filter((entry) => !entry.metadata?.default);
          if (nonDefaultProviderModels.length === 0) {
            return null;
          }

          const providerKind = providerModels[0]?.metadata?.provider_kind ?? "unknown";
          const startsOpen = normalizedSearch !== "" || providerNames.length === 1;

          return (
            <details className="model-provider-group" key={providerName} open={startsOpen}>
              <summary className="model-provider-group__summary">
                <div>
                  <strong className="model-provider-group__name">{providerName}</strong>
                  <p className="model-provider-group__meta">
                    {providerKind} · {nonDefaultProviderModels.length} additional model{nonDefaultProviderModels.length === 1 ? "" : "s"}
                  </p>
                </div>
                <span className="model-count model-count--neutral">{nonDefaultProviderModels.length}</span>
              </summary>
              <div className="stack-sm" style={{ marginTop: "1rem" }}>
                {nonDefaultProviderModels.map((entry) => (
                  <ModelCard entry={entry} key={`${entry.metadata?.provider}-${entry.id}`} />
                ))}
              </div>
            </details>
          );
        })}
      </div>

      {filteredModels.length === 0 ? <p className="body-muted" style={{ marginTop: "0.75rem" }}>No models matched the current filter or search.</p> : null}
    </Panel>
  );
}

function ModelCard(props: { entry: ModelRecord }) {
  const { entry } = props;

  return (
    <article className="model-card">
      <div className="model-card__head">
        <strong className="break-all">{entry.id}</strong>
        <div className="model-card__badges">
          {entry.metadata?.default ? (
            <span className="model-count model-count--default">default</span>
          ) : null}
          <span className="model-count model-count--neutral">{entry.metadata?.provider_kind ?? "unknown"}</span>
        </div>
      </div>
      <p className="model-card__meta">
        {entry.metadata?.provider ?? "unknown"} · {entry.metadata?.discovery_source ?? "n/a"}
      </p>
    </article>
  );
}
