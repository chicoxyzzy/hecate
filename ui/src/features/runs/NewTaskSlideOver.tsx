import { useState } from "react";
import type { ModelRecord } from "../../types/runtime";
import { Icon, Icons, ModelPicker } from "../shared/ui";

export type ExecutionKind = "shell" | "git" | "file" | "agent_loop";

const KIND_LABELS: Record<ExecutionKind, string> = {
  shell: "Shell",
  git: "Git",
  file: "File",
  agent_loop: "Agent loop",
};

function KindTab({ kind, selected, onClick }: { kind: ExecutionKind; selected: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      style={{
        padding: "5px 12px",
        fontSize: 11,
        fontFamily: "var(--font-mono)",
        background: selected ? "var(--teal)" : "transparent",
        color: selected ? "var(--bg0)" : "var(--t2)",
        border: "none",
        borderRadius: "var(--radius-sm)",
        cursor: "pointer",
        transition: "background 0.1s, color 0.1s",
        fontWeight: selected ? 600 : 400,
      }}
    >
      {KIND_LABELS[kind]}
    </button>
  );
}

export type CreateTaskPayload = {
  prompt: string;
  execution_kind: ExecutionKind;
  shell_command?: string;
  git_command?: string;
  file_path?: string;
  file_content?: string;
  file_operation?: string;
  working_directory?: string;
  requested_model?: string;
};

type Props = {
  open: boolean;
  models: ModelRecord[];
  busyAction: string;
  errorMessage?: string;
  onClose: () => void;
  onCreate: (payload: CreateTaskPayload) => void;
};

