import { buildTraceTimeline, formatTraceAttributeKey, formatTraceAttributeValue } from "../../lib/runtime-utils";
import type { TraceSpanRecord } from "../../types/runtime";
import { DefinitionList, EmptyState, InlineNotice, ShellSection, StatusPill, Surface } from "../shared/ConsolePrimitives";
import "../shared/Workbench.css";

type Props = {
  loading: boolean;
  error: string;
  spans: TraceSpanRecord[];
  traceStartedAt?: string;
};

export function TraceWorkbench({ loading, error, spans, traceStartedAt }: Props) {
  const timeline = buildTraceTimeline(spans, traceStartedAt);
  const routingEvents = timeline.filter((event) => event.phase === "routing");
  const providerEvents = timeline.filter((event) => event.phase === "provider");
  const responseEvents = timeline.filter((event) => event.phase === "response" || event.phase === "usage");

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

              <div className="trace-highlight-grid">
                <div className="trace-highlight-card">
                  <span>Routing</span>
                  <strong>{routingEvents.at(-1)?.name || "No routing events"}</strong>
                  <p className="body-muted">
                    {routingEvents.length > 0 ? `${routingEvents.length} routing checkpoints recorded` : "The trace did not emit routing checkpoints."}
                  </p>
                </div>
                <div className="trace-highlight-card">
                  <span>Provider path</span>
                  <strong>{providerEvents.at(-1)?.name || "No provider events"}</strong>
                  <p className="body-muted">
                    {providerEvents.length > 0 ? `${providerEvents.length} provider transitions recorded` : "No provider start/finish events were captured."}
                  </p>
                </div>
                <div className="trace-highlight-card">
                  <span>Completion</span>
                  <strong>{responseEvents.at(-1)?.name || "No response event"}</strong>
                  <p className="body-muted">
                    {responseEvents.length > 0 ? `${responseEvents.length} usage or response checkpoints recorded` : "The trace ended without explicit usage/response markers."}
                  </p>
                </div>
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
                          .slice(0, 4)
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
