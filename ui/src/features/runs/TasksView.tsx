import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  cancelTaskRun, createTask, deleteTask, getModels, getTaskApprovals, getTaskRunArtifacts,
  getTaskRuns, getTaskRunSteps, getTasks, resolveTaskApproval,
  retryTaskRun, retryTaskRunFromTurn, resumeTaskRun, startTask, streamTaskRun,
} from "../../lib/api";
import type { ModelRecord, TaskApprovalRecord, TaskArtifactRecord, TaskRecord, TaskRunRecord, TaskStepRecord } from "../../types/runtime";
import { TaskList } from "./TaskList";
import { TaskDetail } from "./TaskDetail";
import { NewTaskSlideOver, type CreateTaskPayload } from "./NewTaskSlideOver";

type Props = {
  authToken: string;
  session: { isAuthenticated: boolean };
};

type StreamState = "idle" | "connecting" | "live" | "closed" | "error";

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
  const [availableModels, setAvailableModels] = useState<ModelRecord[]>([]);

  const streamCursorRef = useRef(0);

  const selectedTask = useMemo(() => tasks.find(t => t.id === selectedTaskID) ?? null, [tasks, selectedTaskID]);
  const selectedRun = useMemo(() => runs.find(r => r.id === selectedRunID) ?? null, [runs, selectedRunID]);

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

  async function handleSelectTask(taskID: string) {
    setSelectedTaskID(taskID);
    resetRunDetail();
    setNotice(null);
    try { await loadTaskDetail(taskID); } catch { /* ignore */ }
  }

  async function handleSelectRun(runID: string) {
    if (!selectedTaskID || runID === selectedRunID) return;
    setSelectedRunID(runID);
    streamCursorRef.current = 0;
    setNotice(null);
    try { await loadRunDetail(selectedTaskID, runID); } catch { /* ignore */ }
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

  // Retry-from-turn-N: re-issue the LLM call at turn N with the prior
  // conversation preserved. Server-side validation rejects out-of-range
  // turns and non-agent_loop runs with a 4xx — we surface the message
  // in the run-level notice so the operator can correct and try again
  // rather than silently failing.
  async function handleRetryFromTurn(turn: number) {
    if (!selectedTaskID || !selectedRunID) return;
    setBusyAction("retry-from-turn");
    try {
      const res = await retryTaskRunFromTurn(selectedTaskID, selectedRunID, turn, authToken);
      setNotice({ tone: "success", message: `Retrying from turn ${turn} (run #${res.data.number}).` });
      await loadTasks(selectedTaskID, res.data.id);
    } catch (err) {
      setNotice({ tone: "error", message: err instanceof Error ? err.message : "retry-from-turn failed" });
    } finally { setBusyAction(""); }
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

  async function handleCreateTask(payload: CreateTaskPayload) {
    setBusyAction("create");
    try {
      const created = await createTask(payload, authToken);
      const started = await startTask(created.data.id, authToken);
      setNewTaskOpen(false);
      await loadTasks(created.data.id, started.data.id);
    } catch (err) {
      setNotice({ tone: "error", message: err instanceof Error ? err.message : "failed to create task" });
    } finally { setBusyAction(""); }
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
      <TaskList
        tasks={tasks}
        selectedTaskID={selectedTaskID}
        loading={loading}
        busyAction={busyAction}
        onSelect={(id) => void handleSelectTask(id)}
        onDelete={(id) => void handleDeleteTask(id)}
        onNewTask={() => setNewTaskOpen(true)}
        onRefresh={() => void loadTasks(selectedTaskID, selectedRunID)}
      />

      {selectedTask ? (
        <TaskDetail
          task={selectedTask}
          run={selectedRun}
          runs={runs}
          selectedRunID={selectedRunID}
          steps={steps}
          artifacts={artifacts}
          approvals={approvals}
          streamState={streamState}
          busyAction={busyAction}
          notice={notice}
          onSelectRun={(id) => void handleSelectRun(id)}
          onResolveApproval={(approval, decision) => void handleResolveApproval(approval, decision)}
          onCancelRun={() => void handleCancelRun()}
          onRetryRun={() => void handleRetryRun()}
          onResumeRun={() => void handleResumeRun()}
          onRetryFromTurn={(turn) => void handleRetryFromTurn(turn)}
        />
      ) : (
        <div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center" }}>
          <div style={{ textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
            {loading ? "Loading…" : "Select a task to inspect."}
          </div>
        </div>
      )}

      <NewTaskSlideOver
        open={newTaskOpen}
        models={availableModels}
        busyAction={busyAction}
        errorMessage={notice?.tone === "error" ? notice.message : undefined}
        onClose={() => setNewTaskOpen(false)}
        onCreate={(payload) => void handleCreateTask(payload)}
      />

      <style>{`
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }
        @keyframes blink  { 0%,100%{opacity:1} 50%{opacity:0} }
      `}</style>
    </div>
  );
}
