import { buildTraceTimeline, formatTraceAttributeKey, formatTraceAttributeValue } from "../../lib/runtime-utils";
import type { TraceSpanRecord } from "../../types/runtime";
import { DefinitionList, EmptyState, InlineNotice, ShellSection, StatusPill, Surface } from "../shared/ConsolePrimitives";

type Props = {
  loading: boolean;
  error: string;
  spans: TraceSpanRecord[];
  traceStartedAt?: string;
};

export function TraceWorkbench({ loading, error, spans, traceStartedAt }: Props) {
  const timeline = buildTraceTimeline(spans, traceStartedAt);

  return (
    <ShellSection eyebrow="Execution" title="Trace">
      <div className="stack-md">
        <Surface>
          {loading ? <p className="body-muted">Loading trace...</p> : null}
          {!loading && error ? <InlineNotice message={error} tone="error" /> : null}
          {!loading && !error && spans.length === 0 ? <EmptyState title="No trace" detail="Run a request first." /> : null}
          {!loading && !error && spans.length > 0 ? (
            <div className="stack-md">
              <div className="action-row action-row--wide">
                <StatusPill label={`${spans.length} span${spans.length === 1 ? "" : "s"}`} tone="neutral" />
                <StatusPill label={`${timeline.length} event${timeline.length === 1 ? "" : "s"}`} tone="neutral" />
                {traceStartedAt ? <StatusPill label={`started ${new Date(traceStartedAt).toLocaleTimeString()}`} tone="neutral" /> : null}
              </div>

              <div className="trace-timeline">
                {timeline.map((event, index) => (
                  <article className="trace-timeline__item" key={`${event.timestamp}-${event.name}-${index}`}>
                    <div className="trace-timeline__meta">
                      <StatusPill label={event.phase} tone={event.phase === "response" ? "healthy" : event.phase === "provider" ? "warning" : "neutral"} />
                      <span>{event.offsetLabel}</span>
                    </div>
                    <strong>{event.name}</strong>
                    <p className="body-muted">
                      {event.spanName} · {new Date(event.timestamp).toLocaleTimeString()}
                    </p>
                    {event.attributes && Object.keys(event.attributes).length > 0 ? (
                      <DefinitionList
                        compact
                        items={Object.entries(event.attributes)
                          .slice(0, 6)
                          .map(([key, value]) => ({
                            label: formatTraceAttributeKey(key),
                            value: formatTraceAttributeValue(value),
                          }))}
                      />
                    ) : null}
                  </article>
                ))}
              </div>
            </div>
          ) : null}
        </Surface>

        {spans.length > 0 ? (
          <Surface>
            <div className="stack-sm">
              {spans.map((span) => (
                <details className="trace-item" key={span.span_id} open={span.span_id === spans[0]?.span_id}>
                  <summary className="trace-item__summary">
                    <span>{span.name}</span>
                    <span>{span.status_code || "unset"}</span>
                  </summary>
                  <div className="trace-item__body">
                    <p className="body-muted">
                      {span.start_time || "Unknown start"} {span.end_time ? `-> ${span.end_time}` : ""}
                    </p>
                    <p className="body-muted">{span.events?.length ?? 0} events</p>
                    {span.attributes && Object.keys(span.attributes).length > 0 ? (
                      <DefinitionList
                        compact
                        items={Object.entries(span.attributes).map(([key, value]) => ({
                          label: formatTraceAttributeKey(key),
                          value: formatTraceAttributeValue(value),
                        }))}
                      />
                    ) : null}
                  </div>
                </details>
              ))}
            </div>
          </Surface>
        ) : null}
      </div>
    </ShellSection>
  );
}
