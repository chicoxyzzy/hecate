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
  const conversationArtifact = artifacts.find(a => a.kind === "agent_conversation") ?? null;
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
                    borderRadius: "var(--radius)", boxShadow: "var(--shadow-dropdown)",
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
            <div className="kicker" style={{ marginBottom: 8 }}>Steps</div>
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

        {conversationArtifact?.content_text && (
          <AgentConversationView raw={conversationArtifact.content_text} />
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

        {/* Bottom artifacts strip — excludes stdout/stderr (rendered
            in the terminal pane above) and agent_conversation
            (rendered as the chat-bubble timeline). */}
        {artifacts.filter(isVisibleArtifactBadge).length > 0 && (
          <div style={{ padding: "10px 16px", borderTop: "1px solid var(--border)", display: "flex", flexWrap: "wrap", gap: 6, background: "var(--bg1)" }}>
            <span className="kicker" style={{ alignSelf: "center", marginRight: 4 }}>artifacts</span>
            {artifacts.filter(isVisibleArtifactBadge).map(a => (
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
          <div className="kicker" style={{ marginBottom: 4 }}>Error</div>
          <pre style={{ margin: 0, padding: "6px 8px", fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--red)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
            {step.error}
          </pre>
        </div>
      )}
      {step.input && Object.keys(step.input).length > 0 && (
        <div>
          <div className="kicker" style={{ marginBottom: 4 }}>Input</div>
          <pre style={{ margin: 0, padding: "6px 8px", fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--t1)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflowX: "auto", maxHeight: 200 }}>
            {JSON.stringify(step.input, null, 2)}
          </pre>
        </div>
      )}
      {step.output_summary && Object.keys(step.output_summary).length > 0 && (
        <div>
          <div className="kicker" style={{ marginBottom: 4 }}>Output</div>
          <pre style={{ margin: 0, padding: "6px 8px", fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--t1)", background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", overflowX: "auto", maxHeight: 200 }}>
            {JSON.stringify(step.output_summary, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}

// isVisibleArtifactBadge filters which artifacts get a chip in the
// bottom strip. stdout/stderr are rendered in the terminal pane;
// agent_conversation is rendered as a chat-bubble timeline. Both
// would be redundant as bare chips, so we hide them.
function isVisibleArtifactBadge(a: TaskArtifactRecord): boolean {
  return a.kind !== "stdout" && a.kind !== "stderr" && a.kind !== "agent_conversation";
}

// AgentConversationMessage mirrors pkg/types.Message (the shape the
// agent loop persists). Only fields the viewer renders are typed —
// extra fields on the wire (cache control, etc.) are ignored.
type AgentConversationMessage = {
  role: "user" | "assistant" | "tool" | string;
  content?: string;
  tool_call_id?: string;
  tool_calls?: Array<{
    id: string;
    type?: string;
    function?: { name?: string; arguments?: string };
  }>;
};

// AgentConversationView renders the agent_loop conversation as a
// chat-bubble timeline. User prompts on the right, assistant turns on
// the left (with their tool calls expanded), tool results in muted
// frames. The conversation is the agent's reasoning trail — operators
// scan it to understand WHY the agent did what it did, not just WHAT
// it did (the step timeline already covers that).
//
// Robustness: the artifact's content is JSON parsed inline. If the
// JSON is corrupt we render an inline error and continue rendering
// the rest of the run UI — losing the conversation viewer is much
// better than crashing the whole page.
function AgentConversationView({ raw }: { raw: string }) {
  let messages: AgentConversationMessage[] = [];
  try {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) messages = parsed as AgentConversationMessage[];
  } catch {
    return (
      <div style={{ padding: "10px 16px", borderBottom: "1px solid var(--border)", fontSize: 11, color: "var(--red)", fontFamily: "var(--font-mono)" }}>
        Could not parse agent conversation artifact (invalid JSON).
      </div>
    );
  }
  if (messages.length === 0) return null;

  return (
    <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)", display: "flex", flexDirection: "column", gap: 8 }}>
      <div className="kicker" style={{ marginBottom: 4 }}>
        Agent conversation · {messages.length} message{messages.length === 1 ? "" : "s"}
      </div>
      {messages.map((m, i) => (
        <ConversationBubble key={i} message={m} />
      ))}
    </div>
  );
}

function ConversationBubble({ message }: { message: AgentConversationMessage }) {
  if (message.role === "user") {
    return (
      <div style={{ display: "flex", justifyContent: "flex-end" }}>
        <div style={{
          maxWidth: "80%", padding: "8px 12px",
          background: "var(--teal-bg)", border: "1px solid var(--teal-border)",
          borderRadius: "var(--radius)", color: "var(--t0)", fontSize: 13, lineHeight: 1.5,
          whiteSpace: "pre-wrap", wordBreak: "break-word",
        }}>
          {message.content || ""}
        </div>
      </div>
    );
  }
  if (message.role === "tool") {
    // Tool results are typically formatted "status=…\n--- stdout ---\n…"
    // — render as a code block with monospace + scroll for long outputs.
    const callRef = message.tool_call_id ? ` · ${message.tool_call_id.slice(0, 12)}` : "";
    return (
      <div style={{ display: "flex", justifyContent: "flex-start" }}>
        <div style={{
          maxWidth: "90%", padding: "6px 10px",
          background: "var(--bg2)", border: "1px solid var(--border)",
          borderRadius: "var(--radius-sm)", fontSize: 11,
          fontFamily: "var(--font-mono)", color: "var(--t1)",
        }}>
          <div className="kicker" style={{ marginBottom: 4 }}>
            tool result{callRef}
          </div>
          <pre style={{ margin: 0, whiteSpace: "pre-wrap", wordBreak: "break-word", maxHeight: 200, overflowY: "auto", color: "var(--t1)" }}>
            {message.content || ""}
          </pre>
        </div>
      </div>
    );
  }
  // assistant — content + any tool calls
  return (
    <div style={{ display: "flex", justifyContent: "flex-start", flexDirection: "column", gap: 6, alignItems: "stretch" }}>
      {message.content && (
        <div style={{
          alignSelf: "flex-start", maxWidth: "80%", padding: "8px 12px",
          background: "var(--bg3)", border: "1px solid var(--border)",
          borderRadius: "var(--radius)", color: "var(--t0)", fontSize: 13, lineHeight: 1.5,
          whiteSpace: "pre-wrap", wordBreak: "break-word",
        }}>
          {message.content}
        </div>
      )}
      {message.tool_calls && message.tool_calls.length > 0 && (
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          {message.tool_calls.map((tc, i) => (
            <ToolCallChip key={tc.id || i} call={tc} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolCallChip({ call }: { call: NonNullable<AgentConversationMessage["tool_calls"]>[number] }) {
  // Pretty-print the JSON arguments when possible — collapsed to a
  // single line for compactness, with a click-to-expand affordance.
  const argsText = (() => {
    if (!call.function?.arguments) return "";
    try {
      const parsed = JSON.parse(call.function.arguments);
      return JSON.stringify(parsed);
    } catch {
      return call.function.arguments;
    }
  })();
  return (
    <div style={{
      alignSelf: "flex-start", maxWidth: "90%",
      padding: "6px 10px", background: "var(--bg2)",
      border: "1px solid var(--teal-border)", borderRadius: "var(--radius-sm)",
      fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t1)",
    }}>
      <span style={{ color: "var(--teal)", fontWeight: 500 }}>→ {call.function?.name || "(unknown)"}</span>
      {argsText && (
        <>
          <span style={{ color: "var(--t3)" }}> </span>
          <span title={argsText} style={{ color: "var(--t2)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: "100%", display: "inline-block", verticalAlign: "bottom" }}>
            {argsText.length > 200 ? argsText.slice(0, 200) + "…" : argsText}
          </span>
        </>
      )}
    </div>
  );
}
