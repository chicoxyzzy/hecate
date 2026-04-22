import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatDateTime, formatUsd } from "../../lib/format";
import { budgetConsumedPercent, budgetWarningTone, describeBudgetScope } from "../../lib/runtime-utils";
import {
  DefinitionList,
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
  const budgetPercent = budgetConsumedPercent(state.budget);

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection eyebrow="Budget controls" title="Budget">
          <div className="two-column-grid">
            <Surface>
              {state.budget ? (
                <div className="stack-lg">
                  <div className="stack-sm">
                    <div className="action-row">
                      <StatusPill label={describeBudgetScope(state.budget)} tone="neutral" />
                      <StatusPill label={`${budgetPercent}% used`} tone={budgetPercent >= 90 ? "danger" : budgetPercent >= 70 ? "warning" : "healthy"} />
                    </div>
                    <div className="metric-grid metric-grid--compact">
                      <BudgetMetric label="Current" value={formatUsd(state.budget.current_usd)} />
                      <BudgetMetric label="Remaining" value={formatUsd(state.budget.remaining_usd)} />
                      <BudgetMetric label="Limit" value={formatUsd(state.budget.max_usd)} />
                    </div>
                  </div>

                  <DefinitionList
                    items={[
                      { label: "Scope", value: state.budget.scope },
                      { label: "Key", value: state.budget.key },
                      { label: "Backend", value: state.budget.backend },
                      { label: "Limit source", value: state.budget.limit_source },
                      { label: "Provider", value: state.budget.provider || "n/a" },
                      { label: "Tenant", value: state.budget.tenant || "n/a" },
                    ]}
                  />

                  <div className="stack-sm">
                    <p className="label-muted">Warning thresholds</p>
                    {state.budget.warnings?.length ? (
                      <div className="budget-thresholds">
                        {state.budget.warnings.map((warning) => (
                          <div className="budget-threshold" key={warning.threshold_percent}>
                            <div className="action-row">
                              <StatusPill label={`${warning.threshold_percent}%`} tone={budgetWarningTone(warning.triggered)} />
                              <span className="body-muted">{formatUsd((warning.threshold_micros_usd / 1_000_000).toFixed(6))}</span>
                            </div>
                            <p className="body-muted">
                              {warning.triggered ? "Threshold reached for current scope." : "Threshold not reached yet."}
                            </p>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <EmptyState title="No warnings" detail="No warning thresholds returned for this budget scope." />
                    )}
                  </div>
                </div>
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

          <div className="mt-4">
            <Surface>
              <div className="stack-sm">
                <p className="label-muted">Budget history</p>
                {state.budget?.history?.length ? (
                  <ul className="budget-history-list">
                    {state.budget.history.map((entry, index) => (
                      <li className="budget-history-item" key={`${entry.timestamp}-${entry.type}-${index}`}>
                        <div className="budget-history-item__head">
                          <div className="action-row">
                            <strong>{renderBudgetHistoryLabel(entry.type)}</strong>
                            <StatusPill label={entry.provider || "scope"} tone="neutral" />
                            {entry.request_id ? <StatusPill label={entry.request_id} tone="neutral" /> : null}
                          </div>
                          <span className="body-muted">{formatDateTime(entry.timestamp)}</span>
                        </div>
                        <div className="budget-history-item__body">
                          <span>{formatUsd(entry.amount_usd)}</span>
                          <span>Balance {formatUsd(entry.balance_usd)}</span>
                          <span>Limit {formatUsd(entry.limit_usd)}</span>
                          {entry.model ? <span>{entry.model}</span> : null}
                          {entry.actor ? <span>{entry.actor}</span> : null}
                        </div>
                        {entry.detail ? <p className="body-muted">{entry.detail}</p> : null}
                      </li>
                    ))}
                  </ul>
                ) : (
                  <EmptyState title="No history" detail="Top-ups, resets, limit changes, and usage entries will appear here." />
                )}
              </div>
            </Surface>
          </div>
        </ShellSection>

        <ShellSection eyebrow="Retention" title="Retention">
          <div className="two-column-grid">
            <Surface tone="strong">
              <div className="stack-md">
                <TextAreaField
                  label="Subsystems (comma separated)"
                  onChange={actions.setRetentionSubsystems}
                  rows={3}
                  value={state.retentionSubsystems}
                  placeholder="trace_snapshots,exact_cache,semantic_cache,budget_events,audit_events"
                />
                <p className="body-muted">
                  Leave empty to run every configured subsystem. Use a comma-separated subset for targeted cleanup passes.
                </p>
                {state.retentionError ? <InlineNotice message={state.retentionError} tone="error" /> : null}
                <div className="action-row">
                  <ToolbarButton onClick={() => void actions.runRetention()} tone="primary">
                    {state.retentionLoading ? "Running retention..." : "Run retention"}
                  </ToolbarButton>
                  <StatusPill label={state.retentionLastRun ? `Last run ${formatDateTime(state.retentionLastRun.finished_at)}` : "No runs yet"} tone="neutral" />
                </div>
              </div>
            </Surface>

            <Surface>
              {state.retentionLastRun ? (
                <div className="stack-md">
                  <div className="action-row action-row--wide">
                    <StatusPill label={state.retentionLastRun.trigger} tone="neutral" />
                    <StatusPill
                      label={`${state.retentionLastRun.results.filter((item) => !item.skipped).reduce((sum, item) => sum + item.deleted, 0)} deleted`}
                      tone="healthy"
                    />
                  </div>
                  <DefinitionList
                    items={[
                      { label: "Started", value: formatDateTime(state.retentionLastRun.started_at) },
                      { label: "Finished", value: formatDateTime(state.retentionLastRun.finished_at) },
                      { label: "Actor", value: state.retentionLastRun.actor || "system" },
                      { label: "Subsystems", value: state.retentionLastRun.results.length.toString() },
                    ]}
                  />
                  <div className="trace-inline-grid">
                    {state.retentionLastRun.results.map((result) => (
                      <div className="trace-inline-card" key={`${state.retentionLastRun.finished_at}-${result.name}`}>
                        <p className="trace-inline-card__title">{result.name}</p>
                        <div className="action-row">
                          <StatusPill
                            label={result.error ? "failed" : result.skipped ? "skipped" : `${result.deleted} deleted`}
                            tone={result.error ? "danger" : result.skipped ? "neutral" : result.deleted > 0 ? "healthy" : "warning"}
                          />
                        </div>
                        <p className="body-muted">
                          max age {result.max_age || "n/a"} · max count {result.max_count}
                        </p>
                        {result.error ? <p className="body-muted">{result.error}</p> : null}
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <EmptyState title="No retention run" detail="Run retention to inspect deleted rows, skipped subsystems, and per-subsystem limits." />
              )}
            </Surface>
          </div>

          <div className="mt-4">
            <Surface>
              <div className="stack-sm">
                <p className="label-muted">Recent retention runs</p>
                {state.retentionRuns.length ? (
                  <ul className="budget-history-list">
                    {state.retentionRuns.map((run, index) => (
                      <li className="budget-history-item" key={`${run.finished_at}-${index}`}>
                        <div className="budget-history-item__head">
                          <div className="action-row">
                            <strong>{run.trigger}</strong>
                            <StatusPill label={`${run.results.filter((item) => !item.skipped).reduce((sum, item) => sum + item.deleted, 0)} deleted`} tone="healthy" />
                          </div>
                          <span className="body-muted">{formatDateTime(run.finished_at)}</span>
                        </div>
                        <div className="budget-history-item__body">
                          {run.actor ? <span>actor: {run.actor}</span> : null}
                          {run.request_id ? <span>request: {run.request_id}</span> : null}
                          {run.results.map((result) => (
                            <span key={`${run.finished_at}-${result.name}`}>
                              {result.name}: {result.error ? "failed" : result.skipped ? "skipped" : `${result.deleted} deleted`}
                            </span>
                          ))}
                        </div>
                      </li>
                    ))}
                  </ul>
                ) : (
                  <EmptyState title="No retention history" detail="Persisted retention runs will appear here after the first cleanup cycle." />
                )}
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

function BudgetMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="budget-metric">
      <p className="budget-metric__label">{label}</p>
      <p className="budget-metric__value">{value}</p>
    </div>
  );
}

function renderBudgetHistoryLabel(value: string): string {
  switch (value) {
    case "top_up":
      return "Top up";
    case "set_limit":
      return "Set limit";
    case "reset":
      return "Reset";
    case "usage":
      return "Usage";
    default:
      return value;
  }
}
