import { Panel } from "./Panel";
import type { ControlPlaneResponse } from "../types/runtime";

type ControlPlanePanelProps = {
  controlPlane: ControlPlaneResponse["data"] | null;
  controlPlaneError: string;
  inputClassName: string;
  tenantFormID: string;
  tenantFormModels: string;
  tenantFormName: string;
  tenantFormProviders: string;
  apiKeyFormID: string;
  apiKeyFormModels: string;
  apiKeyFormName: string;
  apiKeyFormProviders: string;
  apiKeyFormRole: string;
  apiKeyFormSecret: string;
  apiKeyFormTenant: string;
  rotateAPIKeyID: string;
  rotateAPIKeySecret: string;
  onDeleteAPIKey: (id: string) => void | Promise<void>;
  onDeleteTenant: (id: string) => void | Promise<void>;
  onAPIKeyFormIDChange: (value: string) => void;
  onAPIKeyFormModelsChange: (value: string) => void;
  onAPIKeyFormNameChange: (value: string) => void;
  onAPIKeyFormProvidersChange: (value: string) => void;
  onAPIKeyFormRoleChange: (value: string) => void;
  onAPIKeyFormSecretChange: (value: string) => void;
  onAPIKeyFormTenantChange: (value: string) => void;
  onRotateAPIKey: () => void | Promise<void>;
  onSaveAPIKey: () => void | Promise<void>;
  onSaveTenant: () => void | Promise<void>;
  onSetAPIKeyEnabled: (id: string, enabled: boolean) => void | Promise<void>;
  onSetRotateAPIKeyID: (value: string) => void;
  onSetRotateAPIKeySecret: (value: string) => void;
  onSetTenantEnabled: (id: string, enabled: boolean) => void | Promise<void>;
  onTenantFormIDChange: (value: string) => void;
  onTenantFormModelsChange: (value: string) => void;
  onTenantFormNameChange: (value: string) => void;
  onTenantFormProvidersChange: (value: string) => void;
};

