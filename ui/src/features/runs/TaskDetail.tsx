import { useEffect, useRef, useState } from "react";
import type { TaskApprovalRecord, TaskArtifactRecord, TaskRecord, TaskRunRecord, TaskStepRecord } from "../../types/runtime";
import { Badge, Dot, Icon, Icons } from "../shared/ui";

type StreamState = "idle" | "connecting" | "live" | "closed" | "error";

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

function formatDuration(start?: string, end?: string): string {
  if (!start) return "";
  const startMs = new Date(start).getTime();
  const endMs = end ? new Date(end).getTime() : Date.now();
  const seconds = Math.max(0, (endMs - startMs) / 1000);
  if (seconds < 1) return `${Math.round(seconds * 1000)}ms`;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  return `${Math.floor(seconds / 60)}m ${Math.round(seconds % 60)}s`;
}

type Props = {
  task: TaskRecord;
  run: TaskRunRecord | null;
  runs: TaskRunRecord[];
  selectedRunID: string;
  steps: TaskStepRecord[];
  artifacts: TaskArtifactRecord[];
  approvals: TaskApprovalRecord[];
  streamState: StreamState;
  busyAction: string;
  notice: { tone: "success" | "error"; message: string } | null;
  onSelectRun: (runID: string) => void;
  onResolveApproval: (approval: TaskApprovalRecord, decision: "approve" | "reject") => void;
  onCancelRun: () => void;
  onRetryRun: () => void;
  onResumeRun: () => void;
};

export function TaskDetail({
  task, run, runs, selectedRunID, steps, artifacts, approvals,
  streamState, busyAction, notice,
  onSelectRun, onResolveApproval, onCancelRun, onRetryRun, onResumeRun,
}: Props) {
  const termRef = useRef<HTMLDivElement>(null);
  const [runPickerOpen, setRunPickerOpen] = useState(false);
  const [expandedStepID, setExpandedStepID] = useState<string>("");
  const stdoutArtifact = artifacts.find(a => a.kind === "stdout") ?? null;
  const stderrArtifact = artifacts.find(a => a.kind === "stderr") ?? null;
  const pendingApprovals = approvals.filter(a => a.status === "pending");

  useEffect(() => {
    if (termRef.current) termRef.current.scrollTop = termRef.current.scrollHeight;
  }, [stdoutArtifact]);

  useEffect(() => { setExpandedStepID(""); }, [selectedRunID]);

  return (
    <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", minWidth: 0 }}>
      <div style={{ height: "var(--topbar-h)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 16px", gap: 10, flexShrink: 0, background: "var(--bg1)" }}>
        <Badge status={taskBadgeStatus(task.status)} />
        <span style={{ fontWeight: 500, fontSize: 13, color: "var(--t0)", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {task.title || task.prompt || "Untitled"}
        </span>
        {runs.length > 0 && (
          <div style={{ position: "relative" }}>
            <button
              className="btn btn-ghost btn-sm"
              onClick={() => setRunPickerOpen(o => !o)}
              aria-haspopup="listbox"
              aria-expanded={runPickerOpen}
              aria-label="Select run"
              style={{ fontFamily: "var(--font-mono)", fontSize: 11, gap: 6 }}
            >
              <span>run #{run?.number ?? "?"}</span>
              {runs.length > 1 && <span style={{ color: "var(--t3)" }}>of {runs.length}</span>}
              <Icon d={Icons.chevD} size={11} />
            </button>
            {runPickerOpen && (
              <>
                <div
                  style={{ position: "fixed", inset: 0, zIndex: 40 }}
                  onClick={() => setRunPickerOpen(false)}
                />
                <div
                  role="listbox"
                  style={{
                    position: "absolute", top: "calc(100% + 4px)", right: 0, zIndex: 41,
                    minWidth: 220, maxHeight: 320, overflowY: "auto",
                    background: "var(--bg1)", border: "1px solid var(--border)",
                    borderRadius: "var(--radius)", boxShadow: "0 4px 12px oklch(0 0 0 / 0.2)",
                  }}
                >
                  {runs.map(r => (
                    <button
                      key={r.id}
                      role="option"
                      aria-selected={r.id === selectedRunID}
                      onClick={() => { onSelectRun(r.id); setRunPickerOpen(false); }}
                      style={{
                        width: "100%", textAlign: "left", display: "flex", alignItems: "center", gap: 8,
                        padding: "8px 10px", border: "none",
                        background: r.id === selectedRunID ? "var(--bg2)" : "transparent",
                        cursor: "pointer", borderBottom: "1px solid var(--border)",
                      }}
                    >
                      <Badge status={taskBadgeStatus(r.status)} />
                      <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--t0)", flex: 1 }}>
                        run #{r.number}
                      </span>
                      {r.started_at && (
                        <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                          {new Date(r.started_at).toLocaleTimeString()}
                        </span>
                      )}
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>
        )}
        {run?.model && <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)" }}>{run.model}</span>}
        <div style={{ display: "flex", gap: 6 }}>
          {(run?.status === "queued" || run?.status === "running" || run?.status === "awaiting_approval") && (
            <button className="btn btn-danger btn-sm" disabled={busyAction === "cancel"} onClick={onCancelRun}>Cancel</button>
          )}
          {(run?.status === "failed" || run?.status === "cancelled") && (
            <>
              <button className="btn btn-sm" disabled={busyAction !== ""} onClick={onRetryRun}>Retry</button>
              <button className="btn btn-sm" disabled={busyAction !== ""} onClick={onResumeRun}>Resume</button>
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
              {(task.shell_command || task.git_command) && (
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t1)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "6px 10px", marginBottom: 10 }}>
                  {task.execution_kind === "git" ? `git ${task.git_command}` : task.shell_command}
                </div>
              )}
              <div style={{ display: "flex", gap: 8 }}>
                <button className="btn btn-primary btn-sm" disabled={busyAction !== ""} onClick={() => onResolveApproval(approval, "approve")} style={{ gap: 5 }}>
                  <Icon d={Icons.approve} size={13} /> Approve & run
                </button>
                <button className="btn btn-danger btn-sm" disabled={busyAction !== ""} onClick={() => onResolveApproval(approval, "reject")} style={{ gap: 5 }}>
                  <Icon d={Icons.deny} size={13} /> Deny
                </button>
              </div>
            </div>
          </div>
        ))}

        {steps.length > 0 && (
          <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)" }}>
            <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 8, letterSpacing: "0.06em", textTransform: "uppercase" }}>Steps</div>
            <div style={{ display: "flex", flexDirection: "column" }}>
              {steps.map((step, i) => {
                const expanded = expandedStepID === step.id;
                const hasDetail = !!(step.input || step.output_summary || step.error || step.tool_name || step.phase);
                return (
                  <div key={step.id} style={{ display: "flex", flexDirection: "column" }}>
                    <button
                      type="button"
                      aria-expanded={expanded}
                      aria-label={`Step ${step.title || step.kind || step.tool_name || "step"}`}
                      onClick={() => hasDetail && setExpandedStepID(expanded ? "" : step.id)}
                      style={{
                        display: "flex", alignItems: "center", gap: 10, padding: "5px 0", position: "relative",
                        background: "transparent", border: "none", textAlign: "left",
                        cursor: hasDetail ? "pointer" : "default", color: "inherit",
                      }}
                    >
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
                      {hasDetail && (
                        <span style={{ display: "inline-flex", color: "var(--t3)", transform: expanded ? "rotate(180deg)" : undefined, transition: "transform 0.1s" }}>
                          <Icon d={Icons.chevD} size={11} />
                        </span>
                      )}
                    </button>
                    {expanded && <StepDetail step={step} />}
                  </div>
                );
              })}
            </div>
          </div>
        )}

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
                {run?.status === "queued" ? "Waiting in queue…"
                  : run?.status === "running" ? "Running…"
                  : run?.status === "awaiting_approval" ? "Awaiting approval…"
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
            {(task.status === "running") && (
              <div style={{ color: "var(--teal)", animation: "blink 0.8s step-end infinite" }}>█</div>
            )}
          </div>
        </div>

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
  );
}

