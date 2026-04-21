import { useMemo } from "react";

import { Panel } from "./Panel";
import type { TraceEventRecord, TraceSpanRecord } from "../types/runtime";

type TracePanelProps = {
  requestId: string;
  loading: boolean;
  error: string;
  traceStartedAt?: string;
  spans: TraceSpanRecord[];
};

type TimelineItem = TraceEventRecord & {
  offsetLabel: string;
  spanName: string;
  spanKind: string;
};

export function TracePanel(props: TracePanelProps) {
  const timeline = useMemo(() => {
    const flattened: TimelineItem[] = [];
    const startSource = props.traceStartedAt || props.spans[0]?.start_time || "";
    const startMs = Date.parse(startSource);

    for (const span of props.spans) {
      for (const event of span.events ?? []) {
        const currentMs = Date.parse(event.timestamp);
        const offsetMs = Number.isFinite(startMs) && Number.isFinite(currentMs) ? Math.max(0, currentMs - startMs) : 0;
        flattened.push({
          ...event,
          offsetLabel: `${offsetMs} ms`,
          spanName: span.name,
          spanKind: span.kind || "internal",
        });
      }
    }

    flattened.sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp));
    return flattened;
  }, [props.spans, props.traceStartedAt]);

  return (
    <Panel eyebrow="Trace" title="Request timeline">
      <div className="mt-4 rounded-2xl bg-slate-50/90 p-4">
        <div className="flex flex-wrap items-center gap-2">
          <TraceBadge label="Request" value={props.requestId || "none"} />
          <TraceBadge label="Spans" value={props.spans.length.toString()} />
          <TraceBadge label="Events" value={timeline.length.toString()} />
          {props.traceStartedAt ? <TraceBadge label="Started" value={new Date(props.traceStartedAt).toLocaleTimeString()} /> : null}
        </div>
      </div>

      {props.loading ? (
        <div className="mt-4 rounded-2xl border border-slate-200/80 bg-white px-4 py-4 text-sm text-slate-600">Loading trace…</div>
      ) : null}

      {!props.loading && props.error ? (
        <div className="mt-4 rounded-2xl border border-red-200 bg-red-50 px-4 py-4 text-sm text-red-700">{props.error}</div>
      ) : null}

      {!props.loading && !props.error && props.spans.length === 0 ? (
        <div className="mt-4 rounded-2xl border border-slate-200/80 bg-white px-4 py-4 text-sm text-slate-600">
          Run a chat request to inspect its gateway timeline here.
        </div>
      ) : null}

      {!props.loading && !props.error && props.spans.length > 0 ? (
        <div className="mt-4 grid gap-4">
          <section className="rounded-2xl border border-slate-200/80 bg-white/95 px-4 py-4">
            <h4 className="text-sm font-semibold uppercase tracking-[0.18em] text-slate-500">OpenTelemetry-style spans</h4>
            <ol className="mt-3 grid gap-3">
              {props.spans.map((span) => (
                <li key={span.span_id} className="rounded-2xl bg-slate-50 px-4 py-4">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <p className="text-sm font-semibold text-slate-900">{span.name}</p>
                      <p className="mt-1 text-xs uppercase tracking-[0.16em] text-slate-500">
                        {span.kind || "internal"} {span.status_code ? `· ${span.status_code}` : ""}
                      </p>
                    </div>
                    <p className="text-xs text-slate-500">{formatSpanDuration(span.start_time, span.end_time)}</p>
                  </div>
                  {span.status_message ? <p className="mt-2 text-sm text-red-700">{span.status_message}</p> : null}
                  {span.attributes && Object.keys(span.attributes).length > 0 ? (
                    <dl className="mt-3 grid gap-2 md:grid-cols-2">
                      {Object.entries(span.attributes).map(([key, value]) => (
                        <div key={key} className="rounded-xl bg-white px-3 py-2">
                          <dt className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-500">{formatLabel(key)}</dt>
                          <dd className="mt-1 break-all text-sm text-slate-800">{formatValue(value)}</dd>
                        </div>
                      ))}
                    </dl>
                  ) : null}
                </li>
              ))}
            </ol>
          </section>

          <section className="rounded-2xl border border-slate-200/80 bg-white/95 px-4 py-4">
            <h4 className="text-sm font-semibold uppercase tracking-[0.18em] text-slate-500">Span Events Timeline</h4>
            {timeline.length > 0 ? (
              <ol className="mt-3 grid gap-3">
                {timeline.map((event, index) => (
                  <li key={`${event.timestamp}-${event.name}-${index}`} className="rounded-2xl bg-slate-50 px-4 py-4">
                    <div className="flex flex-wrap items-start justify-between gap-2">
                      <div>
                        <p className="text-sm font-semibold text-slate-900">{event.name}</p>
                        <p className="mt-1 text-xs uppercase tracking-[0.16em] text-slate-500">
                          {event.offsetLabel} · {event.spanName} · {event.spanKind}
                        </p>
                      </div>
                      <p className="text-xs text-slate-500">{new Date(event.timestamp).toLocaleTimeString()}</p>
                    </div>
                    {event.attributes && Object.keys(event.attributes).length > 0 ? (
                      <dl className="mt-3 grid gap-2 md:grid-cols-2">
                        {Object.entries(event.attributes).map(([key, value]) => (
                          <div key={key} className="rounded-xl bg-white px-3 py-2">
                            <dt className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-500">{formatLabel(key)}</dt>
                            <dd className="mt-1 break-all text-sm text-slate-800">{formatValue(value)}</dd>
                          </div>
                        ))}
                      </dl>
                    ) : null}
                  </li>
                ))}
              </ol>
            ) : (
              <div className="mt-3 rounded-2xl bg-slate-50 px-4 py-4 text-sm text-slate-600">
                This trace does not contain span events yet.
              </div>
            )}
          </section>
        </div>
      ) : null}
    </Panel>
  );
}

function TraceBadge(props: { label: string; value: string }) {
  return (
    <span className="rounded-full border border-slate-200/80 bg-white/95 px-3 py-2 text-xs font-medium text-slate-700">
      <span className="mr-1 text-slate-500">{props.label}:</span>
      {props.value}
    </span>
  );
}

function formatLabel(value: string): string {
  return value.split("_").join(" ");
}

function formatSpanDuration(start?: string, end?: string): string {
  if (!start || !end) {
    return "duration n/a";
  }
  const startMs = Date.parse(start);
  const endMs = Date.parse(end);
  if (!Number.isFinite(startMs) || !Number.isFinite(endMs)) {
    return "duration n/a";
  }
  return `${Math.max(0, endMs - startMs)} ms`;
}

function formatValue(value: unknown): string {
  if (value === null || value === undefined) {
    return "n/a";
  }
  if (typeof value === "string") {
    return value;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return JSON.stringify(value);
}
