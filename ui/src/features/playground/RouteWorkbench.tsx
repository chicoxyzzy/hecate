import { formatUsd } from "../../lib/format";
import { describeCachePath, describeRouteReason, routeOutcomeTone, type TraceRouteRecord } from "../../lib/runtime-utils";
import type { RuntimeHeaders } from "../../types/runtime";
import { DefinitionList, EmptyState, MetricTile, ShellSection, StatusPill, Surface } from "../shared/ConsolePrimitives";

type Props = {
  runtimeHeaders: RuntimeHeaders | null;
  route: TraceRouteRecord | null;
};

export function RouteWorkbench({ runtimeHeaders, route }: Props) {
  const cachePath = describeCachePath(runtimeHeaders);
  const candidateCount = route?.candidates?.length ?? 0;
  const failoverCount = route?.failovers?.length ?? 0;

  return (
    <div className="stack-md">
      <ShellSection eyebrow="Runtime decision" title="Routing">
        {runtimeHeaders || route ? (
          <div className="stack-md">
            <div className="metric-grid metric-grid--wide">
              <MetricTile
                label="Final route"
                value={route?.final_provider || runtimeHeaders?.provider || "Unknown"}
                detail={route?.final_model || runtimeHeaders?.resolvedModel || "No resolved model"}
                tone="neutral"
              />
              <MetricTile
                label="Decision reason"
                value={describeRouteReason(route?.final_reason || runtimeHeaders?.routeReason)}
                detail={runtimeHeaders?.providerKind ? `Provider kind ${runtimeHeaders.providerKind}` : "Provider kind not returned"}
                tone="neutral"
              />
              <MetricTile
                label="Attempts"
                value={runtimeHeaders?.attempts || "1"}
                detail={`Retries ${runtimeHeaders?.retries || "0"}`}
                tone={runtimeHeaders?.retries && runtimeHeaders.retries !== "0" ? "warning" : "neutral"}
              />
              <MetricTile
                label="Estimated cost"
                value={formatUsd(runtimeHeaders?.costUsd || "0")}
                detail={candidateCount > 0 ? `${candidateCount} route candidates inspected` : "No candidate report returned"}
                tone="neutral"
              />
            </div>

            <div className="two-column-grid two-column-grid--compact">
              <Surface>
                <div className="stack-sm">
                  <div className="action-row action-row--wide">
                    <StatusPill label={cachePath.title} tone={cachePath.tone} />
                    <StatusPill
                      label={runtimeHeaders?.cache === "true" ? `cache ${runtimeHeaders.cacheType || "hit"}` : "cache miss"}
                      tone={runtimeHeaders?.cache === "true" ? "healthy" : "neutral"}
                    />
                  </div>
                  <p className="body-muted">{cachePath.detail}</p>
                  <DefinitionList
                    compact
                    items={[
                      { label: "Request ID", value: runtimeHeaders?.requestId || "n/a" },
                      { label: "Trace ID", value: runtimeHeaders?.traceId || "n/a" },
                      { label: "Fallback from", value: runtimeHeaders?.fallbackFrom || route?.fallback_from || "n/a" },
                    ]}
                  />
                </div>
              </Surface>

              <Surface>
                <div className="stack-sm">
                  <div className="action-row action-row--wide">
                    <StatusPill label={failoverCount > 0 ? `${failoverCount} failover hop${failoverCount === 1 ? "" : "s"}` : "No failover"} tone={failoverCount > 0 ? "warning" : "neutral"} />
                    {runtimeHeaders?.semanticStrategy ? <StatusPill label={`semantic ${runtimeHeaders.semanticStrategy}`} tone="healthy" /> : null}
                  </div>
                  <DefinitionList
                    compact
                    items={[
                      { label: "Requested model", value: runtimeHeaders?.requestedModel || "n/a" },
                      { label: "Resolved model", value: runtimeHeaders?.resolvedModel || route?.final_model || "n/a" },
                      {
                        label: "Semantic details",
                        value: runtimeHeaders?.semanticStrategy
                          ? `${runtimeHeaders.semanticIndex || "unknown"} · similarity ${runtimeHeaders.semanticSimilarity || "n/a"}`
                          : "No semantic cache metadata returned",
                      },
                    ]}
                  />
                </div>
              </Surface>
            </div>
          </div>
        ) : (
          <EmptyState title="No route data" detail="Run a request to inspect routing, cache path, and failover behavior." />
        )}
      </ShellSection>

      <ShellSection eyebrow="Candidate evaluation" title="Route decision tree">
        {route?.candidates?.length ? (
          <div className="trace-timeline">
            {route.candidates.map((candidate, index) => (
              <article className="trace-timeline__item" key={`${candidate.provider}-${candidate.model}-${candidate.index ?? index}`}>
                <div className="trace-timeline__meta">
                  <StatusPill label={candidate.outcome || "unknown"} tone={routeOutcomeTone(candidate.outcome)} />
                  <span>candidate {candidate.index ?? index}</span>
                </div>
                <strong>{candidate.provider || "unknown provider"} · {candidate.model || "unknown model"}</strong>
                <p className="body-muted">
                  {describeRouteReason(candidate.reason)}{candidate.health_status ? ` · health ${candidate.health_status}` : ""}
                </p>
                <DefinitionList
                  compact
                  items={[
                    { label: "Provider kind", value: candidate.provider_kind || "unknown" },
                    { label: "Estimated", value: candidate.estimated_usd || "$0.00" },
                    { label: "Attempt", value: String(candidate.attempt || 0) },
                    { label: "Retries", value: String(candidate.retry_count || 0) },
                    { label: "Latency", value: candidate.latency_ms ? `${candidate.latency_ms} ms` : "n/a" },
                    {
                      label: "Skip / error",
                      value: candidate.skip_reason || candidate.detail || "n/a",
                    },
                  ]}
                />
              </article>
            ))}
          </div>
        ) : (
          <EmptyState title="No candidate report" detail="The gateway did not return a structured candidate evaluation list for this request." />
        )}
      </ShellSection>

      <ShellSection eyebrow="Failover path" title="Failover chain">
        {route?.failovers?.length ? (
          <div className="trace-timeline">
            {route.failovers.map((failover, index) => (
              <article className="trace-timeline__item" key={`${failover.timestamp || index}-${failover.from_provider}-${failover.to_provider}`}>
                <div className="trace-timeline__meta">
                  <StatusPill label="failover" tone="warning" />
                  <span>{failover.timestamp ? new Date(failover.timestamp).toLocaleTimeString() : `hop ${index + 1}`}</span>
                </div>
                <strong>{failover.from_provider || "unknown"} → {failover.to_provider || "unknown"}</strong>
                <p className="body-muted">
                  {failover.from_model || "unknown model"} → {failover.to_model || "unknown model"}
                </p>
                <p className="body-muted">{describeRouteReason(failover.reason)}</p>
              </article>
            ))}
          </div>
        ) : (
          <EmptyState title="No failover chain" detail="This request stayed on a single provider route or did not emit failover transitions." />
        )}
      </ShellSection>
    </div>
  );
}
