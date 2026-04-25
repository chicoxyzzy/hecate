import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  cancelTaskRun, createTask, deleteTask, getModels, getTaskApprovals, getTaskRunArtifacts,
  getTaskRuns, getTaskRunSteps, getTasks, resolveTaskApproval,
  retryTaskRun, resumeTaskRun, startTask, streamTaskRun,
} from "../../lib/api";
import type { ModelRecord, TaskApprovalRecord, TaskArtifactRecord, TaskRecord, TaskRunRecord, TaskStepRecord } from "../../types/runtime";
import { Badge, Dot, Icon, Icons, ModelPicker } from "../shared/ui";

type Props = {
  authToken: string;
  session: { isAuthenticated: boolean };
};

type StreamState = "idle" | "connecting" | "live" | "closed" | "error";
type ExecutionKind = "shell" | "git" | "file" | "agent_loop";

const STEP_STATUS_COLOR: Record<string, string> = {
  completed: "var(--green)",
  running:   "var(--teal)",
  awaiting_approval: "var(--amber)",
  failed:    "var(--red)",
  cancelled: "var(--red)",
};
function stepColor(status: string) { return STEP_STATUS_COLOR[status] || "var(--t3)"; }

function taskBadgeStatus(status: string): string {
  if (status === "completed") return "done";
  if (status === "awaiting_approval") return "awaiting";
  return status;
}

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

