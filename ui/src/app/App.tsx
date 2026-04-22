import { useEffect, useMemo, useState } from "react";

import { AccessView } from "../features/access/AccessView";
import { AdminView } from "../features/admin/AdminView";
import { OverviewView } from "../features/overview/OverviewView";
import { PlaygroundView } from "../features/playground/PlaygroundView";
import { ProvidersView } from "../features/providers/ProvidersView";
import { StatusPill, ToolbarButton } from "../features/shared/ConsolePrimitives";
import { titleFromKind } from "../lib/format";
import { useRuntimeConsole } from "./useRuntimeConsole";

type WorkspaceID = "overview" | "playground" | "providers" | "access" | "admin";

export default function App() {
  const { state, actions } = useRuntimeConsole();
  const [activeWorkspace, setActiveWorkspace] = useState<WorkspaceID>("overview");

  const workspaces = useMemo(
    () =>
      [
        { id: "overview", label: "Overview", description: "Runtime picture" },
        { id: "playground", label: "Playground", description: "Run requests" },
        { id: "providers", label: "Providers", description: "Inspect routing surfaces" },
        { id: "access", label: "Access", description: "Auth and restrictions" },
        ...(state.session.isAdmin
          ? ([{ id: "admin", label: "Admin", description: "Budget and control plane" }] satisfies Array<{
              id: WorkspaceID;
              label: string;
              description: string;
            }>)
          : []),
      ] satisfies Array<{ id: WorkspaceID; label: string; description: string }>,
    [state.session.isAdmin],
  );

  useEffect(() => {
    if (workspaces.some((workspace) => workspace.id === activeWorkspace)) {
      return;
    }
    setActiveWorkspace("overview");
  }, [activeWorkspace, workspaces]);

  return (
    <div className="console-root">
      <div className="console-backdrop" />
      <div className="console-layout">
        <aside className="console-sidebar" role="complementary" aria-label="Sidebar">
          <div className="console-brand">
            <p className="console-brand__eyebrow">Developer Console</p>
            <h1 className="console-brand__title">Hecate</h1>
            <p className="console-brand__detail">Routing, cache, traces, budgets, tenants.</p>
          </div>

          <div className="console-sidebar__status">
            <StatusPill label={titleFromKind(state.health?.status)} tone={state.health?.status === "ok" ? "healthy" : "warning"} />
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

          <nav className="workspace-nav" aria-label="Workspace navigation">
            {workspaces.map((workspace) => (
              <button
                aria-current={activeWorkspace === workspace.id ? "page" : undefined}
                className={activeWorkspace === workspace.id ? "workspace-nav__item workspace-nav__item--active" : "workspace-nav__item"}
                key={workspace.id}
                onClick={() => setActiveWorkspace(workspace.id)}
                type="button"
              >
                <span>{workspace.label}</span>
                <small>{workspace.description}</small>
              </button>
            ))}
          </nav>

          <div className="console-sidebar__footer">
            <ToolbarButton onClick={() => void actions.loadDashboard()} tone="primary">
              Refresh everything
            </ToolbarButton>
            <p className="console-sidebar__meta">Last trace request: {state.runtimeHeaders?.requestId || "none yet"}</p>
          </div>
        </aside>

        <main className="console-main">
          <header className="console-header">
            <div>
              <p className="console-eyebrow">Runtime workspace</p>
              <h2 className="console-header__title">{workspaceTitle(activeWorkspace)}</h2>
              <div className="console-header__chips">
                <StatusPill label={state.runtimeHeaders?.provider ? `${state.runtimeHeaders.provider} / ${state.runtimeHeaders.resolvedModel || state.runtimeHeaders.requestedModel}` : "No active route"} tone="neutral" />
                <StatusPill label={state.runtimeHeaders?.cacheType ? `Cache ${state.runtimeHeaders.cacheType}` : "Ready"} tone="neutral" />
              </div>
            </div>
            <div className="console-header__actions">
              <ToolbarButton onClick={() => void actions.loadDashboard()} tone="primary">
                Refresh
              </ToolbarButton>
            </div>
          </header>

          {state.error ? <div className="page-banner page-banner--error">{state.error}</div> : null}
          {state.notice ? (
            <div className={state.notice.kind === "success" ? "page-banner page-banner--success" : "page-banner page-banner--error"}>
              <span>{state.notice.message}</span>
              <button className="page-banner__dismiss" onClick={actions.dismissNotice} type="button">
                Dismiss
              </button>
            </div>
          ) : null}

          <div className="console-content">
            {activeWorkspace === "overview" ? <OverviewView actions={actions} onOpenWorkspace={setActiveWorkspace} state={state} /> : null}
            {activeWorkspace === "playground" ? <PlaygroundView actions={actions} state={state} /> : null}
            {activeWorkspace === "providers" ? <ProvidersView actions={actions} state={state} /> : null}
            {activeWorkspace === "access" ? <AccessView actions={actions} state={state} /> : null}
            {activeWorkspace === "admin" && state.session.isAdmin ? <AdminView actions={actions} state={state} /> : null}
          </div>
        </main>
      </div>
    </div>
  );
}

function workspaceTitle(workspace: WorkspaceID): string {
  switch (workspace) {
    case "overview":
      return "Overview";
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
