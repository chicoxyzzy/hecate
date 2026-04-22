import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatDateTime, formatRelativeCount } from "../../lib/format";
import { EmptyState, InlineNotice, MetricTile, SelectField, ShellSection, StatusPill, Surface, TextAreaField, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function ProvidersView({ state, actions }: Props) {
  const cloudPresets = state.providerPresets.filter((preset) => preset.kind === "cloud");
  const localPresets = state.providerPresets.filter((preset) => preset.kind === "local");

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection
          eyebrow="First run"
          title="Provider presets"
          actions={<ToolbarButton onClick={() => void actions.loadDashboard()}>Refresh presets</ToolbarButton>}
        >
          <div className="stack-sm">
            <Surface>
              <p className="body-muted">
                Generate copy-ready `.env` blocks for common cloud and local runtimes. Presets use `GATEWAY_PROVIDERS` with `PROVIDER_<NAME>_*` overrides so onboarding stays readable.
              </p>
            </Surface>

            <div className="metric-grid">
              <MetricTile label="Cloud presets" value={`${cloudPresets.length}`} />
              <MetricTile label="Local presets" value={`${localPresets.length}`} />
              <MetricTile label="Configured providers" value={`${state.providers.length}`} />
            </div>

            {renderPresetGroup("Cloud", cloudPresets, state, actions)}
            {renderPresetGroup("Local", localPresets, state, actions)}
          </div>
        </ShellSection>

        {state.session.isAdmin ? (
          <ShellSection eyebrow="Control plane" title="Managed providers">
            <div className="stack-sm">
              <Surface tone="strong">
                <div className="form-grid">
                  <SelectField label="Preset" onChange={actions.populateProviderFormFromPreset} value={state.providerFormPresetID}>
                    <option value="">Custom provider</option>
                    {state.providerPresets.map((preset) => (
                      <option key={preset.id} value={preset.id}>
                        {preset.name}
                      </option>
                    ))}
                  </SelectField>
                  <TextField label="Provider id" onChange={actions.setProviderFormID} value={state.providerFormID} />
                  <TextField label="Provider name" onChange={actions.setProviderFormName} value={state.providerFormName} />
                  <SelectField label="Kind" onChange={actions.setProviderFormKind} value={state.providerFormKind}>
                    <option value="cloud">cloud</option>
                    <option value="local">local</option>
                  </SelectField>
                  <SelectField label="Protocol" onChange={actions.setProviderFormProtocol} value={state.providerFormProtocol}>
                    <option value="openai">openai</option>
                    <option value="anthropic">anthropic</option>
                  </SelectField>
                  <TextField label="Base URL" onChange={actions.setProviderFormBaseURL} value={state.providerFormBaseURL} />
                  <TextField label="API version" onChange={actions.setProviderFormAPIVersion} value={state.providerFormAPIVersion} />
                  <TextField label="Default model" onChange={actions.setProviderFormDefaultModel} value={state.providerFormDefaultModel} />
                  <SelectField label="Allow any model" onChange={actions.setProviderFormAllowAnyModel} value={state.providerFormAllowAnyModel}>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </SelectField>
                  <SelectField label="Enabled" onChange={actions.setProviderFormEnabled} value={state.providerFormEnabled}>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </SelectField>
                </div>
                <div className="stack-sm">
                  <TextAreaField label="Models (comma separated)" onChange={actions.setProviderFormModels} rows={3} value={state.providerFormModels} />
                  <TextField label="API key or token" onChange={actions.setProviderFormSecret} value={state.providerFormSecret} />
                  <p className="body-muted">
                    Secrets are write-only. They are encrypted before persistence and never returned to the UI after save.
                  </p>
                  <p className="body-muted">
                    Leave model fields blank to use the provider's live catalog discovery. Hecate will prefer upstream model discovery over preset example lists.
                  </p>
                  {state.controlPlaneError ? <InlineNotice message={state.controlPlaneError} tone="error" /> : null}
                  <div className="action-row">
                    <ToolbarButton onClick={() => void actions.upsertProvider()} tone="primary">
                      Save provider
                    </ToolbarButton>
                  </div>
                </div>
              </Surface>

              <Surface>
                {state.controlPlane?.providers?.length ? (
                  <div className="stack-sm">
                    {state.controlPlane.providers.map((provider) => (
                      <div className="data-row" key={provider.id}>
                        <div className="data-row__primary">
                          <div className="action-row">
                            <strong>{provider.name}</strong>
                            <StatusPill label={provider.kind} tone={provider.kind === "local" ? "warning" : "neutral"} />
                            <StatusPill label={provider.protocol} tone="neutral" />
                            <StatusPill label={provider.enabled ? "enabled" : "disabled"} tone={provider.enabled ? "healthy" : "warning"} />
                            {provider.credential_configured ? <StatusPill label={provider.credential_preview || "secret configured"} tone="neutral" /> : null}
                          </div>
                          <p className="body-muted">
                            {provider.base_url}
                            {provider.default_model ? ` • default ${provider.default_model}` : ""}
                            {provider.models?.length ? ` • ${provider.models.length} configured model(s)` : ""}
                          </p>
                        </div>
                        <div className="data-row__secondary">
                          <ToolbarButton onClick={() => actions.setRotateProviderID(provider.id)}>Select</ToolbarButton>
                          <ToolbarButton onClick={() => void actions.setProviderEnabled(provider.id, !provider.enabled)}>
                            {provider.enabled ? "Disable" : "Enable"}
                          </ToolbarButton>
                          <ToolbarButton onClick={() => void actions.deleteProvider(provider.id)} tone="danger">
                            Delete
                          </ToolbarButton>
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <EmptyState title="No managed providers" detail="Save a preset or custom provider to persist it in the control plane." />
                )}
              </Surface>

              <Surface>
                <div className="stack-sm">
                  <TextField label="Rotate provider secret: provider id" onChange={actions.setRotateProviderID} value={state.rotateProviderID} />
                  <TextField label="New provider API key" onChange={actions.setRotateProviderSecret} value={state.rotateProviderSecret} />
                  <div className="action-row">
                    <ToolbarButton onClick={() => void actions.rotateProviderCredential()}>Rotate provider secret</ToolbarButton>
                  </div>
                </div>
              </Surface>
            </div>
          </ShellSection>
        ) : null}

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

function renderPresetGroup(
  title: string,
  presets: RuntimeConsoleViewModel["state"]["providerPresets"],
  state: RuntimeConsoleViewModel["state"],
  actions: RuntimeConsoleViewModel["actions"],
) {
  return (
    <Surface>
      <div className="stack-sm">
        <div className="action-row">
          <strong>{title}</strong>
          <StatusPill label={`${presets.length} presets`} tone="neutral" />
        </div>
        {presets.length > 0 ? (
          presets.map((preset) => (
            <div className="data-row" key={preset.id}>
              <div className="data-row__primary">
                <div className="action-row">
                  <strong>{preset.name}</strong>
                  <StatusPill label={preset.protocol} tone="neutral" />
                  <StatusPill label={preset.kind} tone={preset.kind === "local" ? "warning" : "healthy"} />
                </div>
                <p className="body-muted">
                  {preset.description} Base URL: <code>{preset.base_url}</code>
                  {preset.default_model ? ` • Default model: ${preset.default_model}` : ""}
                </p>
                {preset.example_models && preset.example_models.length > 0 ? (
                  <div className="action-row">
                    {preset.example_models.map((model) => (
                      <span className="mono-chip" key={`${preset.id}-${model}`}>
                        {model}
                      </span>
                    ))}
                  </div>
                ) : null}
              </div>
              <div className="data-row__secondary">
                {preset.docs_url ? (
                  <a className="toolbar-button" href={preset.docs_url} rel="noreferrer" target="_blank">
                    Docs
                  </a>
                ) : null}
                {preset.env_snippet ? (
                  <ToolbarButton onClick={() => void actions.copyCommand(preset.env_snippet!)}>
                    {state.copiedCommand === preset.env_snippet ? "Copied env" : "Copy env"}
                  </ToolbarButton>
                ) : null}
                {state.session.isAdmin ? (
                  <ToolbarButton onClick={() => actions.populateProviderFormFromPreset(preset.id)} tone="primary">
                    Use preset
                  </ToolbarButton>
                ) : null}
              </div>
            </div>
          ))
        ) : (
          <EmptyState title={`No ${title.toLowerCase()} presets`} detail="Preset catalog unavailable." />
        )}
      </div>
    </Surface>
  );
}