export function TasksView({ authToken, session }: Props) {
  const [loading, setLoading] = useState(true);
  const [tasks, setTasks] = useState<TaskRecord[]>([]);
  const [selectedTaskID, setSelectedTaskID] = useState("");
  const [runs, setRuns] = useState<TaskRunRecord[]>([]);
  const [selectedRunID, setSelectedRunID] = useState("");
  const [approvals, setApprovals] = useState<TaskApprovalRecord[]>([]);
  const [steps, setSteps] = useState<TaskStepRecord[]>([]);
  const [artifacts, setArtifacts] = useState<TaskArtifactRecord[]>([]);
  const [streamState, setStreamState] = useState<StreamState>("idle");
  const [busyAction, setBusyAction] = useState("");
  const [notice, setNotice] = useState<{ tone: "success" | "error"; message: string } | null>(null);
  const [newTaskOpen, setNewTaskOpen] = useState(false);

  // New task form state
  const [taskKind, setTaskKind] = useState<ExecutionKind>("shell");
  const [taskPrompt, setTaskPrompt] = useState("");
  const [taskCommand, setTaskCommand] = useState("");
  const [taskGitCommand, setTaskGitCommand] = useState("");
  const [taskWorkingDir, setTaskWorkingDir] = useState("");
  const [taskFilePath, setTaskFilePath] = useState("");
  const [taskFileContent, setTaskFileContent] = useState("");
  const [taskFileOp, setTaskFileOp] = useState("write");
  const [taskModel, setTaskModel] = useState("");
  const [availableModels, setAvailableModels] = useState<ModelRecord[]>([]);

  const termRef = useRef<HTMLDivElement>(null);
  const streamCursorRef = useRef(0);

  const selectedTask = useMemo(() => tasks.find(t => t.id === selectedTaskID) ?? null, [tasks, selectedTaskID]);
  const selectedRun = useMemo(() => runs.find(r => r.id === selectedRunID) ?? null, [runs, selectedRunID]);
  const pendingApprovals = useMemo(() => approvals.filter(a => a.status === "pending"), [approvals]);
  const stdoutArtifact = useMemo(() => artifacts.find(a => a.kind === "stdout") ?? null, [artifacts]);
  const stderrArtifact = useMemo(() => artifacts.find(a => a.kind === "stderr") ?? null, [artifacts]);

  const resetRunDetail = useCallback(() => {
    setSteps([]);
    setArtifacts([]);
    streamCursorRef.current = 0;
  }, []);

  const loadRunDetail = useCallback(async (taskID: string, runID: string) => {
    if (!taskID || !runID) { resetRunDetail(); return; }
    const [stepsRes, artifactsRes] = await Promise.all([
      getTaskRunSteps(taskID, runID, authToken),
      getTaskRunArtifacts(taskID, runID, authToken),
    ]);
    setSteps(stepsRes.data ?? []);
    setArtifacts(artifactsRes.data ?? []);
  }, [authToken, resetRunDetail]);

  const loadTaskDetail = useCallback(async (taskID: string, preferredRunID = "") => {
    if (!taskID) return;
    const [runsRes, approvalsRes] = await Promise.all([
      getTaskRuns(taskID, authToken),
      getTaskApprovals(taskID, authToken),
    ]);
    const nextRuns = runsRes.data ?? [];
    setRuns(nextRuns);
    setApprovals(approvalsRes.data ?? []);
    const nextRunID = (preferredRunID && nextRuns.some(r => r.id === preferredRunID) ? preferredRunID : "") || nextRuns[0]?.id || "";
    setSelectedRunID(nextRunID);
    if (nextRunID) await loadRunDetail(taskID, nextRunID);
    else resetRunDetail();
  }, [authToken, loadRunDetail, resetRunDetail]);

  const loadTasks = useCallback(async (preferredTaskID = "", preferredRunID = "") => {
    if (!session.isAuthenticated) { setTasks([]); setLoading(false); return; }
    setLoading(true);
    try {
      const res = await getTasks(authToken, 30);
      const nextTasks = res.data ?? [];
      setTasks(nextTasks);
      const nextTaskID = (preferredTaskID && nextTasks.some(t => t.id === preferredTaskID) ? preferredTaskID : "") || nextTasks[0]?.id || "";
      setSelectedTaskID(nextTaskID);
      if (nextTaskID) await loadTaskDetail(nextTaskID, preferredRunID);
    } catch { /* silently ignore */ }
    finally { setLoading(false); }
  }, [authToken, loadTaskDetail, session.isAuthenticated]);

  useEffect(() => { void loadTasks(); }, [loadTasks]);

  useEffect(() => {
    if (!session.isAuthenticated) return;
    getModels(authToken).then(res => setAvailableModels(res.data ?? [])).catch(() => {});
  }, [authToken, session.isAuthenticated]);

  // Stream selected run
  useEffect(() => {
    if (!selectedTaskID || !selectedRunID || !session.isAuthenticated) {
      setStreamState(selectedRunID ? "closed" : "idle");
      return;
    }
    const controller = new AbortController();
    setStreamState("connecting");

    void streamTaskRun(
      selectedTaskID, selectedRunID, authToken,
      ({ payload }) => {
        setStreamState("live");
        streamCursorRef.current = payload.data.sequence ?? streamCursorRef.current;
        setRuns(cur => {
          const others = cur.filter(r => r.id !== payload.data.run.id);
          return [payload.data.run, ...others];
        });
        setSteps(payload.data.steps ?? []);
        setArtifacts(payload.data.artifacts ?? []);
        setTasks(cur => cur.map(t => t.id === selectedTaskID ? { ...t, status: payload.data.run.status } : t));
      },
      streamCursorRef.current,
      controller.signal,
    ).then(() => {
      if (!controller.signal.aborted) {
        setStreamState("closed");
        void loadTaskDetail(selectedTaskID, selectedRunID);
      }
    }).catch((err) => {
      if (!controller.signal.aborted) {
        setStreamState("error");
        console.error(err);
      }
    });

    return () => controller.abort();
  }, [authToken, loadTaskDetail, selectedRunID, selectedTaskID, session.isAuthenticated]);

  useEffect(() => {
    if (termRef.current) termRef.current.scrollTop = termRef.current.scrollHeight;
  }, [stdoutArtifact]);

  async function handleSelectTask(taskID: string) {
    setSelectedTaskID(taskID);
    resetRunDetail();
    setNotice(null);
    try { await loadTaskDetail(taskID); } catch { /* ignore */ }
  }

  async function handleResolveApproval(approval: TaskApprovalRecord, decision: "approve" | "reject") {
    if (!selectedTaskID) return;
    setBusyAction(decision);
    try {
      await resolveTaskApproval(selectedTaskID, approval.id, { decision }, authToken);
      setNotice({ tone: "success", message: decision === "approve" ? "Approved." : "Denied." });
      await loadTaskDetail(selectedTaskID, approval.run_id);
    } catch (err) {
      setNotice({ tone: "error", message: err instanceof Error ? err.message : "failed" });
    } finally { setBusyAction(""); }
  }

  async function handleCancelRun() {
    if (!selectedTaskID || !selectedRunID) return;
    setBusyAction("cancel");
    try {
      await cancelTaskRun(selectedTaskID, selectedRunID, authToken);
      await loadTaskDetail(selectedTaskID, selectedRunID);
    } catch { /* ignore */ }
    finally { setBusyAction(""); }
  }

  async function handleRetryRun() {
    if (!selectedTaskID || !selectedRunID) return;
    setBusyAction("retry");
    try {
      const res = await retryTaskRun(selectedTaskID, selectedRunID, authToken);
      await loadTasks(selectedTaskID, res.data.id);
    } catch { /* ignore */ }
    finally { setBusyAction(""); }
  }

  async function handleResumeRun() {
    if (!selectedTaskID || !selectedRunID) return;
    setBusyAction("resume");
    try {
      const res = await resumeTaskRun(selectedTaskID, selectedRunID, authToken);
      await loadTasks(selectedTaskID, res.data.id);
    } catch { /* ignore */ }
    finally { setBusyAction(""); }
  }

  async function handleDeleteTask(taskID: string) {
    setBusyAction("delete:" + taskID);
    try {
      await deleteTask(taskID, authToken);
      const nextTasks = tasks.filter(t => t.id !== taskID);
      setTasks(nextTasks);
      if (selectedTaskID === taskID) {
        const next = nextTasks[0]?.id ?? "";
        setSelectedTaskID(next);
        if (next) await loadTaskDetail(next);
        else resetRunDetail();
      }
    } catch (err) {
      setNotice({ tone: "error", message: err instanceof Error ? err.message : "delete failed" });
    } finally { setBusyAction(""); }
  }

  async function handleCreateTask() {
    const command = taskKind === "shell" ? taskCommand.trim()
      : taskKind === "git" ? taskGitCommand.trim()
      : "";
    const filePath = taskKind === "file" ? taskFilePath.trim() : "";
    if (taskKind === "shell" && !command) return;
    if (taskKind === "git" && !command) return;
    if (taskKind === "file" && !filePath) return;

    setBusyAction("create");
    try {
      const payload = {
        prompt: taskPrompt.trim() || (taskKind === "shell" ? command : taskKind === "git" ? `git ${command}` : filePath),
        execution_kind: taskKind,
        ...(taskKind === "shell" ? { shell_command: command } : {}),
        ...(taskKind === "git" ? { git_command: command } : {}),
        ...(taskKind === "file" ? { file_path: filePath, file_content: taskFileContent, file_operation: taskFileOp } : {}),
        ...(taskWorkingDir.trim() ? { working_directory: taskWorkingDir.trim() } : {}),
        ...(taskModel ? { requested_model: taskModel } : {}),
      };
      const created = await createTask(payload, authToken);
      const started = await startTask(created.data.id, authToken);
      // Reset form
      setTaskPrompt(""); setTaskCommand(""); setTaskGitCommand(""); setTaskWorkingDir("");
      setTaskFilePath(""); setTaskFileContent(""); setTaskFileOp("write");
      setNewTaskOpen(false);
      await loadTasks(created.data.id, started.data.id);
    } catch (err) {
      setNotice({ tone: "error", message: err instanceof Error ? err.message : "failed to create task" });
    } finally { setBusyAction(""); }
  }

  function formIsValid(): boolean {
    if (taskKind === "shell") return taskCommand.trim() !== "";
    if (taskKind === "git") return taskGitCommand.trim() !== "";
    if (taskKind === "file") return taskFilePath.trim() !== "";
    if (taskKind === "agent_loop") return taskPrompt.trim() !== "";
    return false;
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

  if (!session.isAuthenticated) {
    return (
      <div style={{ display: "flex", height: "100%", alignItems: "center", justifyContent: "center" }}>
        <div style={{ textAlign: "center", color: "var(--t2)", fontSize: 13 }}>
          <div style={{ marginBottom: 8 }}>Authentication required</div>
          <div style={{ fontSize: 11, color: "var(--t3)" }}>Add a bearer token in Access to manage tasks.</div>
        </div>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", height: "100%", overflow: "hidden", position: "relative" }}>
      {/* Task list */}
      <div style={{ width: 300, borderRight: "1px solid var(--border)", display: "flex", flexDirection: "column", flexShrink: 0 }}>
        <div style={{ padding: 8, borderBottom: "1px solid var(--border)", display: "flex", gap: 6, background: "var(--bg1)" }}>
          <button className="btn btn-primary btn-sm" style={{ flex: 1, justifyContent: "center" }} onClick={() => setNewTaskOpen(true)}>
            <Icon d={Icons.plus} size={13} /> New task
          </button>
          <button className="btn btn-ghost btn-sm" onClick={() => void loadTasks(selectedTaskID, selectedRunID)} title="Refresh">
            <Icon d={Icons.refresh} size={13} />
          </button>
        </div>
        <div style={{ flex: 1, overflowY: "auto" }}>
          {loading && <div style={{ padding: "16px 12px", fontSize: 12, color: "var(--t3)" }}>Loading…</div>}
          {!loading && tasks.length === 0 && (
            <div style={{ padding: "24px 12px", textAlign: "center", fontSize: 12, color: "var(--t3)" }}>No tasks yet. Create one above.</div>
          )}
          {tasks.map(t => (
            <div key={t.id} onClick={() => void handleSelectTask(t.id)}
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
                    onClick={e => { e.stopPropagation(); void handleDeleteTask(t.id); }}
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

      {/* Task detail */}
      {selectedTask ? (
        <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", minWidth: 0 }}>
          {/* Detail topbar */}
          <div style={{ height: "var(--topbar-h)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 16px", gap: 10, flexShrink: 0, background: "var(--bg1)" }}>
            <Badge status={taskBadgeStatus(selectedTask.status)} />
            <span style={{ fontWeight: 500, fontSize: 13, color: "var(--t0)", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {selectedTask.title || selectedTask.prompt || "Untitled"}
            </span>
            {selectedRun?.model && <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)" }}>{selectedRun.model}</span>}
            <div style={{ display: "flex", gap: 6 }}>
              {(selectedRun?.status === "queued" || selectedRun?.status === "running" || selectedRun?.status === "awaiting_approval") && (
                <button className="btn btn-danger btn-sm" disabled={busyAction === "cancel"} onClick={() => void handleCancelRun()}>Cancel</button>
              )}
              {(selectedRun?.status === "failed" || selectedRun?.status === "cancelled") && (
                <>
                  <button className="btn btn-sm" disabled={busyAction !== ""} onClick={() => void handleRetryRun()}>Retry</button>
                  <button className="btn btn-sm" disabled={busyAction !== ""} onClick={() => void handleResumeRun()}>Resume</button>
                </>
              )}
            </div>
          </div>

          {notice && (
            <div style={{ padding: "6px 16px", fontSize: 12, fontFamily: "var(--font-mono)", background: notice.tone === "success" ? "var(--green-bg)" : "var(--red-bg)", color: notice.tone === "success" ? "var(--green)" : "var(--red)", borderBottom: "1px solid var(--border)" }}>
              {notice.message}
            </div>
          )}

          <div style={{ flex: 1, overflowY: "auto", display: "flex", flexDirection: "column" }}>
            {/* Approval gate */}
            {pendingApprovals.map(approval => (
              <div key={approval.id} style={{ margin: "14px 16px", border: "1px solid var(--amber-border)", borderRadius: "var(--radius)", background: "var(--amber-bg)", overflow: "hidden" }}>
                <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--amber-border)", display: "flex", alignItems: "center", gap: 8 }}>
                  <Icon d={Icons.warning} size={15} />
                  <span style={{ fontWeight: 500, color: "var(--amber)", fontSize: 13 }}>Approval required</span>
                  <span style={{ fontSize: 11, color: "var(--amber-lo)", fontFamily: "var(--font-mono)", marginLeft: "auto" }}>{approval.kind}</span>
                </div>
                <div style={{ padding: "12px 14px" }}>
                  {approval.reason && (
                    <div style={{ fontSize: 12, color: "var(--amber)", marginBottom: 10 }}>
                      <Icon d={Icons.info} size={13} /> {approval.reason}
                    </div>
                  )}
                  {/* Show what will run */}
                  {(selectedTask.shell_command || selectedTask.git_command) && (
                    <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t1)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "6px 10px", marginBottom: 10 }}>
                      {selectedTask.execution_kind === "git" ? `git ${selectedTask.git_command}` : selectedTask.shell_command}
                    </div>
                  )}
                  <div style={{ display: "flex", gap: 8 }}>
                    <button className="btn btn-primary btn-sm" disabled={busyAction !== ""} onClick={() => void handleResolveApproval(approval, "approve")} style={{ gap: 5 }}>
                      <Icon d={Icons.approve} size={13} /> Approve & run
                    </button>
                    <button className="btn btn-danger btn-sm" disabled={busyAction !== ""} onClick={() => void handleResolveApproval(approval, "reject")} style={{ gap: 5 }}>
                      <Icon d={Icons.deny} size={13} /> Deny
                    </button>
                  </div>
                </div>
              </div>
            ))}

            {/* Step timeline */}
            {steps.length > 0 && (
              <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)" }}>
                <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 8, letterSpacing: "0.06em", textTransform: "uppercase" }}>Steps</div>
                <div style={{ display: "flex", flexDirection: "column" }}>
                  {steps.map((step, i) => (
                    <div key={step.id} style={{ display: "flex", alignItems: "center", gap: 10, padding: "5px 0", position: "relative" }}>
                      {i < steps.length - 1 && (
                        <div style={{ position: "absolute", left: 6, top: "50%", width: 1, height: "100%", background: "var(--border)", zIndex: 0 }} />
                      )}
                      <div style={{
                        width: 13, height: 13, borderRadius: "50%", background: stepColor(step.status), flexShrink: 0, zIndex: 1,
                        boxShadow: step.status === "running" ? `0 0 8px ${stepColor(step.status)}` : "none",
                      }} />
                      <span style={{ fontSize: 12, color: (step.status === "queued" || !step.status) ? "var(--t3)" : "var(--t0)", flex: 1 }}>
                        {step.title || step.kind || step.tool_name || "step"}
                      </span>
                      {step.exit_code !== undefined && step.exit_code !== 0 && step.status !== "running" && (
                        <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--red)" }}>exit {step.exit_code}</span>
                      )}
                      {step.started_at && step.status === "completed" && (
                        <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                          {new Date(step.started_at).toLocaleTimeString()}
                        </span>
                      )}
                      {step.status === "running" && <span className="badge badge-teal" style={{ fontSize: 10, animation: "pulse 1.5s infinite" }}>running</span>}
                      {step.status === "awaiting_approval" && <span className="badge badge-amber" style={{ fontSize: 10 }}>awaiting</span>}
                      {step.status === "failed" && step.error && (
                        <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--red)", maxWidth: 120, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={step.error}>
                          {step.error}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Terminal output */}
            <div style={{ flex: 1, display: "flex", flexDirection: "column", minHeight: 180 }}>
              <div style={{ padding: "8px 16px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8, background: "var(--bg1)" }}>
                <Icon d={Icons.terminal} size={13} />
                <span style={{ fontSize: 11, color: "var(--t2)", fontFamily: "var(--font-mono)" }}>stdout</span>
                {streamState === "live" && <Dot color="green" pulse />}
                {streamState === "connecting" && <Dot color="amber" pulse />}
                {stderrArtifact?.content_text && (
                  <span style={{ fontSize: 10, color: "var(--red)", fontFamily: "var(--font-mono)", marginLeft: "auto" }}>stderr available</span>
                )}
              </div>
              <div ref={termRef} style={{ flex: 1, overflowY: "auto", padding: "10px 16px", background: "var(--bg0)", fontFamily: "var(--font-mono)", fontSize: 12, lineHeight: 1.8 }}>
                {stdoutArtifact?.content_text ? (
                  stdoutArtifact.content_text.split("\n").map((line, i) => (
                    <div key={i} style={{ color: "var(--t1)" }}>{line || " "}</div>
                  ))
                ) : (
                  <div style={{ color: "var(--t3)" }}>
                    {selectedRun?.status === "queued" ? "Waiting in queue…"
                      : selectedRun?.status === "running" ? "Running…"
                      : selectedRun?.status === "awaiting_approval" ? "Awaiting approval…"
                      : "No output."}
                  </div>
                )}
                {stderrArtifact?.content_text && (
                  <>
                    <div style={{ color: "var(--t3)", marginTop: 8, borderTop: "1px solid var(--border)", paddingTop: 8 }}>— stderr —</div>
                    {stderrArtifact.content_text.split("\n").map((line, i) => (
                      <div key={i} style={{ color: "var(--red)" }}>{line || " "}</div>
                    ))}
                  </>
                )}
                {(selectedTask.status === "running") && (
                  <div style={{ color: "var(--teal)", animation: "blink 0.8s step-end infinite" }}>█</div>
                )}
              </div>
            </div>

            {/* Artifacts */}
            {artifacts.filter(a => a.kind !== "stdout" && a.kind !== "stderr").length > 0 && (
              <div style={{ padding: "10px 16px", borderTop: "1px solid var(--border)", display: "flex", flexWrap: "wrap", gap: 6, background: "var(--bg1)" }}>
                <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", alignSelf: "center", marginRight: 4, letterSpacing: "0.06em", textTransform: "uppercase" }}>artifacts</span>
                {artifacts.filter(a => a.kind !== "stdout" && a.kind !== "stderr").map(a => (
                  <div key={a.id} style={{ display: "flex", alignItems: "center", gap: 6, background: "var(--bg3)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "3px 8px" }}>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t0)" }}>{a.name || a.kind}</span>
                    {a.size_bytes != null && a.size_bytes > 0 && <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--green)" }}>{a.size_bytes}b</span>}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      ) : (
        <div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center" }}>
          <div style={{ textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
            {loading ? "Loading…" : "Select a task to inspect."}
          </div>
        </div>
      )}

      {/* New task slide-over */}
      {newTaskOpen && (
        <div style={{ position: "absolute", inset: 0, zIndex: 50, display: "flex", background: "oklch(0 0 0 / 0.5)" }} onClick={() => setNewTaskOpen(false)}>
          <div style={{ marginLeft: "auto", width: 480, background: "var(--bg1)", borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", height: "100%" }} onClick={e => e.stopPropagation()}>
            <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ fontWeight: 500, fontSize: 13 }}>New task</span>
              <button className="btn btn-ghost btn-sm" style={{ marginLeft: "auto", padding: "3px 6px" }} onClick={() => setNewTaskOpen(false)}>
                <Icon d={Icons.x} size={14} />
              </button>
            </div>
            <div style={{ padding: 16, flex: 1, display: "flex", flexDirection: "column", gap: 14, overflowY: "auto" }}>

              {/* Execution kind tabs */}
              <div>
                <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 6, fontFamily: "var(--font-mono)" }}>EXECUTION KIND</label>
                <div style={{ display: "flex", gap: 4, background: "var(--bg2)", borderRadius: "var(--radius)", padding: 3, border: "1px solid var(--border)" }}>
                  {(["shell", "git", "file", "agent_loop"] as ExecutionKind[]).map(k => (
                    <KindTab key={k} kind={k} selected={taskKind === k} onClick={() => setTaskKind(k)} />
                  ))}
                </div>
              </div>

              {/* Shell command */}
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
                      onKeyDown={e => e.key === "Enter" && formIsValid() && void handleCreateTask()}
                    />
                  </div>
                  <div style={{ fontSize: 10, color: "var(--amber)", fontFamily: "var(--font-mono)", marginTop: 4 }}>
                    Shell execution requires approval before running.
                  </div>
                </div>
              )}

              {/* Git command */}
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
                      onKeyDown={e => e.key === "Enter" && formIsValid() && void handleCreateTask()}
                    />
                  </div>
                </div>
              )}

              {/* File operation */}
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

              {/* Agent loop prompt */}
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

              {/* Working directory (shell & git) */}
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

              {/* Optional description (non-agent) */}
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
                <ModelPicker value={taskModel} onChange={setTaskModel} models={availableModels} />
              </div>

              {notice && notice.tone === "error" && (
                <div style={{ fontSize: 12, color: "var(--red)", fontFamily: "var(--font-mono)" }}>{notice.message}</div>
              )}
            </div>
            <div style={{ padding: "12px 16px", borderTop: "1px solid var(--border)", display: "flex", gap: 8 }}>
              <button className="btn btn-primary" style={{ flex: 1, justifyContent: "center" }}
                disabled={!formIsValid() || busyAction === "create"}
                onClick={() => void handleCreateTask()}>
                <Icon d={Icons.send} size={14} /> {busyAction === "create" ? "Creating…" : "Queue task"}
              </button>
              <button className="btn" onClick={() => setNewTaskOpen(false)}>Cancel</button>
            </div>
          </div>
        </div>
      )}

      <style>{`
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }
        @keyframes blink  { 0%,100%{opacity:1} 50%{opacity:0} }
      `}</style>
    </div>
  );
}
