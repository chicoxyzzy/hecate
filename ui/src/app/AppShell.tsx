import { useEffect, useState } from "react";

import { AdminView } from "../features/admin/AdminView";
import { CostsView } from "../features/costs/CostsView";
import { ObservabilityView } from "../features/overview/ObservabilityView";
import { ChatView } from "../features/chats/ChatView";
import { ProvidersView } from "../features/providers/ProvidersView";
import { TasksView } from "../features/runs/TasksView";
import { InlineError } from "../features/shared/ui";
import type { RuntimeConsoleViewModel } from "./useRuntimeConsole";

export type WorkspaceID = "overview" | "runs" | "chats" | "providers" | "costs" | "admin";

type WorkspaceDefinition = {
  id: WorkspaceID;
  label: string;
  icon: React.ReactNode;
  shortcut: string;
};

type ConsoleState = RuntimeConsoleViewModel["state"];
type ConsoleActions = RuntimeConsoleViewModel["actions"];

// Icon paths match the design handoff
const IC = {
  observe: ["M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z", "M15 12a3 3 0 11-6 0 3 3 0 016 0z"],
  chat:    "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z",
  tasks:   "M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4",
  providers: ["M5 12h14","M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2","M9 10h.01","M9 16h.01"],
  keys:    "M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z",
  budgets: "M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z",
  // Stack-of-coins outline. Three stacked ellipses with side rails —
  // visually distinct from IC.budgets (the dollar-circle) so the
  // activity bar doesn't have two near-identical glyphs.
  costs:   ["M4 7c0-1.657 3.582-3 8-3s8 1.343 8 3-3.582 3-8 3-8-1.343-8-3z", "M4 7v5c0 1.657 3.582 3 8 3s8-1.343 8-3V7", "M4 12v5c0 1.657 3.582 3 8 3s8-1.343 8-3v-5"],
};

function SvgIcon({ d, size = 18 }: { d: string | string[]; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0 }}>
      {Array.isArray(d) ? d.map((p, i) => <path key={i} d={p} />) : <path d={d} />}
    </svg>
  );
}

// Workspace order is role-aware. The admin lineup is:
//   Chats (1) · Providers (2) · Tasks (3) · Observability (4) · Costs (5) · Settings (6)
// Tenants drop Providers and Settings (they don't have CP store access),
// but keep Costs so they can monitor their own spend:
//   Chats (1) · Tasks (2) · Observability (3) · Costs (4)
// Anonymous fallthrough (TokenGate normally blocks this) is just
// [chats] so the activity bar isn't empty if a session somehow slips
// through.
//
// Note: the workspace id stays "admin" for back-compat with operator
// localStorage values from earlier builds; only the rendered label
// changes to "Settings".
type WorkspaceLineupEntry = Omit<WorkspaceDefinition, "shortcut">;
const WS: Record<WorkspaceID, WorkspaceLineupEntry> = {
  chats:     { id: "chats",     label: "Chats",         icon: <SvgIcon d={IC.chat} /> },
  providers: { id: "providers", label: "Providers",     icon: <SvgIcon d={IC.providers} /> },
  runs:      { id: "runs",      label: "Tasks",         icon: <SvgIcon d={IC.tasks} /> },
  overview:  { id: "overview",  label: "Observability", icon: <SvgIcon d={IC.observe} /> },
  costs:     { id: "costs",     label: "Costs",         icon: <SvgIcon d={IC.costs} /> },
  admin:     { id: "admin",     label: "Settings",      icon: <SvgIcon d={IC.keys} /> },
};

const BARE_WORKSPACES: WorkspaceID[] = ["chats", "runs"];

type SessionRole = "anonymous" | "tenant" | "admin";

export function getAvailableWorkspaces(
  isAdminOrLegacy: boolean | { isAdmin: boolean; isAuthenticated: boolean },
): WorkspaceDefinition[] {
  // Back-compat: tests historically passed a single boolean. Map it
  // to the equivalent role so the new role-aware lineup applies.
  const role: SessionRole = typeof isAdminOrLegacy === "boolean"
    ? (isAdminOrLegacy ? "admin" : "tenant")
    : isAdminOrLegacy.isAdmin
      ? "admin"
      : isAdminOrLegacy.isAuthenticated
        ? "tenant"
        : "anonymous";
  let lineup: WorkspaceLineupEntry[];
  if (role === "admin") {
    lineup = [WS.chats, WS.providers, WS.runs, WS.overview, WS.costs, WS.admin];
  } else if (role === "tenant") {
    lineup = [WS.chats, WS.runs, WS.overview, WS.costs];
  } else {
    lineup = [WS.chats];
  }
  // Shortcut keys are positional (1..N) — keeps the keyboard contract
  // the same regardless of which workspaces the role can see.
  return lineup.map((ws, i) => ({ ...ws, shortcut: String(i + 1) }));
}

