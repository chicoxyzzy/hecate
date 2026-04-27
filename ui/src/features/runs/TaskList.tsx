import type { TaskRecord } from "../../types/runtime";
import { Badge, Icon, Icons } from "../shared/ui";

type Props = {
  tasks: TaskRecord[];
  selectedTaskID: string;
  loading: boolean;
  busyAction: string;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
  onNewTask: () => void;
  onRefresh: () => void;
};

function taskBadgeStatus(status: string): string {
  if (status === "completed") return "done";
  if (status === "awaiting_approval") return "awaiting";
  return status;
}

function taskKindLabel(task: TaskRecord): string {
  const kind = task.execution_kind;
  if (!kind) return "";
  if (kind === "shell") return task.shell_command ? `$ ${task.shell_command}` : "shell";
  if (kind === "git") return task.git_command ? `git ${task.git_command}` : "git";
  if (kind === "file") return task.file_path ? task.file_path : "file";
  if (kind === "agent_loop") return "agent";
  return kind;
}

export function TaskList({ tasks, selectedTaskID, loading, busyAction, onSelect, onDelete, onNewTask, onRefresh }: Props) {
  return (
    <div style={{ width: 300, borderRight: "1px solid var(--border)", display: "flex", flexDirection: "column", flexShrink: 0 }}>
      <div style={{ padding: 8, borderBottom: "1px solid var(--border)", display: "flex", gap: 6, background: "var(--bg1)" }}>
        <button className="btn btn-primary btn-sm" style={{ flex: 1, justifyContent: "center" }} onClick={onNewTask}>
          <Icon d={Icons.plus} size={13} /> New task
        </button>
        <button className="btn btn-ghost btn-sm" onClick={onRefresh} title="Refresh">
          <Icon d={Icons.refresh} size={13} />
        </button>
      </div>
      <div style={{ flex: 1, overflowY: "auto" }}>
        {loading && <div style={{ padding: "16px 12px", fontSize: 12, color: "var(--t3)" }}>Loading…</div>}
        {!loading && tasks.length === 0 && (
          <div style={{ padding: "24px 12px", textAlign: "center", fontSize: 12, color: "var(--t3)" }}>No tasks yet. Create one above.</div>
        )}
        {tasks.map(t => (
          <div key={t.id} onClick={() => onSelect(t.id)}
            style={{
              padding: "10px 12px", cursor: "pointer",
              borderBottom: "1px solid var(--border)",
              borderLeft: selectedTaskID === t.id ? "2px solid var(--teal)" : "2px solid transparent",
              background: selectedTaskID === t.id ? "var(--bg2)" : "transparent",
              transition: "background 0.1s",
            }}>
            <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 4 }}>
              <Badge status={taskBadgeStatus(t.status)} />
              {t.execution_kind && (
                <span style={{ fontSize: 9, color: "var(--teal)", fontFamily: "var(--font-mono)", background: "var(--teal-bg, oklch(0.2 0.04 190))", padding: "1px 5px", borderRadius: 3 }}>
                  {t.execution_kind}
                </span>
              )}
              <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginLeft: "auto" }}>
                {t.step_count ?? 0} steps
              </span>
              {t.status !== "running" && (
                <button
                  className="btn btn-ghost btn-sm"
                  style={{ padding: "1px 3px", color: "var(--red)" }}
                  title="Delete"
                  disabled={busyAction === "delete:" + t.id}
                  onClick={e => { e.stopPropagation(); onDelete(t.id); }}
                >
                  <Icon d={Icons.trash} size={10} />
                </button>
              )}
            </div>
            <div style={{ fontSize: 12, color: "var(--t0)", lineHeight: 1.4, fontWeight: 500, overflow: "hidden", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical" } as React.CSSProperties}>
              {t.title || t.prompt || "Untitled task"}
            </div>
            {taskKindLabel(t) && (
              <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginTop: 2, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {taskKindLabel(t)}
              </div>
            )}
            <div style={{ fontSize: 10, color: "var(--t2)", fontFamily: "var(--font-mono)", marginTop: 2 }}>
              {t.latest_run_id ? `run: ${t.latest_run_id.slice(0, 8)}` : "not started"}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
