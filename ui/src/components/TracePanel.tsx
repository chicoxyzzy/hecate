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
      <div className="info-block action-row">
        <TraceBadge label="Request" value={props.requestId || "none"} />
        <TraceBadge label="Spans" value={props.spans.length.toString()} />
        <TraceBadge label="Events" value={timeline.length.toString()} />
        {props.traceStartedAt ? <TraceBadge label="Started" value={new Date(props.traceStartedAt).toLocaleTimeString()} /> : null}
      </div>

      {props.loading ? (
        <div className="inline-notice inline-notice--success" style={{ marginTop: "1rem" }}>Loading trace…</div>
      ) : null}

      {!props.loading && props.error ? (
        <div className="inline-notice inline-notice--error" style={{ marginTop: "1rem" }}>{props.error}</div>
      ) : null}

      {!props.loading && !props.error && props.spans.length === 0 ? (
        <div className="trace-section" style={{ marginTop: "1rem" }}>
          <p className="body-muted">Run a chat request to inspect its gateway timeline here.</p>
        </div>
      ) : null}

      {!props.loading && !props.error && props.spans.length > 0 ? (
        <div className="stack-md" style={{ marginTop: "1rem" }}>
          <section className="trace-section">
            <h4 className="trace-section__title">OpenTelemetry-style spans</h4>
            <ol className="trace-entry-list">
              {props.spans.map((span) => (
                <li key={span.span_id} className="trace-entry">
                  <div className="trace-entry__head">
                    <div>
                      <p className="trace-entry__name">{span.name}</p>
                      <p className="trace-entry__meta">
                        {span.kind || "internal"} {span.status_code ? `· ${span.status_code}` : ""}
                      </p>
                    </div>
                    <p className="trace-entry__duration">{formatSpanDuration(span.start_time, span.end_time)}</p>
                  </div>
                  {span.status_message ? <p className="trace-entry__error">{span.status_message}</p> : null}
                  {span.attributes && Object.keys(span.attributes).length > 0 ? (
                    <dl className="trace-attr-grid">
                      {Object.entries(span.attributes).map(([key, value]) => (
                        <div key={key} className="trace-attr">
                          <dt>{formatLabel(key)}</dt>
                          <dd>{formatValue(value)}</dd>
                        </div>
                      ))}
                    </dl>
                  ) : null}
                </li>
              ))}
            </ol>
          </section>

          <section className="trace-section">
            <h4 className="trace-section__title">Span Events Timeline</h4>
            {timeline.length > 0 ? (
              <ol className="trace-entry-list">
                {timeline.map((event, index) => (
                  <li key={`${event.timestamp}-${event.name}-${index}`} className="trace-entry">
                    <div className="trace-entry__head">
                      <div>
                        <p className="trace-entry__name">{event.name}</p>
                        <p className="trace-entry__meta">
                          {event.offsetLabel} · {event.spanName} · {event.spanKind}
                        </p>
                      </div>
                      <p className="trace-entry__duration">{new Date(event.timestamp).toLocaleTimeString()}</p>
                    </div>
                    {event.attributes && Object.keys(event.attributes).length > 0 ? (
                      <dl className="trace-attr-grid">
                        {Object.entries(event.attributes).map(([key, value]) => (
                          <div key={key} className="trace-attr">
                            <dt>{formatLabel(key)}</dt>
                            <dd>{formatValue(value)}</dd>
                          </div>
                        ))}
                      </dl>
                    ) : null}
                  </li>
                ))}
              </ol>
            ) : (
              <p className="body-muted" style={{ marginTop: "0.75rem" }}>
                This trace does not contain span events yet.
              </p>
            )}
          </section>
        </div>
      ) : null}
    </Panel>
  );
}

function TraceBadge(props: { label: string; value: string }) {
  return (
    <span className="trace-badge">
      <span className="trace-badge__label">{props.label}:</span>
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