// TokenGate is the first-run / forgotten-token landing screen. The gateway
// auto-generates an admin bearer token on first start and prints it to the
// server logs; the operator pastes it here once and we persist it in
// localStorage. We render this gate whenever no token is set so the rest of
// the console doesn't 401-spin behind the scenes. Visually it mirrors the
// rest of the console: same .card, .input, .btn-primary classes, same
// monospace section labels, and the shared InlineError for the validation
// message.
function TokenGate({
  onSubmit,
  rejected,
}: {
  onSubmit: (token: string) => void;
  rejected?: boolean;
}) {
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = value.trim();
    if (!trimmed) {
      setError("Paste the token from your gateway logs to continue.");
      return;
    }
    onSubmit(trimmed);
  };
  const displayedError =
    error || (rejected ? "The saved token was rejected by the gateway. Paste a fresh one." : "");
  return (
    <div className="hecate-shell" style={{
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
      padding: 16,
    }}>
      <form
        onSubmit={handleSubmit}
        className="card"
        style={{
          width: "100%",
          maxWidth: "32rem",
          padding: 20,
          display: "flex",
          flexDirection: "column",
          gap: 14,
        }}
      >
        <div style={{
          fontSize: 11,
          color: "var(--t3)",
          fontFamily: "var(--font-mono)",
          letterSpacing: "0.06em",
          textTransform: "uppercase",
        }}>
          Authentication
        </div>

        <h1 style={{
          fontSize: 18,
          fontWeight: 600,
          color: "var(--t0)",
          margin: 0,
          lineHeight: 1.3,
        }}>
          Admin token required
        </h1>

        <p style={{
          color: "var(--t2)",
          fontSize: 13,
          lineHeight: 1.55,
          margin: 0,
        }}>
          The gateway auto-generates an admin bearer token on first start.
          Find it in the server logs (look for the
          {" "}<code style={{
            background: "var(--bg3)",
            border: "1px solid var(--border)",
            borderRadius: "var(--radius-sm)",
            padding: "1px 5px",
            fontFamily: "var(--font-mono)",
            fontSize: 12,
            color: "var(--t1)",
          }}>Hecate first-run setup</code>{" "}
          banner) or read it from the bootstrap file. We'll remember it in
          this browser so you only paste it once.
        </p>

        <label
          htmlFor="hecate-admin-token"
          style={{
            fontSize: 11,
            color: "var(--t3)",
            fontFamily: "var(--font-mono)",
            letterSpacing: "0.06em",
            textTransform: "uppercase",
          }}
        >
          Admin bearer token
        </label>
        <input
          id="hecate-admin-token"
          className="input"
          aria-label="Admin bearer token"
          autoFocus
          spellCheck={false}
          autoComplete="off"
          type="password"
          value={value}
          onChange={(e) => { setValue(e.target.value); setError(""); }}
          placeholder="Paste token here"
          style={{ fontFamily: "var(--font-mono)" }}
        />

        {displayedError && <InlineError message={displayedError} />}

        <button
          type="submit"
          className="btn btn-primary"
          style={{ alignSelf: "flex-start" }}
        >
          Connect
        </button>
      </form>
    </div>
  );
}

export function ConsoleShell({
  activeWorkspace,
  onSelectWorkspace,
  state,
  actions,
}: {
  activeWorkspace: WorkspaceID;
  onSelectWorkspace: (workspace: WorkspaceID) => void;
  state: ConsoleState;
  actions: ConsoleActions;
}) {
  // Block the rest of the console behind the token gate when no admin token
  // is set OR when the saved token was rejected on the most recent attempt.
  // The gateway always requires a token now, so an empty token == new user;
  // a rejected token (from a stale localStorage entry, a rotated key, or a
  // gateway reset) re-prompts with an inline explanation rather than dumping
  // the operator into a 401-spinning workspace.
  if (!state.authToken) {
    return <TokenGate onSubmit={actions.setAuthToken} />;
  }
  // While the very first dashboard load is in flight we don't yet know if
  // the saved token is valid. Render a quiet placeholder instead of
  // flashing the workspace with stale state — once /healthz returns, the
  // session check below routes to TokenGate(rejected) or the workspace.
  // `state.health` stays populated across subsequent reloads, so this
  // splash only shows on the very first load after a refresh.
  if (state.health === null && !state.error) {
    return <AuthLoadingShell />;
  }
  if (state.session.kind === "invalid") {
    return <TokenGate onSubmit={actions.setAuthToken} rejected />;
  }
  return (
    <AuthenticatedShell
      activeWorkspace={activeWorkspace}
      onSelectWorkspace={onSelectWorkspace}
      state={state}
      actions={actions}
    />
  );
}

function AuthLoadingShell() {
  return (
    <div
      className="hecate-shell"
      style={{
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 16,
      }}
    >
      <div
        style={{
          fontSize: 11,
          color: "var(--t3)",
          fontFamily: "var(--font-mono)",
          letterSpacing: "0.06em",
          textTransform: "uppercase",
        }}
      >
        Connecting…
      </div>
    </div>
  );
}

