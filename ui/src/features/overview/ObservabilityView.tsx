import { useCallback, useEffect, useRef, useState } from "react";
import { getMCPCacheStats, getRecentTraces, getRuntimeStats, getTrace } from "../../lib/api";
import {
  buildTraceTimeline,
  describeHealthStatus,
  describeRouteReason,
  describeRouteSkipReason,
  formatTraceAttributeKey,
  formatTraceAttributeValue,
  healthStatusTone,
  routeOutcomeTone,
} from "../../lib/runtime-utils";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import type { MCPCacheStatsResponse, RuntimeStatsResponse, TraceListItem, TraceResponse, TraceSpanRecord } from "../../types/runtime";
import { Badge, Dot } from "../shared/ui";
import { ConnectYourClient } from "./ConnectYourClient";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

const SPAN_COLORS: Record<string, string> = {
  "gateway.request":       "var(--teal)",
  "gateway.request.parse": "var(--teal-lo)",
  "gateway.governor":      "var(--green)",
  "gateway.router":        "var(--green)",
  "gateway.cache.exact":   "var(--t3)",
  "gateway.cache.semantic":"var(--t3)",
  "gateway.provider":      "var(--amber)",
  "gateway.usage":         "var(--green)",
  "gateway.cost":          "var(--green)",
  "gateway.response":      "var(--teal-lo)",
};

function spanColor(name: string, i: number): string {
  if (SPAN_COLORS[name]) return SPAN_COLORS[name];
  const palette = ["var(--teal)", "var(--green)", "var(--amber)", "var(--teal-lo)"];
  return palette[i % palette.length];
}

type ComputedSpan = { name: string; startMs: number; durMs: number; color: string; raw: TraceSpanRecord };

function computeSpans(spans: TraceSpanRecord[]): { spans: ComputedSpan[]; totalMs: number } {
  if (!spans.length) return { spans: [], totalMs: 0 };
  const parsed = spans.map(s => ({
    start: s.start_time ? new Date(s.start_time).getTime() : 0,
    end:   s.end_time   ? new Date(s.end_time).getTime()   : 0,
    raw: s,
  }));
  const t0 = Math.min(...parsed.map(s => s.start));
  const totalMs = Math.max(...parsed.map(s => s.end - t0), 1);
  return {
    totalMs,
    spans: parsed.map((s, i) => ({
      name:    s.raw.name,
      startMs: s.start - t0,
      durMs:   Math.max(s.end - s.start, 1),
      color:   spanColor(s.raw.name, i),
      raw:     s.raw,
    })),
  };
}

type StatCardProps = { label: string; value: string | number; sub?: string; highlight?: boolean };
function StatCard({ label, value, sub, highlight }: StatCardProps) {
  return (
    <div className="card" style={{ padding: "12px 14px", minWidth: 110 }}>
      <div className="kicker" style={{ color: "var(--t2)", marginBottom: 6 }}>{label}</div>
      <div style={{ fontSize: 22, fontWeight: 600, fontFamily: "var(--font-mono)", color: highlight ? "var(--amber)" : "var(--t0)", lineHeight: 1 }}>{value}</div>
      {sub && <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginTop: 4 }}>{sub}</div>}
    </div>
  );
}