export function NewTaskSlideOver({ open, models, busyAction, errorMessage, onClose, onCreate }: Props) {
  const [taskKind, setTaskKind] = useState<ExecutionKind>("shell");
  const [taskPrompt, setTaskPrompt] = useState("");
  const [taskCommand, setTaskCommand] = useState("");
  const [taskGitCommand, setTaskGitCommand] = useState("");
  const [taskWorkingDir, setTaskWorkingDir] = useState("");
  const [taskFilePath, setTaskFilePath] = useState("");
  const [taskFileContent, setTaskFileContent] = useState("");
  const [taskFileOp, setTaskFileOp] = useState("write");
  const [taskModel, setTaskModel] = useState("");

  function formIsValid(): boolean {
    if (taskKind === "shell") return taskCommand.trim() !== "";
    if (taskKind === "git") return taskGitCommand.trim() !== "";
    if (taskKind === "file") return taskFilePath.trim() !== "";
    if (taskKind === "agent_loop") return taskPrompt.trim() !== "";
    return false;
  }

  function submit() {
    const command = taskKind === "shell" ? taskCommand.trim()
      : taskKind === "git" ? taskGitCommand.trim()
      : "";
    const filePath = taskKind === "file" ? taskFilePath.trim() : "";
    if (taskKind === "shell" && !command) return;
    if (taskKind === "git" && !command) return;
    if (taskKind === "file" && !filePath) return;
    onCreate({
      prompt: taskPrompt.trim() || (taskKind === "shell" ? command : taskKind === "git" ? `git ${command}` : filePath),
      execution_kind: taskKind,
      ...(taskKind === "shell" ? { shell_command: command } : {}),
      ...(taskKind === "git" ? { git_command: command } : {}),
      ...(taskKind === "file" ? { file_path: filePath, file_content: taskFileContent, file_operation: taskFileOp } : {}),
      ...(taskWorkingDir.trim() ? { working_directory: taskWorkingDir.trim() } : {}),
      ...(taskModel ? { requested_model: taskModel } : {}),
    });
    setTaskPrompt(""); setTaskCommand(""); setTaskGitCommand(""); setTaskWorkingDir("");
    setTaskFilePath(""); setTaskFileContent(""); setTaskFileOp("write");
  }

  if (!open) return null;

  return (
    <div style={{ position: "absolute", inset: 0, zIndex: 50, display: "flex", background: "var(--scrim)" }} onClick={onClose}>
      <div style={{ marginLeft: "auto", width: 480, background: "var(--bg1)", borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", height: "100%" }} onClick={e => e.stopPropagation()}>
        <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ fontWeight: 500, fontSize: 13 }}>New task</span>
          <button className="btn btn-ghost btn-sm" style={{ marginLeft: "auto", padding: "3px 6px" }} onClick={onClose}>
            <Icon d={Icons.x} size={14} />
          </button>
        </div>
        <div style={{ padding: 16, flex: 1, display: "flex", flexDirection: "column", gap: 14, overflowY: "auto" }}>

          <div>
            <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 6, fontFamily: "var(--font-mono)" }}>EXECUTION KIND</label>
            <div style={{ display: "flex", gap: 4, background: "var(--bg2)", borderRadius: "var(--radius)", padding: 3, border: "1px solid var(--border)" }}>
              {(["shell", "git", "file", "agent_loop"] as ExecutionKind[]).map(k => (
                <KindTab key={k} kind={k} selected={taskKind === k} onClick={() => setTaskKind(k)} />
              ))}
            </div>
          </div>

          {taskKind === "shell" && (
            <div>
              <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>SHELL COMMAND <span style={{ color: "var(--red)" }}>*</span></label>
              <div style={{ display: "flex", alignItems: "center", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "0 10px" }}>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--t3)", marginRight: 6 }}>$</span>
                <input
                  className="input"
                  style={{ border: "none", background: "transparent", padding: "7px 0", flex: 1 }}
                  placeholder="ls -la / echo hello"
                  value={taskCommand}
                  onChange={e => setTaskCommand(e.target.value)}
                  onKeyDown={e => e.key === "Enter" && formIsValid() && submit()}
                />
              </div>
              <div style={{ fontSize: 10, color: "var(--amber)", fontFamily: "var(--font-mono)", marginTop: 4 }}>
                Shell execution requires approval before running.
              </div>
            </div>
          )}

          {taskKind === "git" && (
            <div>
              <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>GIT COMMAND <span style={{ color: "var(--red)" }}>*</span></label>
              <div style={{ display: "flex", alignItems: "center", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "0 10px" }}>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--t3)", marginRight: 6 }}>git</span>
                <input
                  className="input"
                  style={{ border: "none", background: "transparent", padding: "7px 0", flex: 1 }}
                  placeholder="status / log --oneline -5"
                  value={taskGitCommand}
                  onChange={e => setTaskGitCommand(e.target.value)}
                  onKeyDown={e => e.key === "Enter" && formIsValid() && submit()}
                />
              </div>
            </div>
          )}

          {taskKind === "file" && (
            <>
              <div>
                <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>OPERATION</label>
                <div style={{ display: "flex", gap: 8 }}>
                  {["write", "append"].map(op => (
                    <label key={op} style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 12, color: taskFileOp === op ? "var(--t0)" : "var(--t2)", cursor: "pointer" }}>
                      <input type="radio" checked={taskFileOp === op} onChange={() => setTaskFileOp(op)} style={{ accentColor: "var(--teal)" }} />
                      {op}
                    </label>
                  ))}
                </div>
              </div>
              <div>
                <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>FILE PATH <span style={{ color: "var(--red)" }}>*</span></label>
                <input
                  className="input"
                  placeholder="/path/to/file.txt"
                  value={taskFilePath}
                  onChange={e => setTaskFilePath(e.target.value)}
                />
              </div>
              <div>
                <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>CONTENT</label>
                <textarea
                  className="input"
                  placeholder="File content…"
                  rows={4}
                  style={{ resize: "vertical" }}
                  value={taskFileContent}
                  onChange={e => setTaskFileContent(e.target.value)}
                />
              </div>
            </>
          )}

          {taskKind === "agent_loop" && (
            <div>
              <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>PROMPT <span style={{ color: "var(--red)" }}>*</span></label>
              <textarea
                className="input"
                placeholder="Describe the task…"
                rows={4}
                style={{ resize: "vertical" }}
                value={taskPrompt}
                onChange={e => setTaskPrompt(e.target.value)}
              />
            </div>
          )}

          {(taskKind === "shell" || taskKind === "git") && (
            <div>
              <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>WORKING DIRECTORY</label>
              <input
                className="input"
                placeholder=". (default)"
                value={taskWorkingDir}
                onChange={e => setTaskWorkingDir(e.target.value)}
              />
            </div>
          )}

          {taskKind !== "agent_loop" && (
            <div>
              <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>DESCRIPTION <span style={{ color: "var(--t3)" }}>(optional)</span></label>
              <input
                className="input"
                placeholder="Human-readable description…"
                value={taskPrompt}
                onChange={e => setTaskPrompt(e.target.value)}
              />
            </div>
          )}

          <div>
            <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>MODEL</label>
            <ModelPicker value={taskModel} onChange={setTaskModel} models={models} />
          </div>

          {errorMessage && (
            <div style={{ fontSize: 12, color: "var(--red)", fontFamily: "var(--font-mono)" }}>{errorMessage}</div>
          )}
        </div>
        <div style={{ padding: "12px 16px", borderTop: "1px solid var(--border)", display: "flex", gap: 8 }}>
          <button className="btn btn-primary" style={{ flex: 1, justifyContent: "center" }}
            disabled={!formIsValid() || busyAction === "create"}
            onClick={submit}>
            <Icon d={Icons.send} size={14} /> {busyAction === "create" ? "Creating…" : "Queue task"}
          </button>
          <button className="btn" onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  );
}
