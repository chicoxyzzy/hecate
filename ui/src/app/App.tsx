import { useEffect, useMemo, useState } from "react";

import { ConsoleShell, getAvailableWorkspaces, type WorkspaceID } from "./AppShell";
import { useRuntimeConsole } from "./useRuntimeConsole";

export default function App() {
  const { state, actions } = useRuntimeConsole();
  const [activeWorkspace, setActiveWorkspace] = useState<WorkspaceID>("overview");

  const workspaces = useMemo(() => getAvailableWorkspaces(state.session.isAdmin), [state.session.isAdmin]);

  useEffect(() => {
    if (workspaces.some((workspace) => workspace.id === activeWorkspace)) {
      return;
    }
    setActiveWorkspace("overview");
  }, [activeWorkspace, workspaces]);

  return <ConsoleShell actions={actions} activeWorkspace={activeWorkspace} onSelectWorkspace={setActiveWorkspace} state={state} />;
}
