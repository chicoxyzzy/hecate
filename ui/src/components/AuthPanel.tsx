import { Panel } from "./Panel";
import { SessionRestrictions } from "./SessionRestrictions";

type AuthPanelProps = {
  authToken: string;
  inputClassName: string;
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

const toneBySessionKind: Record<AuthPanelProps["sessionKind"], string> = {
  anonymous: "border-slate-200 bg-white/70 text-slate-700",
  tenant: "border-cyan-200 bg-cyan-50 text-cyan-900",
  admin: "border-emerald-200 bg-emerald-50 text-emerald-900",
  invalid: "border-red-200 bg-red-50 text-red-800",
};

export function AuthPanel(props: AuthPanelProps) {
  return (
    <Panel eyebrow="Auth" title="Session and access">
      <div className="mt-4 grid gap-4">
        <div className={`rounded-2xl border px-4 py-4 ${toneBySessionKind[props.sessionKind]}`}>
          <p className="text-sm font-medium uppercase tracking-[0.16em]">Current session</p>
          <p className="mt-2 text-2xl font-semibold">{props.sessionLabel}</p>
          <p className="mt-2 text-sm opacity-80">
            {props.sessionKind === "admin"
              ? "Admin endpoints and operator actions are available."
              : props.sessionKind === "tenant"
                ? "Playground and model catalog access are available with the current token."
                : props.sessionKind === "invalid"
                  ? "The current token did not unlock the expected runtime views."
                  : "Add a bearer token to unlock runtime and admin capabilities."}
          </p>
        </div>

        <label>
          <span className="mb-2 block text-sm text-slate-600">Bearer token</span>
          <input
            className={props.inputClassName}
            placeholder="Admin token or tenant API key"
            value={props.authToken}
            onChange={(event) => props.onAuthTokenChange(event.target.value)}
          />
        </label>

        <div className="flex flex-wrap gap-2">
          <button
            className="inline-flex rounded-full bg-slate-900 px-4 py-3 text-sm font-semibold text-white transition hover:-translate-y-0.5"
            onClick={() => void props.onRefresh()}
            type="button"
          >
            Refresh session
          </button>
          <button
            className="inline-flex rounded-full border border-slate-200/80 bg-white px-4 py-3 text-sm font-medium text-slate-900 transition hover:-translate-y-0.5"
            onClick={props.onClearAuthToken}
            type="button"
          >
            Clear token
          </button>
        </div>

        <div className="rounded-2xl bg-slate-50/90 p-4">
          <h3 className="text-sm font-semibold uppercase tracking-[0.16em] text-slate-500">Identity</h3>
          <dl className="mt-3 grid gap-2 text-sm text-slate-700">
            <IdentityRow label="Role" value={props.sessionRole || "anonymous"} />
            <IdentityRow label="Name" value={props.sessionName || "n/a"} />
            <IdentityRow label="Tenant" value={props.sessionTenant || "n/a"} />
            <IdentityRow label="Source" value={props.sessionSource || "n/a"} />
            <IdentityRow label="Key ID" value={props.sessionKeyID || "n/a"} />
          </dl>
        </div>

        <div className="rounded-2xl bg-slate-50/90 p-4">
          <h3 className="text-sm font-semibold uppercase tracking-[0.16em] text-slate-500">What this session can do</h3>
          <ul className="mt-3 grid gap-2 text-sm text-slate-700">
            {props.sessionCapabilities.map((item) => (
              <li className="rounded-xl bg-white px-3 py-2" key={item}>
                {item}
              </li>
            ))}
          </ul>
        </div>

        <SessionRestrictions
          allowedModels={props.sessionAllowedModels}
          allowedProviders={props.sessionAllowedProviders}
          className="grid gap-3 rounded-2xl bg-slate-50/90 p-4 text-sm text-slate-700"
          title="Session restrictions"
        />
      </div>
    </Panel>
  );
}

function IdentityRow(props: { label: string; value: string }) {
  return (
    <div className="flex items-start justify-between gap-3 rounded-xl bg-white px-3 py-2">
      <dt className="text-slate-500">{props.label}</dt>
      <dd className="text-right font-medium text-slate-900">{props.value}</dd>
    </div>
  );
}
