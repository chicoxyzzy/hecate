import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  appendTaskRunEvent,
  cancelTaskRun,
  createTask,
  getTrace,
  getRuntimeStats,
  getTask,
  getTaskApprovals,
  getTaskRunArtifacts,
  getTaskRunEvents,
  getTaskRuns,
  getTaskRunSteps,
  getTasks,
  resumeTaskRun,
  retryTaskRun,
  resolveTaskApproval,
  startTask,
  streamTaskRun,
} from "../../lib/api";
import { formatDateTime } from "../../lib/format";
import type {
  TaskApprovalRecord,
  TaskArtifactRecord,
  TaskRecord,
  TaskRunEventRecord,
  TaskRunRecord,
  TaskStepRecord,
  RuntimeStatsResponse,
} from "../../types/runtime";
import { DefinitionList, EmptyState, InlineNotice, MetricTile, SelectField, ShellSection, StatusPill, Surface, TextAreaField, TextField, ToolbarButton } from "../shared/ConsolePrimitives";
import "./RunsView.css";

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
  const [runEvents, setRunEvents] = useState<TaskRunEventRecord[]>([]);
  const [lastSequence, setLastSequence] = useState(0);
  const streamCursorRef = useRef(0);
  const [approvalNotes, setApprovalNotes] = useState<Record<string, string>>({});
  const [selectedArtifactID, setSelectedArtifactID] = useState("");
  const [traceSummary, setTraceSummary] = useState<string>("");
  const [runtimeStats, setRuntimeStats] = useState<RuntimeStatsResponse["data"] | null>(null);
  const [runtimeStatsError, setRuntimeStatsError] = useState("");
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
  const selectedArtifact = useMemo(() => artifacts.find((artifact) => artifact.id === selectedArtifactID) ?? null, [artifacts, selectedArtifactID]);
  const stdoutArtifact = useMemo(() => artifacts.find((artifact) => artifact.kind === "stdout") ?? null, [artifacts]);
  const stderrArtifact = useMemo(() => artifacts.find((artifact) => artifact.kind === "stderr") ?? null, [artifacts]);
  const [eventTypeFilter, setEventTypeFilter] = useState("all");
  const [eventResultFilter, setEventResultFilter] = useState("all");
  const [eventErrorKindFilter, setEventErrorKindFilter] = useState("all");
  const [eventSearch, setEventSearch] = useState("");

  const telemetrySignals = useMemo(() => Object.entries(runtimeStats?.telemetry?.signals ?? {}), [runtimeStats]);
  const filteredRunEvents = useMemo(
    () =>
      runEvents.filter((event) => {
        const data = event.data ?? {};
        const eventType = event.event_type || "";
        const eventResult = normalizeEventField(data["result"]);
        const eventErrorKind = normalizeEventField(data["error_kind"]);
        const searchText = eventSearch.trim().toLowerCase();
        const searchable = [
          eventType,
          String(event.request_id ?? ""),
          String(event.trace_id ?? ""),
          String(data["tenant"] ?? ""),
          String(data["task_id"] ?? ""),
          String(data["run_id"] ?? ""),
          eventResult,
          eventErrorKind,
        ]
          .join(" ")
          .toLowerCase();

        if (eventTypeFilter !== "all" && eventType !== eventTypeFilter) {
          return false;
        }
        if (eventResultFilter !== "all" && eventResult !== eventResultFilter) {
          return false;
        }
        if (eventErrorKindFilter !== "all" && eventErrorKind !== eventErrorKindFilter) {
          return false;
        }
        if (searchText && !searchable.includes(searchText)) {
          return false;
        }
        return true;
      }),
    [eventErrorKindFilter, eventResultFilter, eventSearch, eventTypeFilter, runEvents],
  );

  const eventTypeOptions = useMemo(
    () => ["all", ...new Set(runEvents.map((event) => event.event_type).filter(Boolean))],
    [runEvents],
  );
  const eventResultOptions = useMemo(
    () => ["all", ...new Set(runEvents.map((event) => normalizeEventField(event.data?.result)).filter(Boolean))],
    [runEvents],
  );
  const eventErrorKindOptions = useMemo(
    () => ["all", ...new Set(runEvents.map((event) => normalizeEventField(event.data?.error_kind)).filter(Boolean))],
    [runEvents],
  );

  const resetRunDetailState = useCallback(() => {
    setSteps([]);
    setArtifacts([]);
    setRunEvents([]);
    setLastSequence(0);
    streamCursorRef.current = 0;
    setSelectedArtifactID("");
  }, []);

  const resetTaskScopeState = useCallback(() => {
    setRuns([]);
    setSelectedRunID("");
    setApprovals([]);
    resetRunDetailState();
  }, [resetRunDetailState]);

  const loadRunDetail = useCallback(
    async (taskID: string, runID: string) => {
      if (!taskID || !runID) {
        resetRunDetailState();
        return;
      }
      const [stepsResponse, artifactsResponse, eventsResponse] = await Promise.all([
        getTaskRunSteps(taskID, runID, authToken),
        getTaskRunArtifacts(taskID, runID, authToken),
        getTaskRunEvents(taskID, runID, 0, authToken),
      ]);
      setSteps(stepsResponse.data ?? []);
      const nextArtifacts = artifactsResponse.data ?? [];
      setArtifacts(nextArtifacts);
      setRunEvents(eventsResponse.data ?? []);
      const nextSequence = eventsResponse.data?.at(-1)?.sequence ?? 0;
      setLastSequence(nextSequence);
      streamCursorRef.current = nextSequence;
      if (nextArtifacts.length > 0) {
        setSelectedArtifactID((current) => (current && nextArtifacts.some((artifact) => artifact.id === current) ? current : nextArtifacts[0].id));
      } else {
        setSelectedArtifactID("");
      }
    },
    [authToken, resetRunDetailState],
  );

  const loadTasks = useCallback(
    async (preferredTaskID = "", preferredRunID = "") => {
      if (!session.isAuthenticated) {
        setTasks([]);
        setSelectedTaskID("");
        resetTaskScopeState();
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
          resetTaskScopeState();
        }
      } catch (loadError) {
        setError(loadError instanceof Error ? loadError.message : "failed to load runs");
      } finally {
        setLoading(false);
      }
    },
    [authToken, resetTaskScopeState, selectedTaskID, session.isAuthenticated],
  );

  const loadRuntimeStats = useCallback(async () => {
    if (!session.isAuthenticated) {
      setRuntimeStats(null);
      setRuntimeStatsError("");
      return;
    }
    try {
      const response = await getRuntimeStats(authToken);
      setRuntimeStats(response.data);
      setRuntimeStatsError("");
    } catch (statsError) {
      setRuntimeStats(null);
      setRuntimeStatsError(statsError instanceof Error ? statsError.message : "failed to load runtime stats");
    }
  }, [authToken, session.isAuthenticated]);

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
        resetRunDetailState();
        return;
      }

      await loadRunDetail(taskID, nextRunID);
    },
    [authToken, loadRunDetail, resetRunDetailState, selectedRunID, session.isAuthenticated],
  );

  useEffect(() => {
    void loadTasks();
  }, [loadTasks]);

  useEffect(() => {
    if (!session.isAuthenticated) {
      return;
    }
    void loadRuntimeStats();
    const interval = window.setInterval(() => void loadRuntimeStats(), 15000);
    return () => window.clearInterval(interval);
  }, [loadRuntimeStats, session.isAuthenticated]);

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
        setLastSequence(payload.data.sequence ?? 0);
        streamCursorRef.current = payload.data.sequence ?? streamCursorRef.current;
        setRunEvents((current) => {
          const entry: TaskRunEventRecord = {
            id: String(payload.data.sequence ?? Date.now()),
            task_id: selectedTaskID,
            run_id: selectedRunID,
            sequence: payload.data.sequence ?? 0,
            event_type: payload.data.event_type || "snapshot",
            created_at: new Date().toISOString(),
            data: {
              run_status: payload.data.run.status,
              step_count: payload.data.steps?.length ?? 0,
              artifact_count: payload.data.artifacts?.length ?? 0,
            },
          };
          const merged = [...current.filter((event) => event.sequence !== entry.sequence), entry];
          merged.sort((a, b) => b.sequence - a.sequence);
          return merged.slice(0, 200);
        });
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
      streamCursorRef.current,
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
    resetRunDetailState();
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

  async function handleResolveApproval(approval: TaskApprovalRecord, decision: "approve" | "reject") {
    if (!selectedTaskID || !approval) {
      return;
    }
    setBusyAction(decision === "approve" ? "approve" : "reject");
    setNotice(null);
    try {
      const note = (approvalNotes[approval.id] || "").trim();
      await resolveTaskApproval(selectedTaskID, approval.id, { decision, note: note || undefined }, authToken);
      setNotice({ tone: "success", message: decision === "approve" ? "Approval granted." : "Approval rejected." });
      if (note) {
        await appendTaskRunEvent(
          selectedTaskID,
          approval.run_id,
          {
            event_type: "approval.note",
            note,
            data: { approval_id: approval.id, decision },
          },
          authToken,
        );
      }
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

  async function handleRetryRun() {
    if (!selectedTaskID || !selectedRunID) {
      return;
    }
    setBusyAction("start");
    setNotice(null);
    try {
      const response = await retryTaskRun(selectedTaskID, selectedRunID, authToken);
      setNotice({ tone: "success", message: "Run retried with a new attempt." });
      await loadTasks(selectedTaskID, response.data.id);
    } catch (retryError) {
      setNotice({ tone: "error", message: retryError instanceof Error ? retryError.message : "failed to retry run" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleResumeRun() {
    if (!selectedTaskID || !selectedRunID) {
      return;
    }
    setBusyAction("start");
    setNotice(null);
    try {
      const response = await resumeTaskRun(selectedTaskID, selectedRunID, authToken);
      setNotice({ tone: "success", message: "Run resumed with a new attempt." });
      await loadTasks(selectedTaskID, response.data.id);
    } catch (resumeError) {
      setNotice({ tone: "error", message: resumeError instanceof Error ? resumeError.message : "failed to resume run" });
    } finally {
      setBusyAction("");
    }
  }

  async function handleLookupTrace() {
    if (!selectedRun?.request_id) {
      return;
    }
    try {
      const trace = await getTrace(selectedRun.request_id, authToken);
      const spanCount = trace.data.spans?.length ?? 0;
      setTraceSummary(`Trace ${trace.data.trace_id || "n/a"} with ${spanCount} spans loaded for request ${selectedRun.request_id}.`);
    } catch (traceError) {
      setTraceSummary(traceError instanceof Error ? traceError.message : "failed to fetch trace");
    }
  }

  function handleOpenTrace(requestID?: string) {
    if (!requestID) {
      return;
    }
    const url = `/v1/traces?request_id=${encodeURIComponent(requestID)}`;
    window.open(url, "_blank", "noopener,noreferrer");
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
              <ToolbarButton onClick={() => void loadRuntimeStats()}>Refresh stats</ToolbarButton>
            </div>
          )}
        >
          <div className="metric-grid metric-grid--wide">
            <MetricTile label="Tasks" value={`${tasks.length}`} tone={tasks.length > 0 ? "healthy" : "warning"} />
            <MetricTile label="Runs" value={`${runs.length}`} tone={runs.length > 0 ? "healthy" : "warning"} />
            <MetricTile label="Pending approvals" value={`${pendingApprovals.length}`} tone={pendingApprovals.length > 0 ? "warning" : "healthy"} />
            <MetricTile label="Selected run" value={selectedRun ? `#${selectedRun.number}` : "None"} detail={selectedRun?.status || "Select a run"} />
            <MetricTile label="Last event seq" value={`${lastSequence}`} tone={lastSequence > 0 ? "neutral" : "warning"} />
          </div>
        </ShellSection>

        {error ? <InlineNotice message={error} tone="error" /> : null}
        {notice ? <InlineNotice message={notice.message} tone={notice.tone === "success" ? "success" : "error"} /> : null}
        {runtimeStatsError ? <InlineNotice message={`Runtime stats: ${runtimeStatsError}`} tone="error" /> : null}

        <ShellSection eyebrow="Observability" title="Telemetry health and SLOs">
          <div className="two-column-grid">
            <Surface>
              <div className="metric-grid metric-grid--wide">
                <MetricTile label="Queue wait p50" value={formatMetricMs(runtimeStats?.slo?.queue_wait_ms_p50)} />
                <MetricTile label="Queue wait p95" value={formatMetricMs(runtimeStats?.slo?.queue_wait_ms_p95)} />
                <MetricTile label="Approval wait p50" value={formatMetricMs(runtimeStats?.slo?.approval_wait_ms_p50)} />
                <MetricTile label="Approval wait p95" value={formatMetricMs(runtimeStats?.slo?.approval_wait_ms_p95)} />
                <MetricTile label="Run success rate" value={formatRate(runtimeStats?.slo?.run_success_rate)} />
                <MetricTile label="Run error rate" value={formatRate(runtimeStats?.slo?.run_error_rate)} />
              </div>
            </Surface>
            <Surface>
              {telemetrySignals.length > 0 ? (
                <div className="stack-sm">
                  {telemetrySignals.map(([name, signal]) => (
                    <div className="runs-step-card" key={name}>
                      <div className="action-row action-row--wide">
                        <strong>{name}</strong>
                        <StatusPill label={signal.enabled ? "enabled" : "disabled"} tone={signal.enabled ? "healthy" : "warning"} />
                      </div>
                      <div className="runs-inline-meta">
                        <span>activity: {signal.activity_count ?? 0}</span>
                        <span>errors: {signal.error_count ?? 0}</span>
                        {signal.last_activity_at ? <span>last activity: {formatDateTime(signal.last_activity_at)}</span> : null}
                        {signal.last_error_at ? <span>last error: {formatDateTime(signal.last_error_at)}</span> : null}
                      </div>
                      {signal.endpoint ? <p className="body-muted">endpoint: {signal.endpoint}</p> : null}
                      {signal.last_error ? <p className="body-muted">last error: {signal.last_error}</p> : null}
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No telemetry signal health yet" detail="Runtime stats endpoint does not expose telemetry signal status in this environment." />
              )}
            </Surface>
          </div>
        </ShellSection>

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
                      {selectedRun.trace_id ? <StatusPill label={`trace:${selectedRun.trace_id}`} tone="neutral" /> : null}
                    </div>
                    <div className="action-row">
                      {canCancelRun(selectedRun.status) ? (
                        <ToolbarButton disabled={busyAction === "cancel"} onClick={() => void handleCancelRun()} tone="danger">
                          {busyAction === "cancel" ? "Cancelling..." : "Cancel run"}
                        </ToolbarButton>
                      ) : null}
                      {(selectedRun.status === "failed" || selectedRun.status === "cancelled") ? (
                        <>
                          <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleRetryRun()}>
                            Retry
                          </ToolbarButton>
                          <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleResumeRun()}>
                            Resume
                          </ToolbarButton>
                        </>
                      ) : null}
                      <ToolbarButton onClick={() => void handleLookupTrace()}>Fetch trace</ToolbarButton>
                      <ToolbarButton onClick={() => handleOpenTrace(selectedRun.request_id)}>Open trace JSON</ToolbarButton>
                    </div>
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
                  {traceSummary ? <InlineNotice message={traceSummary} tone="success" /> : null}
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
                        <TextField
                          label="Resolution note"
                          onChange={(value) => setApprovalNotes((current) => ({ ...current, [approval.id]: value }))}
                          placeholder="Optional note for audit trail"
                          value={approvalNotes[approval.id] || ""}
                        />
                      </div>
                      <div className="action-row">
                        <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleResolveApproval(approval, "approve")} tone="primary">
                          {busyAction === "approve" ? "Approving..." : "Approve"}
                        </ToolbarButton>
                        <ToolbarButton disabled={busyAction !== ""} onClick={() => void handleResolveApproval(approval, "reject")} tone="danger">
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

        <ShellSection eyebrow="Artifacts" title="Run artifacts">
          <div className="two-column-grid">
            <Surface>
              {artifacts.length > 0 ? (
                <div className="stack-sm">
                  {artifacts.map((artifact) => (
                    <button
                      className={artifact.id === selectedArtifactID ? "runs-list-item runs-list-item--active" : "runs-list-item"}
                      key={artifact.id}
                      onClick={() => setSelectedArtifactID(artifact.id)}
                      type="button"
                    >
                      <div className="action-row action-row--wide">
                        <strong>{artifact.name || artifact.kind}</strong>
                        <StatusPill label={artifact.status || "ready"} tone={runStatusTone(artifact.status)} />
                      </div>
                      <div className="runs-inline-meta">
                        <span>{artifact.kind}</span>
                        <span>{artifact.mime_type || "text/plain"}</span>
                      </div>
                    </button>
                  ))}
                </div>
              ) : (
                <EmptyState title="No artifacts yet" detail="Artifacts appear here as the run emits files and logs." />
              )}
            </Surface>
            <Surface>
              {selectedArtifact ? (
                <div className="stack-sm">
                  <DefinitionList
                    items={[
                      { label: "Kind", value: selectedArtifact.kind },
                      { label: "Name", value: selectedArtifact.name || "n/a" },
                      { label: "MIME", value: selectedArtifact.mime_type || "n/a" },
                      { label: "Path", value: selectedArtifact.path || selectedArtifact.object_ref || "n/a" },
                      { label: "SHA256", value: selectedArtifact.sha256 || "n/a" },
                      { label: "Size", value: selectedArtifact.size_bytes ? `${selectedArtifact.size_bytes} bytes` : "n/a" },
                    ]}
                  />
                  <pre className="runs-log-output">{selectedArtifact.content_text || "No inline content."}</pre>
                </div>
              ) : (
                <EmptyState title="Select an artifact" detail="Choose an artifact to inspect metadata and inline content." />
              )}
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

        <ShellSection eyebrow="Events" title="Run event timeline">
          <Surface>
            <div className="form-grid">
              <SelectField label="Event type" onChange={setEventTypeFilter} value={eventTypeFilter}>
                {eventTypeOptions.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </SelectField>
              <SelectField label="Result" onChange={setEventResultFilter} value={eventResultFilter}>
                {eventResultOptions.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </SelectField>
              <SelectField label="Error kind" onChange={setEventErrorKindFilter} value={eventErrorKindFilter}>
                {eventErrorKindOptions.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </SelectField>
              <TextField
                label="Search (tenant/task/run/result/error)"
                onChange={setEventSearch}
                placeholder="tenant id, task id, run id, result..."
                value={eventSearch}
              />
            </div>
            {runEvents.length > 0 ? (
              <div className="stack-sm">
                {filteredRunEvents.map((event) => (
                  <div className="runs-step-card" key={`${event.sequence}-${event.id}`}>
                    <div className="action-row action-row--wide">
                      <strong>{event.event_type}</strong>
                      <div className="action-row">
                        <StatusPill label={`#${event.sequence}`} tone="neutral" />
                        {event.trace_id ? <StatusPill label={event.trace_id} tone="neutral" /> : null}
                      </div>
                    </div>
                    <div className="runs-inline-meta">
                      <span>{event.created_at ? formatDateTime(event.created_at) : "n/a"}</span>
                      {normalizeEventField(event.data?.result) ? <span>result: {normalizeEventField(event.data?.result)}</span> : null}
                      {normalizeEventField(event.data?.error_kind) ? <span>error_kind: {normalizeEventField(event.data?.error_kind)}</span> : null}
                    </div>
                    <div className="action-row">
                      <ToolbarButton onClick={() => handleOpenTrace(event.request_id || selectedRun?.request_id)}>Open trace for event</ToolbarButton>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyState title="No events yet" detail="Run events populate as state changes are persisted." />
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

function normalizeEventField(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function formatMetricMs(value?: number): string {
  return typeof value === "number" && Number.isFinite(value) ? `${value.toFixed(1)} ms` : "n/a";
}

function formatRate(value?: number): string {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    return "n/a";
  }
  if (value <= 1) {
    return `${(value * 100).toFixed(1)}%`;
  }
  return `${value.toFixed(1)}%`;
}