function StepDetail({ step }: { step: TaskStepRecord }) {
  const duration = formatDuration(step.started_at, step.finished_at);
  return (
    <div
      style={{
        margin: "4px 0 8px 24px",
        padding: "10px 12px",
        background: "var(--bg2)",
        border: "1px solid var(--border)",
        borderRadius: "var(--radius-sm)",
        display: "flex",
        flexDirection: "column",
        gap: 8,
      }}
    >
      <div style={{ display: "flex", flexWrap: "wrap", gap: 12, fontSize: 10, fontFamily: "var(--font-mono)", color: "var(--t3)" }}>
        {step.tool_name && <span>tool: <span style={{ color: "var(--t1)" }}>{step.tool_name}</span></span>}
        {step.phase && <span>phase: <span style={{ color: "var(--t1)" }}>{step.phase}</span></span>}
        {step.exit_code !== undefined && <span>exit: <span style={{ color: step.exit_code === 0 ? "var(--green)" : "var(--red)" }}>{step.exit_code}</span></span>}
        {duration && <span>took: <span style={{ color: "var(--t1)" }}>{duration}</span></span>}
        {step.started_at && <span>started: <span style={{ color: "var(--t1)" }}>{new Date(step.started_at).toLocaleString()}</span></span>}
      </div>
      {step.error && (
        <div>
          <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 4, letterSpacing: "0.06em", textTransform: "uppercase" }}>Error</div>
          <pre style={{ margin: 0, padding: "6px 8px", fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--red)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
            {step.error}
          </pre>
        </div>
      )}
      {step.input && Object.keys(step.input).length > 0 && (
        <div>
          <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 4, letterSpacing: "0.06em", textTransform: "uppercase" }}>Input</div>
          <pre style={{ margin: 0, padding: "6px 8px", fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--t1)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflowX: "auto", maxHeight: 200 }}>
            {JSON.stringify(step.input, null, 2)}
          </pre>
        </div>
      )}
      {step.output_summary && Object.keys(step.output_summary).length > 0 && (
        <div>
          <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 4, letterSpacing: "0.06em", textTransform: "uppercase" }}>Output</div>
          <pre style={{ margin: 0, padding: "6px 8px", fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--t1)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflowX: "auto", maxHeight: 200 }}>
            {JSON.stringify(step.output_summary, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
