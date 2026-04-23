import { useCallback, useEffect, useMemo, useState } from "react";

import {
  cancelTaskRun,
  getTask,
  getTaskApprovals,
  getTaskRunArtifacts,
  getTaskRuns,
  getTaskRunSteps,
  getTasks,
  resolveTaskApproval,
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
import { EmptyState, InlineNotice, MetricTile, ShellSection, StatusPill, Surface, ToolbarButton } from "../shared/ConsolePrimitives";

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
  const [busyAction, setBusyAction] = useState<"" | "approve" | "reject" | "cancel">("");

  const selectedRun = useMemo(() => runs.find((run) => run.id === selectedRunID) ?? null, [runs, selectedRunID]);
  const pendingApprovals = useMemo(
    () => approvals.filter((approval) => approval.status === "pending" && (!selectedRunID || approval.run_id === selectedRunID)),
    [approvals, selectedRunID],
  );
  const stdoutArtifact = useMemo(() => artifacts.find((artifact) => artifact.kind === "stdout") ?? null, [artifacts]);
  const stderrArtifact = useMemo(() => artifacts.find((artifact) => artifact.kind === "stderr") ?? null, [artifacts]);

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

      const [stepsResponse, artifactsResponse] = await Promise.all([
        getTaskRunSteps(taskID, nextRunID, authToken),
        getTaskRunArtifacts(taskID, nextRunID, authToken),
      ]);
      setSteps(stepsResponse.data ?? []);
      setArtifacts(artifactsResponse.data ?? []);
    },
    [authToken, selectedRunID, session.isAuthenticated],
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
      const [stepsResponse, artifactsResponse] = await Promise.all([
        getTaskRunSteps(selectedTaskID, runID, authToken),
        getTaskRunArtifacts(selectedTaskID, runID, authToken),
      ]);
      setSteps(stepsResponse.data ?? []);
      setArtifacts(artifactsResponse.data ?? []);
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
                  <dl className="definition-list">
                    <div className="definition-list__row">
                      <dt>Workspace</dt>
                      <dd>{selectedRun.workspace_path || selectedRun.workspace_id || "Not set"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Request ID</dt>
                      <dd>{selectedRun.request_id || "n/a"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Trace ID</dt>
                      <dd>{selectedRun.trace_id || "n/a"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Started</dt>
                      <dd>{selectedRun.started_at ? formatDateTime(selectedRun.started_at) : "n/a"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Finished</dt>
                      <dd>{selectedRun.finished_at ? formatDateTime(selectedRun.finished_at) : "Still active"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Last error</dt>
                      <dd>{selectedRun.last_error || "None"}</dd>
                    </div>
                  </dl>
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
            <Surface>
              <div className="stack-sm">
                <div className="action-row action-row--wide">
                  <strong>stdout</strong>
                  <StatusPill label={stdoutArtifact?.status || "empty"} tone={artifactStatusTone(stdoutArtifact?.status)} />
                </div>
                <pre className="runs-log-output">{stdoutArtifact?.content_text || "No stdout captured yet."}</pre>
              </div>
            </Surface>
            <Surface>
              <div className="stack-sm">
                <div className="action-row action-row--wide">
                  <strong>stderr</strong>
                  <StatusPill label={stderrArtifact?.status || "empty"} tone={artifactStatusTone(stderrArtifact?.status)} />
                </div>
                <pre className="runs-log-output">{stderrArtifact?.content_text || "No stderr captured yet."}</pre>
              </div>
            </Surface>
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
                    <div className="budget-history-item__body">
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
              <EmptyState title="No tasks yet" detail="Create tasks via the API first, then inspect and stream them here." />
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
                    <div className="budget-history-item__body">
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

function artifactStatusTone(status?: string): "neutral" | "healthy" | "warning" | "danger" {
  return runStatusTone(status);
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
