import { AccessView } from "../features/access/AccessView";
import { AdminView } from "../features/admin/AdminView";
import { OverviewView } from "../features/overview/OverviewView";
import { PlaygroundView } from "../features/playground/PlaygroundView";
import { ProvidersView } from "../features/providers/ProvidersView";
import { RunsView } from "../features/runs/RunsView";
import { StatusPill, ToolbarButton } from "../features/shared/ConsolePrimitives";
import { titleFromKind } from "../lib/format";
import type { RuntimeConsoleViewModel } from "./useRuntimeConsole";

export type WorkspaceID = "overview" | "runs" | "playground" | "providers" | "access" | "admin";

type WorkspaceDefinition = {
  id: WorkspaceID;
  label: string;
  description: string;
};

type ConsoleState = RuntimeConsoleViewModel["state"];
type ConsoleActions = RuntimeConsoleViewModel["actions"];

const baseWorkspaces: WorkspaceDefinition[] = [
  { id: "overview", label: "Overview", description: "Runtime picture" },
  { id: "runs", label: "Runs", description: "Watch task execution" },
  { id: "playground", label: "Playground", description: "Run requests" },
  { id: "providers", label: "Providers", description: "Inspect routing surfaces" },
  { id: "access", label: "Access", description: "Auth and restrictions" },
];

const adminWorkspace: WorkspaceDefinition = {
  id: "admin",
  label: "Admin",
  description: "Budget and control plane",
};

export function getAvailableWorkspaces(isAdmin: boolean): WorkspaceDefinition[] {
  return isAdmin ? [...baseWorkspaces, adminWorkspace] : baseWorkspaces;
}

export function getWorkspaceTitle(workspace: WorkspaceID): string {
  switch (workspace) {
    case "overview":
      return "Overview";
    case "runs":
      return "Live runs";
    case "playground":
      return "Playground";
    case "providers":
      return "Providers and models";
    case "access":
      return "Access and restrictions";
    case "admin":
      return "Admin operations";
  }
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

  return (
    <div className="console-root">
      <div className="console-backdrop" />
      <div className="console-layout">
        <ConsoleSidebar
          activeWorkspace={activeWorkspace}
          onSelectWorkspace={onSelectWorkspace}
          runtimeHeaders={state.runtimeHeaders}
          healthStatus={state.health?.status}
          session={state.session}
          workspaces={workspaces}
          onRefresh={actions.loadDashboard}
        />

        <main className="console-main">
          <ConsoleHeader
            activeWorkspace={activeWorkspace}
            onRefresh={actions.loadDashboard}
            runtimeHeaders={state.runtimeHeaders}
          />
          <ConsoleNoticeBanner error={state.error} notice={state.notice} onDismiss={actions.dismissNotice} />
          <WorkspaceContent activeWorkspace={activeWorkspace} onSelectWorkspace={onSelectWorkspace} state={state} actions={actions} />
        </main>
      </div>
    </div>
  );
}

