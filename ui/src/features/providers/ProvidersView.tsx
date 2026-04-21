import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatDateTime, formatRelativeCount } from "../../lib/format";
import { EmptyState, MetricTile, SelectField, ShellSection, StatusPill, Surface, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function ProvidersView({ state, actions }: Props) {
  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection
          eyebrow="Provider routing"
          title="Providers"
          actions={<ToolbarButton onClick={() => void actions.loadDashboard()}>Refresh provider state</ToolbarButton>}
        >
          <div className="metric-grid">
            <MetricTile label="Cloud providers" tone={state.cloudProviders.length > 0 && state.healthyCloudProviders === state.cloudProviders.length ? "healthy" : "warning"} value={formatRelativeCount("healthy", state.healthyCloudProviders, state.cloudProviders.length)} />
            <MetricTile label="Local providers" tone={state.localProviders.length > 0 && state.healthyLocalProviders === state.localProviders.length ? "healthy" : "warning"} value={formatRelativeCount("healthy", state.healthyLocalProviders, state.localProviders.length)} />
            <MetricTile label="Catalog size" value={`${state.models.length} models`} />
          </div>

          <Surface>
            {state.providers.length > 0 ? (
              <div className="stack-sm">
                {state.providers.map((provider) => (
                  <div className="data-row" key={provider.name}>
                    <div className="data-row__primary">
                      <div className="action-row">
                        <strong>{provider.name}</strong>
                        <StatusPill label={provider.kind} tone={provider.kind === "local" ? "warning" : "neutral"} />
                        <StatusPill label={provider.healthy ? "healthy" : provider.status} tone={provider.healthy ? "healthy" : "danger"} />
                      </div>
                      <p className="body-muted">
                        Default model: {provider.default_model || "Not set"} • Discovered {provider.models?.length ?? 0} model(s) • Refreshed {formatDateTime(provider.refreshed_at)}
                      </p>
                    </div>
                    <div className="data-row__secondary">
                      {provider.discovery_source ? <span className="mono-chip">{provider.discovery_source}</span> : null}
                      {provider.error ? <span className="error-chip">{provider.error}</span> : null}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState title="No provider data" detail="Admin access required." />
            )}
          </Surface>
        </ShellSection>

        <ShellSection
          eyebrow="Catalog"
          title="Models"
        >
          <div className="form-grid">
            <SelectField label="Catalog scope" onChange={(value) => actions.setModelFilter(value as "all" | "local" | "cloud")} value={state.modelFilter}>
              <option value="all">All models</option>
              <option value="local">Local only</option>
              <option value="cloud">Cloud only</option>
            </SelectField>
            <TextField label="Pinned provider filter" onChange={actions.setProviderFilter} placeholder="auto or provider name" value={state.providerFilter} />
          </div>

          <Surface>
            {state.visibleModels.length > 0 ? (
              <div className="stack-sm">
                {state.visibleModels.map((model) => (
                  <div className="data-row" key={`${model.metadata?.provider}-${model.id}`}>
                    <div className="data-row__primary">
                      <div className="action-row">
                        <strong>{model.id}</strong>
                        <StatusPill label={model.metadata?.provider || "unknown provider"} tone="neutral" />
                        <StatusPill label={model.metadata?.provider_kind || "unknown kind"} tone={model.metadata?.provider_kind === "local" ? "warning" : "neutral"} />
                        {model.metadata?.default ? <StatusPill label="default" tone="healthy" /> : null}
                      </div>
                      <p className="body-muted">Discovery source: {model.metadata?.discovery_source || "Not returned"}.</p>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState title="No models" detail="Change filters or auth." />
            )}
          </Surface>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Local runtime" title="Issues">
          {state.localProviderIssues.length > 0 ? (
            <div className="stack-sm">
              {state.localProviderIssues.map((issue) => (
                <Surface key={`${issue.provider}-${issue.model}`} tone="danger">
                  <div className="stack-sm">
                    <div className="action-row">
                      <StatusPill label={issue.provider} tone="warning" />
                      <StatusPill label={issue.model} tone="danger" />
                    </div>
                    <p className="body-muted">{issue.message}</p>
                    {issue.command ? (
                      <ToolbarButton onClick={() => void actions.copyCommand(issue.command!)}>{state.copiedCommand === issue.command ? "Copied command" : issue.command}</ToolbarButton>
                    ) : null}
                  </div>
                </Surface>
              ))}
            </div>
          ) : (
            <Surface>
              <EmptyState title="No issues" detail="No local warnings." />
            </Surface>
          )}
        </ShellSection>
      </aside>
    </div>
  );
}
