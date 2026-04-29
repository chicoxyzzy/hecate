import { useMemo, useState } from "react";

import { ConsoleShell, getAvailableWorkspaces, type WorkspaceID } from "./AppShell";
import { useRuntimeConsole } from "./useRuntimeConsole";

const WORKSPACE_STORAGE_KEY = "hecate.workspace";

export default function App() {
  const { state, actions } = useRuntimeConsole();
  // The operator's *preferred* workspace, hydrated from localStorage. We
  // never overwrite this with a fallback — only the explicit selection
  // path persists. Resolving the actually-active workspace happens at
  // render time below; this lets the saved "admin" choice survive a
  // refresh even when the session takes a tick to load and reveal
  // isAdmin. Without this, the "admin not in workspaces yet" branch
  // would clobber the saved value on every reload.
  const [preferredWorkspace, setPreferredWorkspace] = useState<WorkspaceID>(() => {
    const saved = localStorage.getItem(WORKSPACE_STORAGE_KEY);
    return (saved as WorkspaceID | null) ?? "chats";
  });

  const workspaces = useMemo(() => getAvailableWorkspaces(state.session.isAdmin), [state.session.isAdmin]);

  // Resolve preferred → actually-rendered workspace. If the preferred
  // one is currently available (e.g. session has loaded and admin is
  // visible), use it. Otherwise fall back to "overview" without
  // disturbing the persisted preference.
  const activeWorkspace: WorkspaceID =
    workspaces.some(w => w.id === preferredWorkspace) ? preferredWorkspace : "overview";

  function handleSelectWorkspace(id: WorkspaceID) {
    localStorage.setItem(WORKSPACE_STORAGE_KEY, id);
    setPreferredWorkspace(id);
  }

  return <ConsoleShell actions={actions} activeWorkspace={activeWorkspace} onSelectWorkspace={handleSelectWorkspace} state={state} />;
}
