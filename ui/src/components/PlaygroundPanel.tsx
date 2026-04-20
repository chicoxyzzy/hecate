import { useMemo, useState, type SyntheticEvent } from "react";

import { KV } from "./KV";
import { Panel } from "./Panel";
import { SegmentedTabs } from "./SegmentedTabs";
import { SessionRestrictions } from "./SessionRestrictions";
import type {
  ChatResponse,
  ModelRecord,
  ProviderFilter,
  ProviderRecord,
  RuntimeHeaders,
} from "../types/runtime";

type PlaygroundPanelProps = {
  allowedModels: string[];
  allowedProviders: string[];
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
  tenantLocked: boolean;
  tenant: string;
  onMessageChange: (value: string) => void;
  onModelChange: (value: string) => void;
  onProviderFilterChange: (value: string) => void;
  onSubmit: (event: SyntheticEvent<HTMLFormElement>) => void | Promise<void>;
  onTenantChange: (value: string) => void;
};

export function PlaygroundPanel(props: PlaygroundPanelProps) {
  const [resultView, setResultView] = useState<"response" | "runtime" | "usage">("response");
  const availableCloudProviders = filterAllowedProviders(props.cloudProviders, props.allowedProviders);
  const availableLocalProviders = filterAllowedProviders(props.localProviders, props.allowedProviders);
  const availableCloudModels = filterAllowedModels(props.cloudModels, props.allowedModels);
  const availableLocalModels = filterAllowedModels(props.localModels, props.allowedModels);
  const availableScopedModels = filterAllowedModels(props.providerScopedModels, props.allowedModels);
  const selectedProviderLabel = props.providerFilter === "auto" ? "Auto route" : props.providerFilter;
  const selectedModelLabel = props.model || "No model selected";
  const usageItems = useMemo(
    () => [
      { label: "Prompt Tokens", value: props.chatResult?.usage?.prompt_tokens?.toString() },
      { label: "Completion Tokens", value: props.chatResult?.usage?.completion_tokens?.toString() },
      { label: "Total Tokens", value: props.chatResult?.usage?.total_tokens?.toString() },
      { label: "Cost USD", value: props.runtimeHeaders?.costUsd },
      { label: "Cache Hit", value: props.runtimeHeaders?.cache },
    ],
    [props.chatResult?.usage?.completion_tokens, props.chatResult?.usage?.prompt_tokens, props.chatResult?.usage?.total_tokens, props.runtimeHeaders?.cache, props.runtimeHeaders?.costUsd],
  );

  return (
    <Panel eyebrow="Playground" title="Send a request" className="xl:row-span-2">
      <form className="mt-5 grid gap-4 md:grid-cols-2" onSubmit={props.onSubmit}>
        <div className="md:col-span-2 grid gap-3 rounded-2xl bg-slate-50/90 p-4 md:grid-cols-3">
          <SummaryPill label="Provider" value={selectedProviderLabel} />
          <SummaryPill label="Model" value={selectedModelLabel} />
          <SummaryPill label="Tenant" value={props.tenantLocked ? `${props.tenant} (enforced)` : props.tenant || "none"} />
        </div>

        {props.tenantLocked ? (
          <div className="md:col-span-2 rounded-2xl border border-cyan-200 bg-cyan-50 px-4 py-3 text-sm text-cyan-900">
            <span className="font-semibold">Tenant enforced:</span> {props.tenant}
          </div>
        ) : null}

        <SessionRestrictions
          allowedModels={props.allowedModels}
          allowedProviders={props.allowedProviders}
          className="md:col-span-2 grid gap-3 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-4 text-sm text-amber-950"
        />

        <label>
          <span className="mb-2 block text-sm text-slate-600">Provider</span>
          <select
            className={props.inputClassName}
            value={props.providerFilter}
            onChange={(event) => props.onProviderFilterChange(event.target.value)}
          >
            <option value="auto">Auto route</option>
            {availableCloudProviders.length > 0 ? (
              <optgroup label="Cloud">
                {availableCloudProviders.map((provider) => (
                  <option key={provider.name} value={provider.name}>
                    {provider.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
            {availableLocalProviders.length > 0 ? (
              <optgroup label="Local">
                {availableLocalProviders.map((provider) => (
                  <option key={provider.name} value={provider.name}>
                    {provider.name}
                  </option>
                ))}
              </optgroup>
            ) : null}
          </select>
          <p className="mt-2 text-xs text-slate-500">
            {props.allowedProviders.length > 0
              ? "Only providers allowed by the active session are shown."
              : "Auto route lets the gateway choose the provider."}
          </p>
        </label>

        <label>
          <span className="mb-2 block text-sm text-slate-600">Model</span>
          <select className={props.inputClassName} value={props.model} onChange={(event) => props.onModelChange(event.target.value)}>
            {props.providerFilter === "auto" ? (
              <>
                {availableCloudModels.length > 0 ? (
                  <optgroup label="Cloud">
                    {availableCloudModels.map((entry) => (
                      <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                        {entry.id} · {entry.metadata?.provider}
                      </option>
                    ))}
                  </optgroup>
                ) : null}
                {availableLocalModels.length > 0 ? (
                  <optgroup label="Local">
                    {availableLocalModels.map((entry) => (
                      <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                        {entry.id} · {entry.metadata?.provider}
                      </option>
                    ))}
                  </optgroup>
                ) : null}
              </>
            ) : (
              availableScopedModels.map((entry) => (
                <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                  {entry.id}
                </option>
              ))
            )}
            {availableScopedModels.length === 0 && props.providerFilter !== "auto" ? <option value="">No models available</option> : null}
            {props.providerFilter === "auto" && availableCloudModels.length === 0 && availableLocalModels.length === 0 ? <option value="">No models available</option> : null}
          </select>
          <p className="mt-2 text-xs text-slate-500">
            {props.allowedModels.length > 0
              ? "Only models allowed by the active session are shown."
              : "Available models come from the current provider catalog."}
          </p>
        </label>

        <label>
          <span className="mb-2 block text-sm text-slate-600">Tenant</span>
          <input
            className={`${props.inputClassName} ${props.tenantLocked ? "cursor-not-allowed bg-slate-100/90 text-slate-500" : ""}`}
            disabled={props.tenantLocked}
            value={props.tenant}
            onChange={(event) => props.onTenantChange(event.target.value)}
          />
          <p className="mt-2 text-xs text-slate-500">
            {props.tenantLocked ? "This tenant is locked by the active tenant token." : "Optional tenant/user context sent with the request."}
          </p>
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

      <div className="mt-5 grid gap-4">
        <SegmentedTabs
          tabs={[
            { id: "response", label: "Response" },
            { id: "runtime", label: "Runtime" },
            { id: "usage", label: "Usage & Cost" },
          ]}
          value={resultView}
          onChange={setResultView}
        />

        {resultView === "response" ? (
          <article className="rounded-3xl bg-slate-50/90 p-5">
            <h3 className="mb-3 text-lg font-semibold text-slate-900">Assistant response</h3>
            {props.chatResult ? (
              <pre className="whitespace-pre-wrap font-mono text-sm text-sky-950">
                {props.chatResult.choices?.[0]?.message?.content ?? "No message content returned."}
              </pre>
            ) : (
              <div className="rounded-2xl border border-slate-200/80 bg-white px-4 py-4 text-sm text-slate-600">
                Send a request to inspect the assistant output here.
              </div>
            )}
          </article>
        ) : null}

        {resultView === "runtime" ? (
          <article className="rounded-3xl bg-slate-50/90 p-5">
            <h3 className="mb-3 text-lg font-semibold text-slate-900">Runtime metadata</h3>
            {props.runtimeHeaders ? (
              <dl>
                <KV label="Request ID" value={props.runtimeHeaders.requestId} />
                <KV label="Provider" value={props.runtimeHeaders.provider} />
                <KV label="Kind" value={props.runtimeHeaders.providerKind} />
                <KV label="Route Reason" value={props.runtimeHeaders.routeReason} />
                <KV label="Requested Model" value={props.runtimeHeaders.requestedModel} />
                <KV label="Resolved Model" value={props.runtimeHeaders.resolvedModel} />
              </dl>
            ) : (
              <div className="rounded-2xl border border-slate-200/80 bg-white px-4 py-4 text-sm text-slate-600">
                Runtime metadata will appear after the first successful request.
              </div>
            )}
          </article>
        ) : null}

        {resultView === "usage" ? (
          <article className="rounded-3xl bg-slate-50/90 p-5">
            <h3 className="mb-3 text-lg font-semibold text-slate-900">Usage and cost</h3>
            {props.chatResult || props.runtimeHeaders ? (
              <dl>
                {usageItems.map((item) => (
                  <KV key={item.label} label={item.label} value={item.value} />
                ))}
              </dl>
            ) : (
              <div className="rounded-2xl border border-slate-200/80 bg-white px-4 py-4 text-sm text-slate-600">
                Usage and cost details will appear once a request has been processed.
              </div>
            )}
          </article>
        ) : null}
      </div>
    </Panel>
  );
}

function filterAllowedProviders(items: ProviderRecord[], allowedProviders: string[]): ProviderRecord[] {
  if (allowedProviders.length === 0) {
    return items;
  }
  const allowed = new Set(allowedProviders);
  return items.filter((item) => allowed.has(item.name));
}

function filterAllowedModels(items: ModelRecord[], allowedModels: string[]): ModelRecord[] {
  if (allowedModels.length === 0) {
    return items;
  }
  const allowed = new Set(allowedModels);
  return items.filter((item) => allowed.has(item.id));
}

function SummaryPill(props: { label: string; value: string }) {
  return (
    <div className="rounded-2xl bg-white px-4 py-3">
      <p className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-500">{props.label}</p>
      <p className="mt-1 text-sm font-medium text-slate-900 break-all">{props.value}</p>
    </div>
  );
}