function AuthenticatedShell({
  activeWorkspace,
  onSelectWorkspace,
  state,
  actions,
}: {
  activeWorkspace: WorkspaceID;
  onSelectWorkspace: (workspace: WorkspaceID) => void;
  state: ConsoleState;
  actions: ConsoleActions;
}) {
  const workspaces = getAvailableWorkspaces({
    isAdmin: state.session.isAdmin,
    isAuthenticated: state.session.isAuthenticated,
  });

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLSelectElement) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const idx = parseInt(e.key) - 1;
      if (idx >= 0 && idx < workspaces.length) onSelectWorkspace(workspaces[idx].id);
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [workspaces, onSelectWorkspace]);

  const isBare = BARE_WORKSPACES.includes(activeWorkspace);

  return (
    <div className="hecate-shell">
      <div className="hecate-workarea">
        {/* Activity bar */}
        <nav className="hecate-activitybar" aria-label="Workspace navigation">
          {workspaces.map(ws => {
            const noProviders = ws.id === "chats" && (state.adminConfig?.providers?.length ?? 0) === 0;
            return (
              <button key={ws.id}
                aria-label={`${ws.label} (${ws.shortcut})`}
                aria-current={activeWorkspace === ws.id ? "page" : undefined}
                className={`hecate-activitybtn${activeWorkspace === ws.id ? " hecate-activitybtn--active" : ""}`}
                onClick={() => onSelectWorkspace(ws.id)}
                title={noProviders ? "Add a provider first" : `${ws.label} — press ${ws.shortcut}`}
                style={noProviders ? { opacity: 0.4, pointerEvents: "none" } : undefined}
                type="button">
                {ws.icon}
                <span className="hecate-activitybtn__key">{ws.shortcut}</span>
              </button>
            );
          })}
        </nav>

        {/* Main content */}
        <main className="hecate-content">
          {state.error && <div className="page-banner page-banner--error">{state.error}</div>}
          <div className={`console-content${isBare ? " console-content--bare" : ""}`}>
            {activeWorkspace === "overview"   && <ObservabilityView actions={actions} state={state} onNavigate={onSelectWorkspace} />}
            {activeWorkspace === "chats" && (
              (state.adminConfig?.providers?.length ?? 0) === 0 ? (
                <div style={{ display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", height: "100%", gap: 8 }}>
                  <div style={{ fontSize: 14, color: "var(--t1)", fontWeight: 500 }}>No providers configured</div>
                  <div style={{ fontSize: 12, color: "var(--t3)" }}>Add a provider to start chatting</div>
                  <button className="btn btn-primary btn-sm" style={{ marginTop: 8 }} onClick={() => onSelectWorkspace("providers")}>
                    Go to Providers
                  </button>
                </div>
              ) : (
                <ChatView actions={actions} state={state} />
              )
            )}
            {activeWorkspace === "runs"          && <TasksView authToken={state.authToken} session={state.session} />}
            {activeWorkspace === "providers"     && <ProvidersView actions={actions} state={state} />}
            {activeWorkspace === "costs"         && <CostsView actions={actions} state={state} />}
            {activeWorkspace === "admin" && state.session.isAdmin && <AdminView actions={actions} state={state} />}
          </div>
        </main>
      </div>

      {/* Status bar */}
      <div className="hecate-statusbar">
        <span className="hecate-statusbar__brand">hecate</span>
        {state.health?.version && (
          <>
            <span className="hecate-statusbar__sep">|</span>
            <span style={{ fontFamily: "var(--font-mono)" }}>{state.health.version}</span>
          </>
        )}
        <span className="hecate-statusbar__sep">|</span>
        <span>{state.session.label}</span>
        <span className="hecate-statusbar__sep">|</span>
        {/* "configured" = providers in the CP store (operator-added).
            "models" is intersected with the configured set so the count
            reflects models the operator can actually route to from the
            chat picker — env-only models would inflate the number
            without being selectable. Tenant sessions (no adminConfig)
            see the unfiltered model list since the runtime is their
            only source of truth. */}
        {(() => {
          const configured = state.adminConfig?.providers ?? null;
          const configuredCount = configured?.length ?? 0;
          const modelCount = configured
            ? state.models.filter(m => {
                const p = m.metadata?.provider;
                return typeof p === "string" && configured.some(c => c.id === p);
              }).length
            : state.models.length;
          return (
            <>
              <span>{configuredCount} configured</span>
              <span className="hecate-statusbar__sep">|</span>
              <span>{modelCount} models</span>
            </>
          );
        })()}
      </div>

      {/* Toast notifications */}
      {!state.error && state.notice && (
        <div className={`toast toast--${state.notice.kind}`} role="alert">
          <span>{state.notice.message}</span>
          <button className="toast__dismiss" onClick={actions.dismissNotice} type="button">✕</button>
        </div>
      )}
    </div>
  );
}
