import { useEffect } from "react";

import { AdminView } from "../features/admin/AdminView";
import { ObservabilityView } from "../features/overview/ObservabilityView";
import { ChatView } from "../features/playground/ChatView";
import { ProvidersView } from "../features/providers/ProvidersView";
import { TasksView } from "../features/runs/TasksView";
import type { RuntimeConsoleViewModel } from "./useRuntimeConsole";

export type WorkspaceID = "overview" | "runs" | "playground" | "providers" | "admin";

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
};

function SvgIcon({ d, size = 18 }: { d: string | string[]; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" style={{ flexShrink: 0 }}>
      {Array.isArray(d) ? d.map((p, i) => <path key={i} d={p} />) : <path d={d} />}
    </svg>
  );
}

const baseWorkspaces: WorkspaceDefinition[] = [
  { id: "playground", label: "Chat",      shortcut: "1", icon: <SvgIcon d={IC.chat} /> },
  { id: "overview",   label: "Observe",   shortcut: "2", icon: <SvgIcon d={IC.observe} /> },
  { id: "runs",       label: "Tasks",     shortcut: "3", icon: <SvgIcon d={IC.tasks} /> },
  { id: "providers",  label: "Providers", shortcut: "4", icon: <SvgIcon d={IC.providers} /> },
];

const adminWorkspace: WorkspaceDefinition = {
  id: "admin", label: "Admin", shortcut: "5", icon: <SvgIcon d={IC.keys} />,
};

const BARE_WORKSPACES: WorkspaceID[] = ["playground", "runs"];

export function getAvailableWorkspaces(isAdmin: boolean): WorkspaceDefinition[] {
  return isAdmin ? [...baseWorkspaces, adminWorkspace] : baseWorkspaces;
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
  const workspaces = getAvailableWorkspaces(state.session.isAdmin);

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
          {workspaces.map(ws => (
            <button key={ws.id}
              aria-label={`${ws.label} (${ws.shortcut})`}
              aria-current={activeWorkspace === ws.id ? "page" : undefined}
              className={`hecate-activitybtn${activeWorkspace === ws.id ? " hecate-activitybtn--active" : ""}`}
              onClick={() => onSelectWorkspace(ws.id)}
              title={`${ws.label} — press ${ws.shortcut}`}
              type="button">
              {ws.icon}
              <span className="hecate-activitybtn__key">{ws.shortcut}</span>
            </button>
          ))}
        </nav>

        {/* Main content */}
        <main className="hecate-content">
          {state.error && <div className="page-banner page-banner--error">{state.error}</div>}
          <div className={`console-content${isBare ? " console-content--bare" : ""}`}>
            {activeWorkspace === "overview"   && <ObservabilityView actions={actions} state={state} />}
            {activeWorkspace === "playground" && <ChatView actions={actions} state={state} />}
            {activeWorkspace === "runs"       && <TasksView authToken={state.authToken} session={state.session} />}
            {activeWorkspace === "providers"  && <ProvidersView actions={actions} state={state} />}
            {activeWorkspace === "admin" && state.session.isAdmin && <AdminView actions={actions} state={state} />}
          </div>
        </main>
      </div>

      {/* Status bar */}
      <div className="hecate-statusbar">
        <span className="hecate-statusbar__brand">hecate</span>
        <span className="hecate-statusbar__sep">|</span>
        <span>{state.session.label}</span>
        <span className="hecate-statusbar__sep">|</span>
        <span>{state.healthyProviders}/{state.providers.length} providers</span>
        <span className="hecate-statusbar__sep">|</span>
        <span>{state.models.length} models</span>
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