export function ObservabilityView({ state }: Props) {
  const [runtimeStats, setRuntimeStats] = useState<RuntimeStatsResponse["data"] | null>(null);
  const [mcpCacheStats, setMCPCacheStats] = useState<MCPCacheStatsResponse["data"] | null>(null);
  const [traces, setTraces] = useState<TraceListItem[]>([]);
  const [liveMode, setLiveMode] = useState(true);
  const [selectedReqId, setSelectedReqId] = useState<string | null>(null);
  const [selectedSpan, setSelectedSpan] = useState<string | null>(null);
  const [traceDetail, setTraceDetail] = useState<TraceResponse["data"] | null>(null);
  const [traceFetching, setTraceFetching] = useState(false);
  const traceRetryRef = useRef<ReturnType<typeof window.setInterval> | null>(null);

  const loadStats = useCallback(async () => {
    if (!state.session.isAdmin) return;
    try {
      const res = await getRuntimeStats(state.authToken);
      setRuntimeStats(res.data);
    } catch { /* silently ignore */ }
    // MCP cache stats are best-effort and orthogonal to runtime
    // stats: a deploy without a wired cache returns
    // configured=false, which we still render. A failure here
    // (404 from older gateways, transient 5xx) leaves the prior
    // value in place rather than wiping the panel.
    try {
      const res = await getMCPCacheStats(state.authToken);
      setMCPCacheStats(res.data);
    } catch { /* silently ignore */ }
  }, [state.authToken, state.session.isAdmin]);

  const loadTraces = useCallback(async () => {
    if (!state.session.isAdmin) return;
    try {
      const res = await getRecentTraces(state.authToken, 50);
      setTraces(res.data ?? []);
    } catch { /* silently ignore */ }
  }, [state.authToken, state.session.isAdmin]);

  useEffect(() => {
    void loadStats();
    const interval = window.setInterval(() => void loadStats(), 10000);
    return () => window.clearInterval(interval);
  }, [loadStats]);

  useEffect(() => {
    void loadTraces();
    const interval = window.setInterval(() => void loadTraces(), 4000);
    return () => window.clearInterval(interval);
  }, [loadTraces]);

  // In live mode, auto-select the newest request
  useEffect(() => {
    if (!liveMode || traces.length === 0) return;
    const newest = traces[0];
    if (newest?.request_id) {
      setSelectedReqId(id => id === newest.request_id ? id : newest.request_id);
    }
  }, [liveMode, traces]);

  const fetchTraceDetail = useCallback((reqId: string) => {
    setTraceFetching(true);
    getTrace(reqId, state.authToken)
      .then(res => setTraceDetail(res.data))
      .catch(() => setTraceDetail(null))
      .finally(() => setTraceFetching(false));
  }, [state.authToken]);

  useEffect(() => {
    if (traceRetryRef.current) {
      window.clearInterval(traceRetryRef.current);
      traceRetryRef.current = null;
    }
    if (!selectedReqId) { setTraceDetail(null); return; }
    setTraceDetail(null);
    fetchTraceDetail(selectedReqId);
    // Retry every 2s until spans arrive (they come in asynchronously for in-flight requests)
    traceRetryRef.current = window.setInterval(() => {
      setTraceDetail(prev => {
        if (prev?.spans?.length) {
          if (traceRetryRef.current) window.clearInterval(traceRetryRef.current);
          return prev;
        }
        fetchTraceDetail(selectedReqId);
        return prev;
      });
    }, 2000);
    return () => {
      if (traceRetryRef.current) window.clearInterval(traceRetryRef.current);
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedReqId]);

  const stats = runtimeStats;
  const selectedTrace = traces.find(t => t.request_id === selectedReqId);
  const { spans: computedSpans, totalMs } = traceDetail?.spans?.length
    ? computeSpans(traceDetail.spans)
    : { spans: [], totalMs: 0 };
  const traceTimeline = traceDetail?.spans?.length ? buildTraceTimeline(traceDetail.spans, traceDetail.started_at) : [];

  // Gateway URL: derive from the page origin (UI is served from the same
  // host:port as the API now thanks to the embed). This keeps the snippets
  // accurate whether the operator visits via 127.0.0.1, localhost, or a
  // reverse-proxy hostname.
  const gatewayURL = typeof window !== "undefined" ? window.location.origin : "http://127.0.0.1:8080";

  return (
    <div style={{ height: "100%", overflowY: "auto", padding: 16, display: "flex", flexDirection: "column", gap: 16 }}>

      <ConnectYourClient gatewayURL={gatewayURL} token={state.authToken} />

      {/* Runtime stats */}
      {stats && (
        <div>
          <div className="kicker-lg" style={{ marginBottom: 8 }}>Runtime</div>
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            <StatCard label="queue depth" value={stats.queue_depth} sub={stats.queue_capacity ? `cap ${stats.queue_capacity}` : undefined} highlight={stats.queue_depth > 0} />
            <StatCard label="workers" value={stats.worker_count} />
            <StatCard label="in-flight" value={stats.in_flight_jobs} highlight={stats.in_flight_jobs > 0} />
            <StatCard label="running" value={stats.running_runs} highlight={stats.running_runs > 0} />
            {stats.queued_runs > 0 && <StatCard label="queued" value={stats.queued_runs} highlight />}
            {stats.awaiting_approval_runs > 0 && <StatCard label="awaiting approval" value={stats.awaiting_approval_runs} highlight />}
            {stats.store_backend && <StatCard label="store" value={stats.store_backend} />}
          </div>
        </div>
      )}

      {/* MCP client cache stats. Configured=false is rendered as a
          single "no cache" cell rather than hidden — operators
          benefit from knowing the cache is intentionally off vs.
          merely failing to fetch. */}
      {mcpCacheStats && (
        <div>
          <div className="kicker-lg" style={{ marginBottom: 8 }}>MCP client cache</div>
          {!mcpCacheStats.configured ? (
            <div style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
              No cache wired — agent_loop runs spawn fresh MCP subprocesses each time.
            </div>
          ) : (
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }} aria-label="MCP cache stats">
              <StatCard label="entries" value={mcpCacheStats.entries} />
              <StatCard label="in-use" value={mcpCacheStats.in_use} highlight={mcpCacheStats.in_use > 0} />
              <StatCard label="idle" value={mcpCacheStats.idle} />
            </div>
          )}
        </div>
      )}

      {/* Trace list + detail */}
      <div style={{ display: "flex", gap: 12, flex: 1, minHeight: 0, alignItems: "flex-start" }}>

        {/* Left: trace list */}
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="kicker-lg" style={{ marginBottom: 8, display: "flex", alignItems: "center", gap: 8 }}>
            <span>Recent requests</span>
            <button
              onClick={() => setLiveMode(m => !m)}
              style={{ background: "none", border: "none", padding: 0, cursor: "pointer", display: "flex", alignItems: "center", gap: 4 }}
            >
              <Dot color={liveMode ? "green" : "muted"} pulse={liveMode} />
              <span style={{ color: liveMode ? "var(--teal)" : "var(--t3)", fontSize: 10 }}>{liveMode ? "live" : "paused"}</span>
            </button>
          </div>
          <div className="card" style={{ overflow: "hidden" }}>
            {traces.length > 0 ? (
              <table className="table" style={{ tableLayout: "fixed" }}>
                <colgroup>
                  <col style={{ width: 70 }} />
                  <col style={{ width: 96 }} />
                  <col />
                  <col style={{ width: 70 }} />
                  <col style={{ width: 48 }} />
                  <col style={{ width: 54 }} />
                </colgroup>
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Request ID</th>
                    <th>Model</th>
                    <th>Provider</th>
                    <th>Spans</th>
                    <th>Duration</th>
                  </tr>
                </thead>
                <tbody>
                  {traces.map(t => {
                    const isError = t.status_code === "error";
                    const isSel = selectedReqId === t.request_id;
                    return (
                      <tr
                        key={t.request_id}
                        onClick={() => {
                          setLiveMode(false);
                          setSelectedReqId(id => id === t.request_id ? null : t.request_id);
                        }}
                        style={{ cursor: "pointer", background: isSel ? "var(--teal-bg)" : "transparent" }}
                      >
                        <td className="mono" style={{ color: "var(--t3)" }}>
                          {t.started_at ? new Date(t.started_at).toLocaleTimeString() : "—"}
                        </td>
                        <td className="mono" style={{ color: "var(--teal)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={t.request_id}>
                          {t.request_id.slice(0, 8)}…
                        </td>
                        <td className="mono" style={{ color: isError ? "var(--red)" : "var(--t0)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                          {t.route?.final_model || "—"}
                        </td>
                        <td className="mono" style={{ color: "var(--t2)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                          {t.route?.final_provider || "—"}
                        </td>
                        <td className="mono" style={{ color: "var(--t2)" }}>{t.span_count}</td>
                        <td className="mono" style={{ color: "var(--t2)" }}>
                          {t.duration_ms != null ? `${t.duration_ms}ms` : "—"}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            ) : (
              <div style={{ padding: "24px 16px", textAlign: "center", color: "var(--t2)", fontSize: 12 }}>
                No requests yet — send a chat completion to see traces here.
              </div>
            )}
          </div>
        </div>

        {/* Right: trace detail */}
        {selectedReqId && (
          <div style={{ width: 380, flexShrink: 0 }}>
            <div className="kicker-lg" style={{ marginBottom: 8 }}>Trace</div>
            <div className="card" style={{ padding: 0, overflow: "hidden" }}>

              {/* Header */}
              <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--border)" }}>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--teal)", marginBottom: 4, wordBreak: "break-all" }}>{selectedReqId}</div>
                {selectedTrace && (
                  <div style={{ display: "flex", flexWrap: "wrap", gap: "3px 10px" }}>
                    {[
                      ["model",    selectedTrace.route?.final_model],
                      ["provider", selectedTrace.route?.final_provider],
                      ["reason",   selectedTrace.route?.final_reason],
                      ["dur",      selectedTrace.duration_ms != null ? `${selectedTrace.duration_ms}ms` : null],
                    ].filter(([,v]) => v).map(([k, v]) => (
                      <span key={k} style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--t2)" }}>
                        <span style={{ color: "var(--t3)" }}>{k} </span>{v}
                      </span>
                    ))}
                    {selectedTrace.status_code === "error" && (
                      <span style={{ fontSize: 11, fontFamily: "var(--font-mono)", color: "var(--red)", flexBasis: "100%" }}>
                        error{selectedTrace.status_message ? `: ${selectedTrace.status_message}` : ""}
                      </span>
                    )}
                  </div>
                )}
              </div>

              {traceFetching && !traceDetail && (
                <div style={{ padding: "12px 14px", color: "var(--t3)", fontSize: 12, fontFamily: "var(--font-mono)" }}>loading…</div>
              )}

              {(traceDetail?.route || selectedTrace?.route) && (
                <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--border)" }}>
                  <div className="kicker" style={{ marginBottom: 8 }}>Route summary</div>
                  <div style={{ display: "flex", flexWrap: "wrap", gap: 6, marginBottom: 8 }}>
                    {selectedTrace?.route?.final_provider && <Badge status="ok" label={selectedTrace.route.final_provider} />}
                    {selectedTrace?.route?.final_model && <Badge status="healthy" label={selectedTrace.route.final_model} />}
                    {selectedTrace?.route?.final_reason && <Badge status="queued" label={describeRouteReason(selectedTrace.route.final_reason)} />}
                    {selectedTrace?.status_code === "error" && <Badge status="error" label={selectedTrace.status_message || "request failed"} />}
                  </div>
                  {selectedTrace?.route?.fallback_from && (
                    <div style={{ fontSize: 11, color: "var(--amber)", lineHeight: 1.45 }}>
                      Fallback from {selectedTrace.route.fallback_from} before the selected route completed.
                    </div>
                  )}
                </div>
              )}

              {/* Span waterfall — always available from in-memory tracer */}
              {traceDetail?.spans && traceDetail.spans.length > 0 && (
                <div style={{ padding: "10px 14px", borderBottom: traceDetail.route?.candidates?.length ? "1px solid var(--border)" : undefined }}>
                  <div className="kicker" style={{ marginBottom: 8 }}>
                    Spans — {computedSpans.length} · {totalMs}ms total
                  </div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
                    {computedSpans.map(span => {
                      const leftPct = (span.startMs / totalMs) * 100;
                      const widthPct = Math.max((span.durMs / totalMs) * 100, 1.5);
                      const isSel = selectedSpan === span.name;
                      const isErr = span.raw.status_code === "error";
                      return (
                        <div
                          key={span.name}
                          onClick={() => setSelectedSpan(s => s === span.name ? null : span.name)}
                          style={{ cursor: "pointer", padding: "3px 0", display: "flex", alignItems: "center", gap: 8, background: isSel ? "var(--bg3)" : "transparent", borderRadius: "var(--radius-sm)" }}
                        >
                          <div style={{ width: 130, flexShrink: 0, fontFamily: "var(--font-mono)", fontSize: 10, color: isErr ? "var(--red)" : isSel ? "var(--t0)" : "var(--t2)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                            {span.name}
                          </div>
                          <div style={{ flex: 1, height: 12, position: "relative", background: "var(--bg3)", borderRadius: 2, overflow: "hidden" }}>
                            <div style={{ position: "absolute", left: `${leftPct}%`, width: `${widthPct}%`, height: "100%", background: isErr ? "var(--red)" : span.color, borderRadius: 2, minWidth: 2 }} />
                          </div>
                          <div style={{ width: 44, flexShrink: 0, fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", textAlign: "right" }}>{span.durMs}ms</div>
                        </div>
                      );
                    })}
                  </div>

                  {/* Selected span detail */}
                  {selectedSpan && (() => {
                    const span = computedSpans.find(s => s.name === selectedSpan);
                    if (!span) return null;
                    const attrs = span.raw.attributes ?? {};
                    const attrEntries = Object.entries(attrs).filter(([, v]) => v != null && v !== "");
                    return (
                      <div style={{ marginTop: 10, padding: "8px 10px", background: "var(--bg3)", borderRadius: "var(--radius-sm)" }}>
                        <div className="kicker" style={{ marginBottom: 6 }}>{selectedSpan}</div>
                        <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
                          {[
                            ["span_id",  span.raw.span_id],
                            ["start",    `+${span.startMs}ms`],
                            ["duration", `${span.durMs}ms`],
                            ["status",   span.raw.status_code],
                          ].filter(([,v]) => v).map(([k, v]) => (
                            <div key={k} style={{ display: "flex", gap: 8 }}>
                              <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", width: 60, flexShrink: 0 }}>{k}</span>
                              <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--teal)" }}>{v}</span>
                            </div>
                          ))}
                          {attrEntries.length > 0 && (
                            <div style={{ marginTop: 4, borderTop: "1px solid var(--border)", paddingTop: 4, display: "flex", flexDirection: "column", gap: 2 }}>
                              {attrEntries.map(([k, v]) => (
                                <div key={k} style={{ display: "flex", gap: 8 }}>
                                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", width: 60, flexShrink: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={k}>{k.split(".").pop()}</span>
                                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t1)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{String(v)}</span>
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                    );
                  })()}
                </div>
              )}

              {traceTimeline.length > 0 && (
                <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--border)" }}>
                  <div className="kicker" style={{ marginBottom: 8 }}>Event flow</div>
                  <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                    {traceTimeline.map((event, index) => (
                      <div key={`${event.timestamp}-${event.name}-${index}`} style={{ display: "grid", gridTemplateColumns: "56px 92px 1fr", gap: 10, alignItems: "start" }}>
                        <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>{event.offsetLabel}</div>
                        <Badge
                          status={
                            event.phase === "provider" ? "healthy" :
                            event.phase === "cache" ? "queued" :
                            event.phase === "routing" ? "ok" :
                            event.phase === "response" ? "done" :
                            "disabled"
                          }
                          label={event.phase}
                        />
                        <div>
                          <div style={{ fontSize: 12, color: "var(--t0)", marginBottom: 4 }}>{event.name}</div>
                          {event.attributes && Object.keys(event.attributes).length > 0 && (
                            <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                              {Object.entries(event.attributes).slice(0, 4).map(([key, value]) => (
                                <span key={key} style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                                  {formatTraceAttributeKey(key)} <span style={{ color: "var(--t1)" }}>{formatTraceAttributeValue(value)}</span>
                                </span>
                              ))}
                            </div>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Route candidates */}
              {(() => {
                type Candidate = NonNullable<NonNullable<TraceListItem["route"]>["candidates"]>[number];
                const candidates: Candidate[] = traceDetail?.route?.candidates ?? selectedTrace?.route?.candidates ?? [];
                return candidates.length > 0 && (
                <div style={{ padding: "10px 14px" }}>
                  <div className="kicker" style={{ marginBottom: 6 }}>Route candidates</div>
                  {candidates.map((c, i) => (
                    <div key={i} style={{ padding: "8px 0", borderBottom: "1px solid var(--border)" }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t0)", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                          {c.provider}/{c.model || "no model"}
                        </span>
                        <Badge status={
                          routeOutcomeTone(c.outcome) === "healthy" ? "done" :
                          routeOutcomeTone(c.outcome) === "danger" ? "error" :
                          routeOutcomeTone(c.outcome) === "warning" ? "warn" :
                          "disabled"
                        } label={c.outcome || "unknown"} />
                      </div>
                      <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                        {c.reason && <Badge status="queued" label={describeRouteReason(c.reason)} />}
                        {c.skip_reason && <Badge status="warn" label={describeRouteSkipReason(c.skip_reason)} />}
                        {c.health_status && <Badge status={
                          healthStatusTone(c.health_status) === "healthy" ? "healthy" :
                          healthStatusTone(c.health_status) === "danger" ? "error" :
                          healthStatusTone(c.health_status) === "warning" ? "warn" :
                          "disabled"
                        } label={describeHealthStatus(c.health_status)} />}
                        {c.latency_ms != null && c.latency_ms > 0 && <Badge status="disabled" label={`${c.latency_ms}ms`} />}
                        {c.estimated_usd && <Badge status="disabled" label={c.estimated_usd} />}
                      </div>
                      {c.detail && (
                        <div style={{ marginTop: 6, fontSize: 11, color: "var(--t2)", lineHeight: 1.45 }}>
                          {c.detail}
                        </div>
                      )}
                      {(c.failover_from || c.failover_to || c.attempt || c.retry_count) && (
                        <div style={{ marginTop: 6, display: "flex", flexWrap: "wrap", gap: 10 }}>
                          {c.attempt != null && (
                            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                              attempt <span style={{ color: "var(--t1)" }}>{c.attempt}</span>
                            </span>
                          )}
                          {c.retry_count != null && (
                            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                              retries <span style={{ color: "var(--t1)" }}>{c.retry_count}</span>
                            </span>
                          )}
                          {c.failover_from && c.failover_to && (
                            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                              failover <span style={{ color: "var(--t1)" }}>{c.failover_from}</span> → <span style={{ color: "var(--t1)" }}>{c.failover_to}</span>
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              );
              })()}

              {!traceFetching && !traceDetail && (
                <div style={{ padding: "12px 14px", color: "var(--t3)", fontSize: 12 }}>
                  No trace detail available.
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
