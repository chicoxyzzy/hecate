import type { SyntheticEvent } from "react";

import { KV } from "./KV";
import { Panel } from "./Panel";
import type {
  ChatResponse,
  ModelRecord,
  ProviderFilter,
  ProviderRecord,
  RuntimeHeaders,
} from "../types/runtime";

type PlaygroundPanelProps = {
  chatError: string;
  chatLoading: boolean;
  chatResult: ChatResponse | null;
  cloudModels: ModelRecord[];
  cloudProviders: ProviderRecord[];
  inputClassName: string;
  localModels: ModelRecord[];
  localProviders: ProviderRecord[];
  message: string;
  model: string;
  providerFilter: ProviderFilter;
  providerScopedModels: ModelRecord[];
  runtimeHeaders: RuntimeHeaders | null;
  tenant: string;
  onMessageChange: (value: string) => void;
  onModelChange: (value: string) => void;
  onProviderFilterChange: (value: string) => void;
  onSubmit: (event: SyntheticEvent<HTMLFormElement>) => void | Promise<void>;
  onTenantChange: (value: string) => void;
};

export function PlaygroundPanel(props: PlaygroundPanelProps) {
  return (
    <Panel eyebrow="Playground" title="Send a request" className="xl:row-span-2">
      <form className="mt-5 grid gap-4 md:grid-cols-2" onSubmit={props.onSubmit}>
        <label>
          <span className="mb-2 block text-sm text-slate-600">Provider</span>
          <select
            className={props.inputClassName}
            value={props.providerFilter}
            onChange={(event) => props.onProviderFilterChange(event.target.value)}
          >
            <option value="auto">Auto route</option>
            {props.cloudProviders.length > 0 ? (
              <optgroup label="Cloud">
                {props.cloudProviders.map((provider) => (
                  <option key={provider.name} value={provider.name}>
                    {provider.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
            {props.localProviders.length > 0 ? (
              <optgroup label="Local">
                {props.localProviders.map((provider) => (
                  <option key={provider.name} value={provider.name}>
                    {provider.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
          </select>
        </label>

        <label>
          <span className="mb-2 block text-sm text-slate-600">Model</span>
          <select className={props.inputClassName} value={props.model} onChange={(event) => props.onModelChange(event.target.value)}>
            {props.providerFilter === "auto" ? (
              <>
                {props.cloudModels.length > 0 ? (
                  <optgroup label="Cloud">
                    {props.cloudModels.map((entry) => (
                      <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                        {entry.id} · {entry.metadata?.provider}
                      </option>
                    ))}
                  </optgroup>
                ) : null}
                {props.localModels.length > 0 ? (
                  <optgroup label="Local">
                    {props.localModels.map((entry) => (
                      <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                        {entry.id} · {entry.metadata?.provider}
                      </option>
                    ))}
                  </optgroup>
                ) : null}
              </>
            ) : (
              props.providerScopedModels.map((entry) => (
                <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                  {entry.id}
                </option>
              ))
            )}
            {props.providerScopedModels.length === 0 ? <option value="">No models available</option> : null}
          </select>
        </label>

        <label>
          <span className="mb-2 block text-sm text-slate-600">Tenant</span>
          <input className={props.inputClassName} value={props.tenant} onChange={(event) => props.onTenantChange(event.target.value)} />
        </label>

        <label className="md:col-span-2">
          <span className="mb-2 block text-sm text-slate-600">Prompt</span>
          <textarea
            className={`${props.inputClassName} min-h-32 resize-y`}
            rows={5}
            value={props.message}
            onChange={(event) => props.onMessageChange(event.target.value)}
          />
        </label>

        <div className="flex items-center justify-between gap-3 md:col-span-2">
          <button
            className="inline-flex rounded-full bg-gradient-to-br from-cyan-700 to-cyan-900 px-5 py-3 text-sm font-semibold text-white shadow-[0_12px_24px_rgba(5,92,103,0.24)] transition hover:-translate-y-0.5 disabled:cursor-not-allowed disabled:opacity-60"
            disabled={props.chatLoading}
            type="submit"
          >
            {props.chatLoading ? "Running..." : "Run Chat Completion"}
          </button>
        </div>
      </form>

      {props.chatError ? (
        <div className="mt-4 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{props.chatError}</div>
      ) : null}

      <div className="mt-5 grid gap-4 lg:grid-cols-2">
        <article className="rounded-3xl bg-slate-50/90 p-5">
          <h3 className="mb-3 text-lg font-semibold text-slate-900">Assistant Response</h3>
          <pre className="whitespace-pre-wrap font-mono text-sm text-sky-950">
            {props.chatResult?.choices?.[0]?.message?.content ?? "No response yet."}
          </pre>
        </article>

        <article className="rounded-3xl bg-slate-50/90 p-5">
          <h3 className="mb-3 text-lg font-semibold text-slate-900">Runtime Headers</h3>
          <dl>
            <KV label="Request ID" value={props.runtimeHeaders?.requestId} />
            <KV label="Provider" value={props.runtimeHeaders?.provider} />
            <KV label="Kind" value={props.runtimeHeaders?.providerKind} />
            <KV label="Route Reason" value={props.runtimeHeaders?.routeReason} />
            <KV label="Requested Model" value={props.runtimeHeaders?.requestedModel} />
            <KV label="Resolved Model" value={props.runtimeHeaders?.resolvedModel} />
            <KV label="Cache Hit" value={props.runtimeHeaders?.cache} />
            <KV label="Cost USD" value={props.runtimeHeaders?.costUsd} />
          </dl>
        </article>
      </div>
    </Panel>
  );
}
