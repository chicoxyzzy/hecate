import { useEffect, useMemo, useState } from "react";

import { ConsoleShell, getAvailableWorkspaces, type WorkspaceID } from "./AppShell";
import { useRuntimeConsole } from "./useRuntimeConsole";

export default function App() {
  const { state, actions } = useRuntimeConsole();
  const [activeWorkspace, setActiveWorkspace] = useState<WorkspaceID>(() => {
    const saved = localStorage.getItem("hecate.workspace") as WorkspaceID | null;
    return saved ?? "playground";
  });

  const workspaces = useMemo(() => getAvailableWorkspaces(state.session.isAdmin), [state.session.isAdmin]);

  useEffect(() => {
    if (workspaces.some((workspace) => workspace.id === activeWorkspace)) {
      return;
    }
    setActiveWorkspace("overview");
  }, [activeWorkspace, workspaces]);

  function handleSelectWorkspace(id: WorkspaceID) {
    localStorage.setItem("hecate.workspace", id);
    setActiveWorkspace(id);
  }

  return <ConsoleShell actions={actions} activeWorkspace={activeWorkspace} onSelectWorkspace={handleSelectWorkspace} state={state} />;
}