function ConsoleSidebar({
  activeWorkspace,
  onSelectWorkspace,
  runtimeHeaders,
  healthStatus,
  session,
  workspaces,
  onRefresh,
}: {
  activeWorkspace: WorkspaceID;
  onSelectWorkspace: (workspace: WorkspaceID) => void;
  runtimeHeaders: ConsoleState["runtimeHeaders"];
  healthStatus: ConsoleState["health"] extends { status: infer T } ? T : string | undefined;
  session: ConsoleState["session"];
  workspaces: WorkspaceDefinition[];
  onRefresh: () => Promise<void>;
}) {
  return (
    <aside className="console-sidebar" role="complementary" aria-label="Sidebar">
      <div className="console-brand">
        <p className="console-brand__eyebrow">Developer Console</p>
        <h1 className="console-brand__title">Hecate</h1>
        <p className="console-brand__detail">Routing, cache, traces, budgets, tenants.</p>
      </div>

      <button
        className="console-sidebar__status console-sidebar__session-btn"
        onClick={() => onSelectWorkspace("access")}
        title="Go to Access settings"
        type="button"
      >
        <StatusPill label={titleFromKind(healthStatus)} tone={healthStatus === "ok" ? "healthy" : "warning"} />
        <StatusPill label={session.label} tone={sessionKindTone(session.kind)} />
        {(session.kind === "anonymous" || session.kind === "invalid") ? (
          <span className="sidebar-cta">Set up access →</span>
        ) : null}
      </button>

      <nav className="workspace-nav" aria-label="Workspace navigation">
        {workspaces.map((workspace) => (
          <button
            aria-current={activeWorkspace === workspace.id ? "page" : undefined}
            className={activeWorkspace === workspace.id ? "workspace-nav__item workspace-nav__item--active" : "workspace-nav__item"}
            key={workspace.id}
            onClick={() => onSelectWorkspace(workspace.id)}
            type="button"
          >
            <span>{workspace.label}</span>
            <small>{workspace.description}</small>
          </button>
        ))}
      </nav>

      <div className="console-sidebar__footer">
        <ToolbarButton onClick={() => void onRefresh()} tone="primary">
          Refresh everything
        </ToolbarButton>
        <p className="console-sidebar__meta">Last trace request: {runtimeHeaders?.requestId || "none yet"}</p>
      </div>
    </aside>
  );
}

function ConsoleHeader({
  activeWorkspace,
  onRefresh,
  runtimeHeaders,
}: {
  activeWorkspace: WorkspaceID;
  onRefresh: () => Promise<void>;
  runtimeHeaders: ConsoleState["runtimeHeaders"];
}) {
  return (
    <header className="console-header">
      <div>
        <p className="console-eyebrow">Runtime workspace</p>
        <h2 className="console-header__title">{getWorkspaceTitle(activeWorkspace)}</h2>
        <div className="console-header__chips">
          <StatusPill
            label={runtimeHeaders?.provider ? `${runtimeHeaders.provider} / ${runtimeHeaders.resolvedModel || runtimeHeaders.requestedModel}` : "No active route"}
            tone="neutral"
          />
          <StatusPill label={runtimeHeaders?.cacheType ? `Cache ${runtimeHeaders.cacheType}` : "Ready"} tone="neutral" />
        </div>
      </div>

      <div className="console-header__actions">
        <ToolbarButton onClick={() => void onRefresh()} tone="primary">
          Refresh
        </ToolbarButton>
      </div>
    </header>
  );
}

function ConsoleNoticeBanner({
  error,
  notice,
  onDismiss,
}: {
  error: string;
  notice: ConsoleState["notice"];
  onDismiss: () => void;
}) {
  if (error) {
    return <div className="page-banner page-banner--error">{error}</div>;
  }

  if (!notice) {
    return null;
  }

  return (
    <div className={notice.kind === "success" ? "page-banner page-banner--success" : "page-banner page-banner--error"}>
      <span>{notice.message}</span>
      <button className="page-banner__dismiss" onClick={onDismiss} type="button">
        Dismiss
      </button>
    </div>
  );
}

function WorkspaceContent({
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
  return (
    <div className="console-content">
      {activeWorkspace === "overview" ? <OverviewView actions={actions} onOpenWorkspace={onSelectWorkspace} state={state} /> : null}
      {activeWorkspace === "runs" ? <RunsView authToken={state.authToken} session={state.session} /> : null}
      {activeWorkspace === "playground" ? <PlaygroundView actions={actions} state={state} /> : null}
      {activeWorkspace === "providers" ? <ProvidersView actions={actions} state={state} /> : null}
      {activeWorkspace === "access" ? <AccessView actions={actions} state={state} /> : null}
      {activeWorkspace === "admin" && state.session.isAdmin ? <AdminView actions={actions} state={state} /> : null}
    </div>
  );
}

function sessionKindTone(kind: ConsoleState["session"]["kind"]): "healthy" | "neutral" | "danger" | "warning" {
  switch (kind) {
    case "admin":
      return "healthy";
    case "tenant":
      return "neutral";
    case "invalid":
      return "danger";
    case "anonymous":
      return "warning";
  }
}
