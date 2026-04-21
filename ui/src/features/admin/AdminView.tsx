import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatDateTime } from "../../lib/format";
import {
  EmptyState,
  InlineNotice,
  ShellSection,
  StatusPill,
  Surface,
  TextAreaField,
  TextField,
  ToolbarButton,
} from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function AdminView({ state, actions }: Props) {
  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection
          eyebrow="Budget controls"
          title="Budget"
        >
          <div className="two-column-grid">
            <Surface>
              {state.budget ? (
                <dl className="definition-list">
                  <div className="definition-list__row">
                    <dt>Scope</dt>
                    <dd>{state.budget.scope}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Key</dt>
                    <dd>{state.budget.key}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Current spend</dt>
                    <dd>{state.budget.current_usd}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Limit</dt>
                    <dd>{state.budget.max_usd}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Remaining</dt>
                    <dd>{state.budget.remaining_usd}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Backend</dt>
                    <dd>{state.budget.backend}</dd>
                  </div>
                </dl>
              ) : (
                <EmptyState title="No budget data" detail="Admin access required." />
              )}
            </Surface>

            <Surface tone="strong">
              <div className="stack-md">
                <TextField label="Top-up amount (USD)" onChange={actions.setBudgetAmountUsd} value={state.budgetAmountUsd} />
                <TextField label="Budget limit (USD)" onChange={actions.setBudgetLimitUsd} value={state.budgetLimitUsd} />
                {state.budgetActionError ? <InlineNotice message={state.budgetActionError} tone="error" /> : null}
                <div className="action-row">
                  <ToolbarButton onClick={() => void actions.topUpBudget()} tone="primary">
                    Top up budget
                  </ToolbarButton>
                  <ToolbarButton onClick={() => void actions.setBudgetLimit()}>Set limit</ToolbarButton>
                  <ToolbarButton onClick={() => void actions.resetBudget()} tone="danger">
                    Reset usage
                  </ToolbarButton>
                </div>
              </div>
            </Surface>
          </div>
        </ShellSection>

        <ShellSection
          eyebrow="Control plane"
          title="Control plane"
        >
          {state.controlPlaneError ? <InlineNotice message={state.controlPlaneError} tone="error" /> : null}
          <div className="two-column-grid">
            <Surface>
              <div className="stack-lg">
                <div className="stack-sm">
                  <p className="label-muted">Create or update tenant</p>
                  <TextField label="Tenant ID" onChange={actions.setTenantFormID} value={state.tenantFormID} />
                  <TextField label="Tenant name" onChange={actions.setTenantFormName} value={state.tenantFormName} />
                  <TextAreaField label="Allowed providers (comma separated)" onChange={actions.setTenantFormProviders} rows={3} value={state.tenantFormProviders} />
                  <TextAreaField label="Allowed models (comma separated)" onChange={actions.setTenantFormModels} rows={3} value={state.tenantFormModels} />
                  <ToolbarButton onClick={() => void actions.upsertTenant()} tone="primary">
                    Save tenant
                  </ToolbarButton>
                </div>

                <div className="stack-sm">
                  <p className="label-muted">Create or update API key</p>
                  <TextField label="Key ID" onChange={actions.setAPIKeyFormID} value={state.apiKeyFormID} />
                  <TextField label="Name" onChange={actions.setAPIKeyFormName} value={state.apiKeyFormName} />
                  <TextField label="Secret" onChange={actions.setAPIKeyFormSecret} value={state.apiKeyFormSecret} />
                  <TextField label="Tenant" onChange={actions.setAPIKeyFormTenant} value={state.apiKeyFormTenant} />
                  <TextField label="Role" onChange={actions.setAPIKeyFormRole} value={state.apiKeyFormRole} />
                  <TextAreaField label="Allowed providers (comma separated)" onChange={actions.setAPIKeyFormProviders} rows={3} value={state.apiKeyFormProviders} />
                  <TextAreaField label="Allowed models (comma separated)" onChange={actions.setAPIKeyFormModels} rows={3} value={state.apiKeyFormModels} />
                  <ToolbarButton onClick={() => void actions.upsertAPIKey()} tone="primary">
                    Save API key
                  </ToolbarButton>
                </div>
              </div>
            </Surface>

            <Surface>
              <div className="stack-lg">
                <div className="stack-sm">
                  <p className="label-muted">Rotate API key</p>
                  <TextField label="Key ID" onChange={actions.setRotateAPIKeyID} value={state.rotateAPIKeyID} />
                  <TextField label="New secret" onChange={actions.setRotateAPIKeySecret} value={state.rotateAPIKeySecret} />
                  <ToolbarButton onClick={() => void actions.rotateAPIKey()}>Rotate key</ToolbarButton>
                </div>

                <div className="stack-sm">
                  <p className="label-muted">Tenants</p>
                  {state.controlPlane?.tenants.length ? (
                    state.controlPlane.tenants.map((tenant) => (
                      <div className="data-row" key={tenant.id}>
                        <div className="data-row__primary">
                          <div className="action-row">
                            <strong>{tenant.name || tenant.id}</strong>
                            <StatusPill label={tenant.enabled ? "enabled" : "disabled"} tone={tenant.enabled ? "healthy" : "warning"} />
                          </div>
                          <p className="body-muted">{tenant.id}</p>
                        </div>
                        <div className="action-row">
                          <ToolbarButton onClick={() => void actions.setTenantEnabled(tenant.id, !tenant.enabled)}>
                            {tenant.enabled ? "Disable" : "Enable"}
                          </ToolbarButton>
                          <ToolbarButton onClick={() => void actions.deleteTenant(tenant.id)} tone="danger">
                            Delete
                          </ToolbarButton>
                        </div>
                      </div>
                    ))
                  ) : (
                    <EmptyState title="No tenants" detail="Create one to begin." />
                  )}
                </div>

                <div className="stack-sm">
                  <p className="label-muted">API keys</p>
                  {state.controlPlane?.api_keys.length ? (
                    state.controlPlane.api_keys.map((apiKey) => (
                      <div className="data-row" key={apiKey.id}>
                        <div className="data-row__primary">
                          <div className="action-row">
                            <strong>{apiKey.name || apiKey.id}</strong>
                            <StatusPill label={apiKey.role} tone="neutral" />
                            <StatusPill label={apiKey.enabled ? "enabled" : "disabled"} tone={apiKey.enabled ? "healthy" : "warning"} />
                          </div>
                          <p className="body-muted">
                            {apiKey.id} • {apiKey.tenant || "no tenant"} • Updated {formatDateTime(apiKey.updated_at)}
                          </p>
                        </div>
                        <div className="action-row">
                          <ToolbarButton onClick={() => void actions.setAPIKeyEnabled(apiKey.id, !apiKey.enabled)}>
                            {apiKey.enabled ? "Disable" : "Enable"}
                          </ToolbarButton>
                          <ToolbarButton onClick={() => void actions.deleteAPIKey(apiKey.id)} tone="danger">
                            Delete
                          </ToolbarButton>
                        </div>
                      </div>
                    ))
                  ) : (
                    <EmptyState title="No API keys" detail="Create one to begin." />
                  )}
                </div>
              </div>
            </Surface>
          </div>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Audit" title="Events">
          <Surface>
            {state.controlPlane?.events.length ? (
              <ul className="audit-list">
                {state.controlPlane.events.slice(0, 12).map((event) => (
                  <li key={`${event.timestamp}-${event.action}-${event.target_id}`}>
                    <strong>{event.action}</strong>
                    <span>
                      {event.target_type}:{event.target_id}
                    </span>
                    <small>
                      {event.actor} • {formatDateTime(event.timestamp)}
                    </small>
                  </li>
                ))}
              </ul>
            ) : (
              <EmptyState title="No events" detail="No audit events loaded." />
            )}
          </Surface>
        </ShellSection>
      </aside>
    </div>
  );
}
