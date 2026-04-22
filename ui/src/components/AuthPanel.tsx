import { Panel } from "./Panel";
import { SessionRestrictions } from "./SessionRestrictions";

type AuthPanelProps = {
  authToken: string;
  sessionAllowedModels: string[];
  sessionAllowedProviders: string[];
  sessionCapabilities: string[];
  sessionKeyID: string;
  sessionKind: "anonymous" | "tenant" | "admin" | "invalid";
  sessionLabel: string;
  sessionName: string;
  sessionRole: string;
  sessionSource: string;
  sessionTenant: string;
  onAuthTokenChange: (value: string) => void;
  onClearAuthToken: () => void;
  onRefresh: () => void | Promise<void>;
};

export function AuthPanel(props: AuthPanelProps) {
  return (
    <Panel eyebrow="Auth" title="Session and access">
      <div className="stack-lg">
        <div className={`session-badge session-badge--${props.sessionKind}`}>
          <p className="session-badge__eyebrow">Current session</p>
          <p className="session-badge__label">{props.sessionLabel}</p>
          <p className="session-badge__description">
            {props.sessionKind === "admin"
              ? "Admin endpoints and operator actions are available."
              : props.sessionKind === "tenant"
                ? "Playground and model catalog access are available with the current token."
                : props.sessionKind === "invalid"
                  ? "The current token did not unlock the expected runtime views."
                  : "Add a bearer token to unlock runtime and admin capabilities."}
          </p>
        </div>

        <label className="field">
          <span className="field__label">Bearer token</span>
          <input
            className="field__input"
            placeholder="Admin token or tenant API key"
            value={props.authToken}
            onChange={(event) => props.onAuthTokenChange(event.target.value)}
          />
        </label>

        <div className="action-row">
          <button className="toolbar-button toolbar-button--primary" onClick={() => void props.onRefresh()} type="button">
            Refresh session
          </button>
          <button className="toolbar-button" onClick={props.onClearAuthToken} type="button">
            Clear token
          </button>
        </div>

        <div className="info-block">
          <h3 className="info-block__title">Identity</h3>
          <dl className="info-list">
            <IdentityRow label="Role" value={props.sessionRole || "anonymous"} />
            <IdentityRow label="Name" value={props.sessionName || "n/a"} />
            <IdentityRow label="Tenant" value={props.sessionTenant || "n/a"} />
            <IdentityRow label="Source" value={props.sessionSource || "n/a"} />
            <IdentityRow label="Key ID" value={props.sessionKeyID || "n/a"} />
          </dl>
        </div>

        <div className="info-block">
          <h3 className="info-block__title">What this session can do</h3>
          <ul className="info-list">
            {props.sessionCapabilities.map((item) => (
              <li className="info-row" key={item}>
                {item}
              </li>
            ))}
          </ul>
        </div>

        <SessionRestrictions
          allowedModels={props.sessionAllowedModels}
          allowedProviders={props.sessionAllowedProviders}
          className="info-block info-block--warning"
          title="Session restrictions"
        />
      </div>
    </Panel>
  );
}

function IdentityRow(props: { label: string; value: string }) {
  return (
    <div className="info-row">
      <dt>{props.label}</dt>
      <dd>{props.value}</dd>
    </div>
  );
}
