import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { EmptyState, ShellSection, StatusPill, Surface, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function AccessView({ state, actions }: Props) {
  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection
          eyebrow="Authentication"
          title="Session"
        >
          <Surface tone="strong">
            <div className="stack-lg">
              <TextField label="Bearer token" onChange={actions.setAuthToken} placeholder="Paste tenant or admin token" value={state.authToken} />
              <div className="action-row">
                <ToolbarButton onClick={() => void actions.loadDashboard()} tone="primary">
                  Refresh session
                </ToolbarButton>
                <ToolbarButton onClick={actions.clearAuthToken}>Clear token</ToolbarButton>
                <StatusPill
                  label={state.session.label}
                  tone={
                    state.session.kind === "admin"
                      ? "healthy"
                      : state.session.kind === "tenant"
                        ? "neutral"
                        : state.session.kind === "invalid"
                          ? "danger"
                          : "warning"
                  }
                />
              </div>
            </div>
          </Surface>
        </ShellSection>

        <ShellSection
          eyebrow="Scope"
          title="Scope"
        >
          <div className="two-column-grid">
            <Surface>
              <dl className="definition-list">
                <div className="definition-list__row">
                  <dt>Name</dt>
                  <dd>{state.session.name || "Not set"}</dd>
                </div>
                <div className="definition-list__row">
                  <dt>Role</dt>
                  <dd>{state.session.role}</dd>
                </div>
                <div className="definition-list__row">
                  <dt>Tenant</dt>
                  <dd>{state.session.tenant || "Not scoped"}</dd>
                </div>
                <div className="definition-list__row">
                  <dt>Source</dt>
                  <dd>{state.session.source || "Not returned"}</dd>
                </div>
                <div className="definition-list__row">
                  <dt>Key ID</dt>
                  <dd>{state.session.keyID || "Not returned"}</dd>
                </div>
              </dl>
            </Surface>
            <Surface>
              <div className="stack-sm">
                <p className="label-muted">Capabilities</p>
                {state.session.capabilities.length > 0 ? (
                  <ul className="bullet-list">
                    {state.session.capabilities.map((capability) => (
                      <li key={capability}>{capability}</li>
                    ))}
                  </ul>
                ) : (
                  <EmptyState title="No capabilities" detail="None returned." />
                )}
              </div>
            </Surface>
          </div>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Restrictions" title="Access">
          <Surface>
            <div className="stack-md">
              <div>
                <p className="label-muted">Allowed providers</p>
                {state.session.allowedProviders.length > 0 ? (
                  <div className="chip-row">
                    {state.session.allowedProviders.map((provider) => (
                      <span className="mono-chip" key={provider}>
                        {provider}
                      </span>
                    ))}
                  </div>
                ) : (
                  <p className="body-muted">No provider restriction.</p>
                )}
              </div>
              <div>
                <p className="label-muted">Allowed models</p>
                {state.session.allowedModels.length > 0 ? (
                  <div className="chip-row">
                    {state.session.allowedModels.slice(0, 16).map((model) => (
                      <span className="mono-chip" key={model}>
                        {model}
                      </span>
                    ))}
                    {state.session.allowedModels.length > 16 ? <span className="body-muted">+{state.session.allowedModels.length - 16} more</span> : null}
                  </div>
                ) : (
                  <p className="body-muted">No model restriction.</p>
                )}
              </div>
            </div>
          </Surface>
        </ShellSection>
      </aside>
    </div>
  );
}
