import { Panel } from "./Panel";
import type { ModelFilter, ModelRecord } from "../types/runtime";

type ModelsPanelProps = {
  localModels: ModelRecord[];
  modelFilter: ModelFilter;
  visibleModels: ModelRecord[];
  onModelFilterChange: (value: ModelFilter) => void;
};

export function ModelsPanel(props: ModelsPanelProps) {
  return (
    <Panel eyebrow="Models" title="Discovered catalog">
      <div className="mt-4 inline-flex gap-1 rounded-full bg-slate-200/80 p-1">
        {(["all", "cloud", "local"] as const).map((filter) => (
          <button
            className={
              props.modelFilter === filter
                ? "rounded-full bg-white px-3 py-2 text-sm font-medium text-slate-900 shadow"
                : "rounded-full px-3 py-2 text-sm text-slate-600"
            }
            key={filter}
            onClick={() => props.onModelFilterChange(filter)}
            type="button"
          >
            {filter === "all" ? "All" : filter[0].toUpperCase() + filter.slice(1)}
          </button>
        ))}
      </div>

      {props.localModels.length === 0 ? (
        <div className="mt-4 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800">
          No local models are currently registered. In your current runtime config, this usually means the local provider is not enabled.
          Check `LOCAL_PROVIDER_ENABLED=true` and confirm a local provider base URL and model list are configured.
        </div>
      ) : null}

      <div className="mt-4 grid gap-3">
        {props.visibleModels.map((entry) => (
          <article className="rounded-2xl bg-slate-50/90 p-4" key={`${entry.metadata?.provider}-${entry.id}`}>
            <div className="flex items-center justify-between gap-3">
              <strong>{entry.id}</strong>
              <div className="flex flex-wrap gap-2">
                {entry.metadata?.default ? (
                  <span className="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-700">default</span>
                ) : null}
                <span className="rounded-full bg-slate-200 px-2.5 py-1 text-xs font-medium text-slate-700">
                  {entry.metadata?.provider_kind ?? "unknown"}
                </span>
              </div>
            </div>
            <p className="mt-1 text-sm text-slate-500">
              {entry.metadata?.provider ?? "unknown"} · {entry.metadata?.discovery_source ?? "n/a"}
            </p>
          </article>
        ))}
      </div>

      {props.visibleModels.length === 0 ? <p className="mt-3 text-sm text-slate-500">No models matched the current filter.</p> : null}
    </Panel>
  );
}
