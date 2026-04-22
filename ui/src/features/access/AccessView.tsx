import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { DefinitionList, InlineNotice, ShellSection, StatusPill, Surface, ToolbarButton, TokenField } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function AccessView({ state, actions }: Props) {
  const { kind, role, name, tenant, source, keyID, capabilities, allowedProviders, allowedModels } = state.session;
  const isAuthenticated = kind === "admin" || kind === "tenant";

  if (!isAuthenticated) {
    return (
      <div className="auth-gate">
        <div className="auth-gate__card">
          <div className="auth-gate__header">
            <span className={`role-badge role-badge--${kind}`}>
              {kind === "invalid" ? "Token invalid" : "Not authenticated"}
            </span>
            <h2 className="auth-gate__title">Connect to Hecate</h2>
            <p className="auth-gate__subtitle">
              Paste a bearer token to authenticate. Your role and access scope are set by the token.
            </p>
          </div>

          {kind === "invalid" ? (
            <InlineNotice message="The token was rejected by the server. Check that it is correct and not expired." tone="error" />
          ) : null}

          <TokenField
            label="Bearer token"
            onChange={actions.setAuthToken}
            placeholder="Paste tenant or admin token"
            value={state.authToken}
          />

          <div className="stack-sm">
            <ToolbarButton
              className="auth-gate__submit"
              disabled={!state.authToken.trim()}
              onClick={() => void actions.loadDashboard()}
              tone="primary"
            >
              Connect
            </ToolbarButton>
            {state.authToken ? (
              <button className="auth-gate__clear" onClick={actions.clearAuthToken} type="button">
                Clear token
              </button>
            ) : null}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection eyebrow="Active session" title="Identity">
          <Surface>
            <div className="session-identity">
              <div className="session-identity__hero">
                <span className={`role-badge role-badge--${kind}`}>{role || kind}</span>
                <div className="session-identity__text">
                  <p className="session-identity__name">{name || tenant || "Authenticated"}</p>
                  <p className="session-identity__sub">
                    {[tenant && `Tenant: ${tenant}`, source && `via ${source}`].filter(Boolean).join(" · ") || "No tenant scope"}
                  </p>
                </div>
              </div>
              <DefinitionList
                compact
                items={[
                  { label: "Role", value: role || "—" },
                  { label: "Name", value: name || "Not set" },
                  { label: "Tenant", value: tenant || "Not scoped" },
                  { label: "Source", value: source || "—" },
                  { label: "Key ID", value: keyID || "—" },
                ]}
              />
            </div>
          </Surface>
        </ShellSection>

        <ShellSection eyebrow="Token management" title="Credentials">
          <Surface tone="strong">
            <div className="stack-md">
              <TokenField
                label="Bearer token"
                onChange={actions.setAuthToken}
                placeholder="Replace with a different token"
                value={state.authToken}
              />
              <div className="action-row">
                <ToolbarButton onClick={() => void actions.loadDashboard()} tone="primary">
                  Refresh session
                </ToolbarButton>
                <ToolbarButton onClick={actions.clearAuthToken} tone="danger">
                  Sign out
                </ToolbarButton>
              </div>
            </div>
          </Surface>
        </ShellSection>

        <ShellSection eyebrow="Permissions" title="Capabilities">
          <Surface>
            <div className="stack-sm">
              <p className="label-muted">What this session can do</p>
              {capabilities.length > 0 ? (
                <ul className="capability-list">
                  {capabilities.map((cap) => (
                    <li className="capability-list__item" key={cap}>
                      <span className="capability-list__dot" />
                      {cap}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="body-muted">No capabilities returned.</p>
              )}
            </div>
          </Surface>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Access restrictions" title="Scope limits">
          <Surface>
            <div className="stack-md">
              <div className="stack-sm">
                <p className="label-muted">Allowed providers</p>
                {allowedProviders.length > 0 ? (
                  <div className="chip-row">
                    {allowedProviders.map((p) => (
                      <span className="mono-chip" key={p}>{p}</span>
                    ))}
                  </div>
                ) : (
                  <div className="access-unrestricted">
                    <StatusPill label="Any provider" tone="healthy" />
                  </div>
                )}
              </div>
              <div className="stack-sm">
                <p className="label-muted">Allowed models</p>
                {allowedModels.length > 0 ? (
                  <div className="chip-row">
                    {allowedModels.slice(0, 16).map((m) => (
                      <span className="mono-chip" key={m}>{m}</span>
                    ))}
                    {allowedModels.length > 16 ? (
                      <span className="body-muted">+{allowedModels.length - 16} more</span>
                    ) : null}
                  </div>
                ) : (
                  <div className="access-unrestricted">
                    <StatusPill label="Any model" tone="healthy" />
                  </div>
                )}
              </div>
            </div>
          </Surface>
        </ShellSection>
      </aside>
    </div>
  );
}