export function ControlPlanePanel(props: ControlPlanePanelProps) {
  return (
    <Panel eyebrow="Control Plane" title="Tenants and keys">
      {props.controlPlane ? (
        <div className="mt-4 grid gap-4">
          <div className="rounded-2xl bg-slate-50/90 p-4">
            <p className="text-sm text-slate-500">Backend</p>
            <p className="font-medium text-slate-900">{props.controlPlane.backend}</p>
            {props.controlPlane.path ? <p className="mt-1 text-xs text-slate-500">{props.controlPlane.path}</p> : null}
          </div>

          <div className="grid gap-3 lg:grid-cols-2">
            <article className="rounded-2xl bg-slate-50/90 p-4">
              <h3 className="text-lg font-semibold text-slate-900">Tenants</h3>
              <div className="mt-3 grid gap-3">
                {props.controlPlane.tenants.length === 0 ? <p className="text-sm text-slate-500">No persisted tenants yet.</p> : null}
                {props.controlPlane.tenants.map((entry) => (
                  <div className="rounded-2xl border border-slate-200/80 bg-white px-4 py-3" key={entry.id}>
                    <div className="flex items-center justify-between gap-3">
                      <strong>{entry.name}</strong>
                      <span className="text-xs text-slate-500">{entry.id}</span>
                    </div>
                    <p className="mt-1 text-sm text-slate-500">
                      Providers: {(entry.allowed_providers ?? []).join(", ") || "any"} · Models: {(entry.allowed_models ?? []).join(", ") || "any"}
                    </p>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <button
                        className="rounded-full border border-slate-200/80 bg-white px-3 py-2 text-xs font-medium text-slate-900"
                        onClick={() => void props.onSetTenantEnabled(entry.id, !entry.enabled)}
                        type="button"
                      >
                        {entry.enabled ? "Disable" : "Enable"}
                      </button>
                      <button
                        className="rounded-full border border-red-200 bg-red-50 px-3 py-2 text-xs font-medium text-red-700"
                        onClick={() => void props.onDeleteTenant(entry.id)}
                        type="button"
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                ))}
              </div>

              <div className="mt-4 grid gap-3">
                <input className={props.inputClassName} placeholder="Tenant name" value={props.tenantFormName} onChange={(event) => props.onTenantFormNameChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Tenant id (optional)" value={props.tenantFormID} onChange={(event) => props.onTenantFormIDChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Allowed providers: openai,ollama" value={props.tenantFormProviders} onChange={(event) => props.onTenantFormProvidersChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Allowed models: gpt-4o-mini,llama3.1:8b" value={props.tenantFormModels} onChange={(event) => props.onTenantFormModelsChange(event.target.value)} />
                <button
                  className="inline-flex rounded-full bg-slate-900 px-4 py-3 text-sm font-semibold text-white transition hover:-translate-y-0.5"
                  onClick={() => void props.onSaveTenant()}
                  type="button"
                >
                  Save tenant
                </button>
              </div>
            </article>

            <article className="rounded-2xl bg-slate-50/90 p-4">
              <h3 className="text-lg font-semibold text-slate-900">API keys</h3>
              <div className="mt-3 grid gap-3">
                {props.controlPlane.api_keys.length === 0 ? <p className="text-sm text-slate-500">No persisted api keys yet.</p> : null}
                {props.controlPlane.api_keys.map((entry) => (
                  <div className="rounded-2xl border border-slate-200/80 bg-white px-4 py-3" key={entry.id}>
                    <div className="flex items-center justify-between gap-3">
                      <strong>{entry.name}</strong>
                      <span className="text-xs text-slate-500">{entry.key_preview || entry.id}</span>
                    </div>
                    <p className="mt-1 text-sm text-slate-500">
                      {entry.role} · tenant {entry.tenant || "unbound"} · providers {(entry.allowed_providers ?? []).join(", ") || "any"}
                    </p>
                    <div className="mt-3 flex flex-wrap gap-2">
                      <button
                        className="rounded-full border border-slate-200/80 bg-white px-3 py-2 text-xs font-medium text-slate-900"
                        onClick={() => void props.onSetAPIKeyEnabled(entry.id, !entry.enabled)}
                        type="button"
                      >
                        {entry.enabled ? "Disable" : "Enable"}
                      </button>
                      <button
                        className="rounded-full border border-red-200 bg-red-50 px-3 py-2 text-xs font-medium text-red-700"
                        onClick={() => void props.onDeleteAPIKey(entry.id)}
                        type="button"
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                ))}
              </div>

              <div className="mt-4 grid gap-3">
                <input className={props.inputClassName} placeholder="Key name" value={props.apiKeyFormName} onChange={(event) => props.onAPIKeyFormNameChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Key id (optional)" value={props.apiKeyFormID} onChange={(event) => props.onAPIKeyFormIDChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Secret token" value={props.apiKeyFormSecret} onChange={(event) => props.onAPIKeyFormSecretChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Tenant id" value={props.apiKeyFormTenant} onChange={(event) => props.onAPIKeyFormTenantChange(event.target.value)} />
                <select className={props.inputClassName} value={props.apiKeyFormRole} onChange={(event) => props.onAPIKeyFormRoleChange(event.target.value)}>
                  <option value="tenant">tenant</option>
                  <option value="admin">admin</option>
                </select>
                <input className={props.inputClassName} placeholder="Allowed providers: openai,ollama" value={props.apiKeyFormProviders} onChange={(event) => props.onAPIKeyFormProvidersChange(event.target.value)} />
                <input className={props.inputClassName} placeholder="Allowed models: gpt-4o-mini,llama3.1:8b" value={props.apiKeyFormModels} onChange={(event) => props.onAPIKeyFormModelsChange(event.target.value)} />
                <button
                  className="inline-flex rounded-full border border-slate-200/80 bg-white px-4 py-3 text-sm font-semibold text-slate-900 transition hover:-translate-y-0.5"
                  onClick={() => void props.onSaveAPIKey()}
                  type="button"
                >
                  Save api key
                </button>
              </div>

              <div className="mt-4 border-t border-slate-200/80 pt-4">
                <h4 className="text-sm font-semibold uppercase tracking-[0.16em] text-slate-500">Rotate key</h4>
                <div className="mt-3 grid gap-3">
                  <input className={props.inputClassName} placeholder="Key id" value={props.rotateAPIKeyID} onChange={(event) => props.onSetRotateAPIKeyID(event.target.value)} />
                  <input className={props.inputClassName} placeholder="New secret token" value={props.rotateAPIKeySecret} onChange={(event) => props.onSetRotateAPIKeySecret(event.target.value)} />
                  <button
                    className="inline-flex rounded-full border border-slate-200/80 bg-white px-4 py-3 text-sm font-semibold text-slate-900 transition hover:-translate-y-0.5"
                    onClick={() => void props.onRotateAPIKey()}
                    type="button"
                  >
                    Rotate api key
                  </button>
                </div>
              </div>
            </article>
          </div>

          <article className="rounded-2xl bg-slate-50/90 p-4">
            <h3 className="text-lg font-semibold text-slate-900">Recent activity</h3>
            <div className="mt-3 grid gap-3">
              {props.controlPlane.events.length === 0 ? <p className="text-sm text-slate-500">No control-plane changes recorded yet.</p> : null}
              {props.controlPlane.events.map((event, index) => (
                <div className="rounded-2xl border border-slate-200/80 bg-white px-4 py-3" key={`${event.timestamp || "event"}-${event.action}-${event.target_id}-${index}`}>
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <strong className="text-sm text-slate-900">{event.action}</strong>
                    <span className="text-xs text-slate-500">{event.timestamp || "unknown time"}</span>
                  </div>
                  <p className="mt-1 text-sm text-slate-600">
                    {event.target_type} <span className="font-medium text-slate-900">{event.target_id}</span> by {event.actor || "system"}
                  </p>
                  {event.detail ? <p className="mt-1 text-xs text-slate-500">{event.detail}</p> : null}
                </div>
              ))}
            </div>
          </article>

          {props.controlPlaneError ? (
            <div className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">{props.controlPlaneError}</div>
          ) : null}
        </div>
      ) : (
        <div className="mt-4 rounded-2xl border border-slate-200/80 bg-slate-50/90 px-4 py-4 text-sm text-slate-600">
          Control-plane management is available with an admin bearer token and a configured file backend.
        </div>
      )}
    </Panel>
  );
}
