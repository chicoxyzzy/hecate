import { Panel } from "./Panel";
import type { ProviderRecord } from "../types/runtime";

type ProvidersPanelProps = {
  copiedCommand: string;
  providers: ProviderRecord[];
  onCopyCommand: (command: string) => void | Promise<void>;
};

export function ProvidersPanel(props: ProvidersPanelProps) {
  return (
    <Panel eyebrow="Providers" title="Upstream health">
      <div className="mt-4 grid gap-3">
        {props.providers.map((provider) => (
          <article className="rounded-2xl bg-slate-50/90 p-4" key={provider.name}>
            <div className="flex items-center justify-between gap-3">
              <strong>{provider.name}</strong>
              <span
                className={
                  provider.healthy
                    ? "rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-medium text-emerald-700"
                    : "rounded-full bg-red-100 px-2.5 py-1 text-xs font-medium text-red-700"
                }
              >
                {provider.status}
              </span>
            </div>
            <p className="mt-1 text-sm text-slate-500">
              {provider.kind} · default {provider.default_model ?? "n/a"} · {provider.models?.length ?? 0} models
            </p>
            {provider.discovery_source ? <p className="mt-1 text-xs text-slate-500">Discovery: {provider.discovery_source}</p> : null}
            {provider.error ? <p className="mt-1 text-sm text-red-700">{provider.error}</p> : null}
            {provider.kind === "local" && provider.default_model && !provider.models?.includes(provider.default_model) ? (
              <div className="mt-3 rounded-2xl border border-amber-200 bg-amber-50 px-3 py-3 text-sm text-amber-900">
                <p>
                  Default model <span className="font-mono">{provider.default_model}</span> is configured but not currently reported by this provider.
                </p>
                {provider.name === "ollama" ? (
                  <div className="mt-2 rounded-xl bg-slate-950 px-3 py-2 text-slate-100">
                    <div className="flex items-center justify-between gap-3">
                      <code className="overflow-x-auto">{`ollama pull ${provider.default_model}`}</code>
                      <button
                        className="shrink-0 rounded-full border border-slate-700 bg-slate-900 px-3 py-1.5 text-xs font-medium text-slate-100 transition hover:bg-slate-800"
                        onClick={() => void props.onCopyCommand(`ollama pull ${provider.default_model}`)}
                        type="button"
                      >
                        {props.copiedCommand === `ollama pull ${provider.default_model}` ? "Copied" : "Copy"}
                      </button>
                    </div>
                  </div>
                ) : null}
              </div>
            ) : null}
          </article>
        ))}
      </div>
    </Panel>
  );
}
