import { useCallback, useEffect, useMemo, useState } from "react";

import {
  cancelTaskRun,
  createTask,
  getTask,
  getTaskApprovals,
  getTaskRunArtifacts,
  getTaskRuns,
  getTaskRunSteps,
  getTasks,
  resolveTaskApproval,
  startTask,
  streamTaskRun,
} from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type {
  TaskApprovalRecord,
  TaskArtifactRecord,
  TaskRecord,
  TaskRunRecord,
  TaskStepRecord,
} from "../../types/runtime";
import { DefinitionList, EmptyState, InlineNotice, MetricTile, SelectField, ShellSection, StatusPill, Surface, TextAreaField, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type SessionState = {
  isAuthenticated: boolean;
};

type Props = {
  authToken: string;
  session: SessionState;
};

type StreamState = "idle" | "connecting" | "live" | "closed" | "error";

export function RunsView({ authToken, session }: Props) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [tasks, setTasks] = useState<TaskRecord[]>([]);
  const [selectedTaskID, setSelectedTaskID] = useState("");
  const [runs, setRuns] = useState<TaskRunRecord[]>([]);
  const [selectedRunID, setSelectedRunID] = useState("");
  const [approvals, setApprovals] = useState<TaskApprovalRecord[]>([]);
  const [steps, setSteps] = useState<TaskStepRecord[]>([]);
  const [artifacts, setArtifacts] = useState<TaskArtifactRecord[]>([]);
  const [streamState, setStreamState] = useState<StreamState>("idle");
  const [notice, setNotice] = useState<{ tone: "success" | "error"; message: string } | null>(null);
  const [busyAction, setBusyAction] = useState<"" | "approve" | "reject" | "cancel" | "create" | "create_start" | "start">("");
  const [taskTitle, setTaskTitle] = useState("");
  const [taskPrompt, setTaskPrompt] = useState("");
  const [executionKind, setExecutionKind] = useState("stub");
  const [workingDirectory, setWorkingDirectory] = useState("");
  const [shellCommand, setShellCommand] = useState("");
  const [gitCommand, setGitCommand] = useState("");
  const [fileOperation, setFileOperation] = useState("write");
  const [filePath, setFilePath] = useState("");
  const [fileContent, setFileContent] = useState("");

  const selectedRun = useMemo(() => runs.find((run) => run.id === selectedRunID) ?? null, [runs, selectedRunID]);
  const pendingApprovals = useMemo(
    () => approvals.filter((approval) => approval.status === "pending" && (!selectedRunID || approval.run_id === selectedRunID)),
    [approvals, selectedRunID],
  );
  const stdoutArtifact = useMemo(() => artifacts.find((artifact) => artifact.kind === "stdout") ?? null, [artifacts]);
  const stderrArtifact = useMemo(() => artifacts.find((artifact) => artifact.kind === "stderr") ?? null, [artifacts]);

  const loadRunDetail = useCallback(
    async (taskID: string, runID: string) => {
      if (!taskID || !runID) {
        setSteps([]);
        setArtifacts([]);
        return;
      }
      const [stepsResponse, artifactsResponse] = await Promise.all([
        getTaskRunSteps(taskID, runID, authToken),
        getTaskRunArtifacts(taskID, runID, authToken),
      ]);
      setSteps(stepsResponse.data ?? []);
      setArtifacts(artifactsResponse.data ?? []);
    },
    [authToken],
  );

  const loadTasks = useCallback(
    async (preferredTaskID = "", preferredRunID = "") => {
      if (!session.isAuthenticated) {
        setTasks([]);
        setSelectedTaskID("");
        setRuns([]);
        setSelectedRunID("");
        setApprovals([]);
        setSteps([]);
        setArtifacts([]);
        setLoading(false);
        setStreamState("idle");
        return;
      }
      setLoading(true);
      setError("");
      try {
        const tasksResponse = await getTasks(authToken, 20);
        const nextTasks = tasksResponse.data ?? [];
        setTasks(nextTasks);
        const nextTaskID =
          (preferredTaskID && nextTasks.some((task) => task.id === preferredTaskID) ? preferredTaskID : "") ||
          (selectedTaskID && nextTasks.some((task) => task.id === selectedTaskID) ? selectedTaskID : "") ||
          nextTasks[0]?.id ||
          "";
        setSelectedTaskID(nextTaskID);
        if (nextTaskID) {
          await loadTaskDetail(nextTaskID, preferredRunID);
        } else {
          setRuns([]);
          setSelectedRunID("");
          setApprovals([]);
          setSteps([]);
          setArtifacts([]);
        }
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : "failed to load runs");
      } finally {
        setLoading(false);
      }
    },
    [authToken, selectedTaskID, session.isAuthenticated],
  );

  const loadTaskDetail = useCallback(
    async (taskID: string, preferredRunID = "") => {
      if (!taskID || !session.isAuthenticated) {
        return;
      }
      const [taskResponse, runsResponse, approvalsResponse] = await Promise.all([
        getTask(taskID, authToken),
        getTaskRuns(taskID, authToken),
        getTaskApprovals(taskID, authToken),
      ]);
      setTasks((current) => {
        const others = current.filter((task) => task.id !== taskID);
        return [taskResponse.data, ...others];
      });
      const nextRuns = runsResponse.data ?? [];
      setRuns(nextRuns);
      setApprovals(approvalsResponse.data ?? []);

      const nextRunID =
        (preferredRunID && nextRuns.some((run) => run.id === preferredRunID) ? preferredRunID : "") ||
        (selectedRunID && nextRuns.some((run) => run.id === selectedRunID) ? selectedRunID : "") ||
        taskResponse.data.latest_run_id ||
        nextRuns[0]?.id ||
        "";
      setSelectedRunID(nextRunID);

      if (!nextRunID) {
        setSteps([]);
        setArtifacts([]);
        return;
      }

      await loadRunDetail(taskID, nextRunID);
    },
    [authToken, loadRunDetail, selectedRunID, session.isAuthenticated],
  );

  useEffect(() => {
    void loadTasks();
  }, [loadTasks]);

  useEffect(() => {
    if (!selectedTaskID || !selectedRunID || !session.isAuthenticated) {
      setStreamState(selectedRunID ? "closed" : "idle");
      return;
    }
    const controller = new AbortController();
    setStreamState("connecting");
    setError("");

    void streamTaskRun(
      selectedTaskID,
      selectedRunID,
      authToken,
      ({ payload }) => {
        setStreamState("live");
        setRuns((current) => updateRunList(current, payload.data.run));
        setSteps(payload.data.steps ?? []);
        setArtifacts(payload.data.artifacts ?? []);
        setTasks((current) =>
          current.map((task) =>
            task.id === selectedTaskID
              ? {
                  ...task,
                  status: payload.data.run.status,
                  latest_run_id: payload.data.run.id,
                  step_count: payload.data.run.step_count ?? task.step_count,
                  artifact_count: payload.data.run.artifact_count ?? task.artifact_count,
                  latest_request_id: payload.data.run.request_id ?? task.latest_request_id,
                  latest_trace_id: payload.data.run.trace_id ?? task.latest_trace_id,
                  last_error: payload.data.run.last_error ?? "",
                }
              : task,
          ),
        );
      },
      controller.signal,
    )
      .then(() => {
        if (!controller.signal.aborted) {
          setStreamState("closed");
          void loadTaskDetail(selectedTaskID, selectedRunID);
        }
      })
      .catch((streamError) => {
        if (controller.signal.aborted) {
          return;
        }
        setStreamState("error");
        setError(streamError instanceof Error ? streamError.message : "stream disconnected");
      });

    return () => controller.abort();
  }, [authToken, loadTaskDetail, selectedRunID, selectedTaskID, session.isAuthenticated]);

  async function handleSelectTask(taskID: string) {
    setSelectedTaskID(taskID);
    setSteps([]);
    setArtifacts([]);
    setNotice(null);
    try {
      await loadTaskDetail(taskID);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "failed to load task detail");
    }
  }

  async function handleSelectRun(runID: string) {
    setSelectedRunID(runID);
    if (!selectedTaskID) {
      return;
    }
    try {
      await loadRunDetail(selectedTaskID, runID);
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : "failed to load run detail");
    }
  }

  async function handleResolveApproval(decision: "approve" | "reject") {
    const approval = pendingApprovals[0];
    if (!selectedTaskID || !approval) {
      return;
    }
    setBusyAction(decision === "approve" ? "approve" : "reject");
    setNotice(null);
    try {
      await resolveTaskApproval(selectedTaskID, approval.id, { decision }, authToken);
      setNotice({ tone: "success", message: decision === "approve" ? "Approval granted." : "Approval rejected." });
      await loadTaskDetail(selectedTaskID, approval.run_id);
    } catch (approvalError) {
      setNotice({ tone: "error", message: approvalError instanceof Error ? approvalError.message : "failed to resolve approval" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCancelRun() {
    if (!selectedTaskID || !selectedRunID) {
      return;
    }
    setBusyAction("cancel");
    setNotice(null);
    try {
      await cancelTaskRun(selectedTaskID, selectedRunID, authToken);
      setNotice({ tone: "success", message: "Run cancelled." });
      await loadTaskDetail(selectedTaskID, selectedRunID);
    } catch (cancelError) {
      setNotice({ tone: "error", message: cancelError instanceof Error ? cancelError.message : "failed to cancel run" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleCreateTask(startImmediately: boolean) {
    if (!taskPrompt.trim()) {
      setNotice({ tone: "error", message: "Prompt is required." });
      return;
    }
    if (executionKind === "shell" && !shellCommand.trim()) {
      setNotice({ tone: "error", message: "Shell command is required for shell tasks." });
      return;
    }
    if (executionKind === "git" && !gitCommand.trim()) {
      setNotice({ tone: "error", message: "Git command is required for git tasks." });
      return;
    }
    if (executionKind === "file" && !filePath.trim()) {
      setNotice({ tone: "error", message: "File path is required for file tasks." });
      return;
    }

    setBusyAction(startImmediately ? "create_start" : "create");
    setNotice(null);
    try {
      const created = await createTask(
        {
          title: taskTitle.trim() || undefined,
          prompt: taskPrompt.trim(),
          execution_kind: executionKind === "stub" ? undefined : executionKind,
          shell_command: executionKind === "shell" ? shellCommand.trim() : undefined,
          git_command: executionKind === "git" ? gitCommand.trim() : undefined,
          working_directory: workingDirectory.trim() || undefined,
          file_operation: executionKind === "file" ? fileOperation : undefined,
          file_path: executionKind === "file" ? filePath.trim() : undefined,
          file_content: executionKind === "file" ? fileContent : undefined,
        },
        authToken,
      );

      let preferredRunID = "";
      if (startImmediately) {
        const started = await startTask(created.data.id, authToken);
        preferredRunID = started.data.id;
      }

      resetComposer();
      setNotice({ tone: "success", message: startImmediately ? "Task created and started." : "Task created." });
      await loadTasks(created.data.id, preferredRunID);
    } catch (taskError) {
      setNotice({ tone: "error", message: taskError instanceof Error ? taskError.message : "failed to create task" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleStartSelectedTask() {
    if (!selectedTaskID) {
      return;
    }
    setBusyAction("start");
    setNotice(null);
    try {
      const started = await startTask(selectedTaskID, authToken);
      setNotice({ tone: "success", message: "Task started." });
      await loadTasks(selectedTaskID, started.data.id);
    } catch (startError) {
      setNotice({ tone: "error", message: startError instanceof Error ? startError.message : "failed to start task" });
    } finally {
      setBusyAction("");
    }
  }

  function resetComposer() {
    setTaskTitle("");
    setTaskPrompt("");
    setExecutionKind("stub");
    setWorkingDirectory("");
    setShellCommand("");
    setGitCommand("");
    setFileOperation("write");
    setFilePath("");
    setFileContent("");
  }

  if (!session.isAuthenticated) {
    return (
      <ShellSection eyebrow="Runs" title="Live runs">
        <Surface>
          <EmptyState title="Authentication required" detail="Add a bearer token in Access to inspect task runs, approvals, and live stdout or stderr." />
        </Surface>
      </ShellSection>
    );
  }

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection
          eyebrow="Runtime"
          title="Live runs"
          description="Inspect queued and running task executions, approve blocked shell runs, and watch streamed stdout or stderr in one place."
          actions={(
            <div className="action-row">
              <StatusPill label={streamStatusLabel(streamState)} tone={streamStatusTone(streamState)} />
              <ToolbarButton onClick={() => void loadTasks(selectedTaskID, selectedRunID)} tone="primary">
                Refresh runs
              </ToolbarButton>
            </div>
          )}
        >
          <div className="metric-grid metric-grid--wide">
            <MetricTile label="Tasks" value={`${tasks.length}`} tone={tasks.length > 0 ? "healthy" : "warning"} />
            <MetricTile label="Runs" value={`${runs.length}`} tone={runs.length > 0 ? "healthy" : "warning"} />
            <MetricTile label="Pending approvals" value={`${pendingApprovals.length}`} tone={pendingApprovals.length > 0 ? "warning" : "healthy"} />
            <MetricTile label="Selected run" value={selectedRun ? `#${selectedRun.number}` : "None"} detail={selectedRun?.status || "Select a run"} />
          </div>
        </ShellSection>

        {error ? <InlineNotice message={error} tone="error" /> : null}
        {notice ? <InlineNotice message={notice.message} tone={notice.tone === "success" ? "success" : "error"} /> : null}

        <ShellSection eyebrow="Compose" title="Create task">
          <Surface tone="strong">
            <div className="stack-md">
              <p className="body-muted">Create a bounded coding task here, then start it immediately or leave it queued for later inspection.</p>
              <div className="form-grid">
                <TextField label="Title" onChange={setTaskTitle} placeholder="Optional task title" value={taskTitle} />
                <SelectField label="Execution kind" onChange={setExecutionKind} value={executionKind}>
                  <option value="stub">stub</option>
                  <option value="shell">shell</option>
                  <option value="file">file</option>
                  <option value="git">git</option>
                </SelectField>
                <TextField label="Working directory" onChange={setWorkingDirectory} placeholder="Optional working directory" value={workingDirectory} />
                {executionKind === "file" ? (
                  <SelectField label="File operation" onChange={setFileOperation} value={fileOperation}>
                    <option value="write">write</option>
                    <option value="append">append</option>
                  </SelectField>
                ) : (
                  <div />
                )}
              </div>
              <TextAreaField label="Prompt" onChange={setTaskPrompt} placeholder="Describe the task you want Hecate to execute." rows={4} value={taskPrompt} />
              {executionKind === "shell" ? (
                <TextAreaField label="Shell command" onChange={setShellCommand} placeholder="printf 'hello world\n'" rows={3} value={shellCommand} />
              ) : null}
              {executionKind === "git" ? (
                <TextAreaField label="Git command" onChange={setGitCommand} placeholder="status --short" rows={3} value={gitCommand} />
              ) : null}
              {executionKind === "file" ? (
                <div className="stack-sm">
                  <TextField label="File path" onChange={setFilePath} placeholder="notes/todo.txt" value={filePath} />
                  <TextAreaField label="File content" onChange={setFileContent} placeholder="Write the file contents here." rows={6} value={fileContent} />
                </div>
              ) : null}
              <div className="action-row">
                <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleCreateTask(false)} tone="primary">
                  {busyAction === "create" ? "Creating..." : "Create task"}
                </ToolbarButton>
                <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleCreateTask(true)}>
                  {busyAction === "create_start" ? "Creating and starting..." : "Create and start"}
                </ToolbarButton>
              </div>
            </div>
          </Surface>
        </ShellSection>

        <ShellSection eyebrow="Execution" title={selectedRun ? `Run #${selectedRun.number}` : "Run detail"}>
          <div className="two-column-grid two-column-grid--compact">
            <Surface>
              {selectedRun ? (
                <div className="stack-md">
                  <div className="action-row action-row--wide">
                    <div className="action-row">
                      <StatusPill label={selectedRun.status} tone={runStatusTone(selectedRun.status)} />
                      {selectedRun.model ? <StatusPill label={selectedRun.model} tone="neutral" /> : null}
                      {selectedRun.provider ? <StatusPill label={selectedRun.provider} tone="neutral" /> : null}
                    </div>
                    {canCancelRun(selectedRun.status) ? (
                      <ToolbarButton disabled={busyAction === "cancel"} onClick={() => void handleCancelRun()} tone="danger">
                        {busyAction === "cancel" ? "Cancelling..." : "Cancel run"}
                      </ToolbarButton>
                    ) : null}
                  </div>
                  <DefinitionList
                    items={[
                      { label: "Workspace", value: selectedRun.workspace_path || selectedRun.workspace_id || "Not set" },
                      { label: "Request ID", value: selectedRun.request_id || "n/a" },
                      { label: "Trace ID", value: selectedRun.trace_id || "n/a" },
                      { label: "Started", value: selectedRun.started_at ? formatDateTime(selectedRun.started_at) : "n/a" },
                      { label: "Finished", value: selectedRun.finished_at ? formatDateTime(selectedRun.finished_at) : "Still active" },
                      { label: "Last error", value: selectedRun.last_error || "None" },
                    ]}
                  />
                </div>
              ) : (
                <EmptyState title="No run selected" detail="Choose a task and run to inspect live updates and logs." />
              )}
            </Surface>

            <Surface>
              {pendingApprovals.length > 0 ? (
                <div className="stack-md">
                  <div className="action-row">
                    <StatusPill label={`${pendingApprovals.length} pending approval${pendingApprovals.length === 1 ? "" : "s"}`} tone="warning" />
                  </div>
                  {pendingApprovals.map((approval) => (
                    <div className="runs-approval-card" key={approval.id}>
                      <div className="action-row action-row--wide">
                        <div className="stack-sm">
                          <strong>{approval.kind}</strong>
                          <span className="body-muted">{approval.reason || "Approval required before execution continues."}</span>
                        </div>
                        <StatusPill label={approval.status} tone="warning" />
                      </div>
                      <div className="action-row">
                        <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleResolveApproval("approve")} tone="primary">
                          {busyAction === "approve" ? "Approving..." : "Approve"}
                        </ToolbarButton>
                        <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleResolveApproval("reject")} tone="danger">
                          {busyAction === "reject" ? "Rejecting..." : "Reject"}
                        </ToolbarButton>
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No approvals pending" detail="Selected task does not currently require operator approval." />
              )}
            </Surface>
          </div>
        </ShellSection>

        <ShellSection eyebrow="Output" title="Live stdout and stderr">
          <div className="two-column-grid">
            {[
              { label: "stdout", empty: "No stdout captured yet.", artifact: stdoutArtifact },
              { label: "stderr", empty: "No stderr captured yet.", artifact: stderrArtifact },
            ].map(({ label, empty, artifact }) => (
              <Surface key={label}>
                <div className="stack-sm">
                  <div className="action-row action-row--wide">
                    <strong>{label}</strong>
                    <StatusPill label={artifact?.status || "empty"} tone={runStatusTone(artifact?.status)} />
                  </div>
                  <pre className="runs-log-output">{artifact?.content_text || empty}</pre>
                </div>
              </Surface>
            ))}
          </div>
        </ShellSection>

        <ShellSection eyebrow="Steps" title="Execution timeline">
          <Surface>
            {steps.length > 0 ? (
              <div className="stack-sm">
                {steps.map((step) => (
                  <div className="runs-step-card" key={step.id}>
                    <div className="action-row action-row--wide">
                      <div className="action-row">
                        <strong>{step.title || step.kind}</strong>
                        <StatusPill label={step.status} tone={runStatusTone(step.status)} />
                      </div>
                      <span className="body-muted">{step.started_at ? formatDateTime(step.started_at) : "No start time"}</span>
                    </div>
                    <div className="runs-inline-meta">
                      <span>tool: {step.tool_name || step.kind}</span>
                      {typeof step.exit_code === "number" ? <span>exit: {step.exit_code}</span> : null}
                      {step.error_kind ? <span>{step.error_kind}</span> : null}
                    </div>
                    {step.error ? <p className="body-muted">{step.error}</p> : null}
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState title="No steps yet" detail="Step records will appear here once the selected run begins executing." />
            )}
          </Surface>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Tasks" title="Recent tasks">
          <Surface>
            {selectedTaskID ? (
              <div className="action-row runs-task-actions">
                <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleStartSelectedTask()} tone="primary">
                  {busyAction === "start" ? "Starting..." : "Start selected task"}
                </ToolbarButton>
              </div>
            ) : null}
            {loading ? (
              <p className="body-muted">Loading tasks...</p>
            ) : tasks.length > 0 ? (
              <div className="stack-sm">
                {tasks.map((task) => (
                  <button
                    className={task.id === selectedTaskID ? "runs-list-item runs-list-item--active" : "runs-list-item"}
                    key={task.id}
                    onClick={() => void handleSelectTask(task.id)}
                    type="button"
                  >
                    <div className="action-row action-row--wide">
                      <strong>{task.title}</strong>
                      <StatusPill label={task.status} tone={runStatusTone(task.status)} />
                    </div>
                    <p className="body-muted">{task.prompt}</p>
                  </button>
                ))}
              </div>
            ) : (
              <EmptyState title="No tasks yet" detail="Create your first task above, then inspect and stream it here." />
            )}
          </Surface>
        </ShellSection>

        <ShellSection eyebrow="Runs" title="Selected task runs">
          <Surface>
            {runs.length > 0 ? (
              <div className="stack-sm">
                {runs.map((run) => (
                  <button
                    className={run.id === selectedRunID ? "runs-list-item runs-list-item--active" : "runs-list-item"}
                    key={run.id}
                    onClick={() => void handleSelectRun(run.id)}
                    type="button"
                  >
                    <div className="action-row action-row--wide">
                      <strong>Run #{run.number}</strong>
                      <StatusPill label={run.status} tone={runStatusTone(run.status)} />
                    </div>
                    <div className="runs-inline-meta">
                      <span>{run.step_count ?? 0} steps</span>
                      <span>{run.artifact_count ?? 0} artifacts</span>
                    </div>
                  </button>
                ))}
              </div>
            ) : (
              <EmptyState title="No runs" detail="The selected task has not started a run yet." />
            )}
          </Surface>
        </ShellSection>
      </aside>
    </div>
  );
}

function updateRunList(current: TaskRunRecord[], nextRun: TaskRunRecord): TaskRunRecord[] {
  const others = current.filter((run) => run.id !== nextRun.id);
  return [nextRun, ...others];
}

function runStatusTone(status?: string): "neutral" | "healthy" | "warning" | "danger" {
  switch (status) {
    case "completed":
    case "ready":
      return "healthy";
    case "running":
    case "queued":
      return "neutral";
    case "awaiting_approval":
    case "streaming":
      return "warning";
    case "failed":
    case "cancelled":
    case "rejected":
      return "danger";
    default:
      return "neutral";
  }
}

function streamStatusLabel(state: StreamState): string {
  switch (state) {
    case "connecting":
      return "Stream connecting";
    case "live":
      return "Stream live";
    case "closed":
      return "Stream closed";
    case "error":
      return "Stream error";
    default:
      return "Stream idle";
  }
}

function streamStatusTone(state: StreamState): "neutral" | "healthy" | "warning" | "danger" {
  switch (state) {
    case "live":
      return "healthy";
    case "connecting":
    case "closed":
      return "warning";
    case "error":
      return "danger";
    default:
      return "neutral";
  }
}

function canCancelRun(status?: string): boolean {
  return status === "queued" || status === "running" || status === "awaiting_approval";
}
