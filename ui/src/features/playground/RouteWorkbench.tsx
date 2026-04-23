import { formatUsd } from "../../lib/format";
import {
  buildSemanticCacheInsight,
  countRouteHealthStatuses,
  describeCachePath,
  describeHealthStatus,
  describeRouteReason,
  describeRouteRecovery,
  healthStatusTone,
  routeOutcomeTone,
  type TraceRouteRecord,
} from "../../lib/runtime-utils";
import type { RuntimeHeaders, TraceSpanRecord } from "../../types/runtime";
import { DefinitionList, EmptyState, MetricTile, ShellSection, StatusPill, Surface } from "../shared/ConsolePrimitives";

type Props = {
  runtimeHeaders: RuntimeHeaders | null;
  route: TraceRouteRecord | null;
  spans?: TraceSpanRecord[];
};

export function RouteWorkbench({ runtimeHeaders, route, spans = [] }: Props) {
  const cachePath = describeCachePath(runtimeHeaders);
  const candidateCount = route?.candidates?.length ?? 0;
  const failoverCount = route?.failovers?.length ?? 0;
  const selectedCandidate = route?.candidates?.find((candidate) => candidate.outcome === "selected") ?? null;
  const skippedCount = route?.candidates?.filter((candidate) => candidate.outcome === "skipped" || candidate.outcome === "denied" || candidate.skip_reason).length ?? 0;
  const healthCounts = countRouteHealthStatuses(route);
  const finalHealth = selectedCandidate?.health_status;
  const finalHealthLabel = finalHealth ? describeHealthStatus(finalHealth) : "No health signal";
  const finalHealthTone = healthStatusTone(finalHealth);
  const routeRecovery = describeRouteRecovery(route, runtimeHeaders);
  const semanticInsight = buildSemanticCacheInsight(runtimeHeaders, spans);

  return (
    <div className="stack-md">
      <ShellSection eyebrow="Decision" title="Route decision">
        {runtimeHeaders || route ? (
          <div className="stack-md">
            <Surface tone={finalHealthTone === "danger" ? "danger" : "strong"} className="route-summary">
              <div className="route-summary__headline">
                <div className="stack-sm">
                  <div className="action-row action-row--wide">
                    <StatusPill label={finalHealthLabel} tone={finalHealthTone} />
                    <StatusPill
                      label={failoverCount > 0 ? `${failoverCount} failover hop${failoverCount === 1 ? "" : "s"}` : "Direct route"}
                      tone={failoverCount > 0 ? "warning" : "healthy"}
                    />
                    {selectedCandidate?.provider_kind ? <StatusPill label={selectedCandidate.provider_kind} tone="neutral" /> : null}
                  </div>
                  <div className="route-summary__title-row">
                    <h3 className="route-summary__title">
                      {route?.final_provider || runtimeHeaders?.provider || "Unknown provider"} · {route?.final_model || runtimeHeaders?.resolvedModel || "No resolved model"}
                    </h3>
                    <p className="body-muted">{describeRouteReason(route?.final_reason || runtimeHeaders?.routeReason)}</p>
                  </div>
                </div>

                <div className="route-summary__cluster">
                  <div className="route-summary__metric">
                    <span>Fallback</span>
                    <strong>{failoverCount > 0 ? routeRecovery : "Not used"}</strong>
                  </div>
                  <div className="route-summary__metric">
                    <span>Cache path</span>
                    <strong>{cachePath.title}</strong>
                  </div>
                </div>
              </div>

              <div className="route-summary__health">
                <div className="route-health-stat">
                  <span>Healthy</span>
                  <strong>{healthCounts.healthy}</strong>
                </div>
                <div className="route-health-stat">
                  <span>Recovering</span>
                  <strong>{healthCounts.warning}</strong>
                </div>
                <div className="route-health-stat">
                  <span>Unavailable</span>
                  <strong>{healthCounts.danger}</strong>
                </div>
              </div>
            </Surface>

            <div className="metric-grid metric-grid--wide">
              <MetricTile
                label="Final route"
                value={route?.final_provider || runtimeHeaders?.provider || "Unknown"}
                detail={route?.final_model || runtimeHeaders?.resolvedModel || "No resolved model"}
                tone={finalHealthTone === "healthy" ? "healthy" : finalHealthTone === "warning" ? "warning" : "neutral"}
              />
              <MetricTile
                label="Why selected"
                value={describeRouteReason(route?.final_reason || runtimeHeaders?.routeReason)}
                detail={runtimeHeaders?.providerKind ? `Provider kind ${runtimeHeaders.providerKind}` : "Provider kind not returned"}
                tone="neutral"
              />
              <MetricTile
                label="Skipped"
                value={String(skippedCount)}
                detail={candidateCount > 0 ? `${candidateCount} candidate${candidateCount === 1 ? "" : "s"} inspected` : "No candidate report returned"}
                tone={skippedCount > 0 ? "warning" : "neutral"}
              />
              <MetricTile
                label="Final cost"
                value={formatUsd(runtimeHeaders?.costUsd || "0")}
                detail={selectedCandidate?.estimated_usd ? `Preflight estimate ${formatUsd(selectedCandidate.estimated_usd)}` : "No preflight estimate returned"}
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
                      { label: "Recovery path", value: routeRecovery },
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

            {semanticInsight ? (
              <Surface className="semantic-inspector">
                <div className="semantic-inspector__head">
                  <div className="stack-sm">
                    <div className="action-row action-row--wide">
                      <StatusPill label={semanticInsight.title} tone={semanticInsight.tone} />
                      <StatusPill label={semanticInsight.writebackStatus} tone={semanticInsight.writebackTone} />
                    </div>
                    <div className="semantic-inspector__title-row">
                      <h3 className="route-summary__title">Semantic cache</h3>
                      <p className="body-muted">{semanticInsight.summary}</p>
                    </div>
                  </div>
                  <div className="semantic-inspector__metric">
                    <span>Similarity</span>
                    <strong>{semanticInsight.similarity}</strong>
                  </div>
                </div>

                <div className="two-column-grid two-column-grid--compact">
                  <div className="stack-sm">
                    <p className="body-muted">{semanticInsight.detail}</p>
                    <DefinitionList
                      compact
                      items={[
                        { label: "Strategy", value: semanticInsight.strategy },
                        { label: "Index", value: semanticInsight.index },
                        { label: "Scope", value: semanticInsight.scope },
                      ]}
                    />
                  </div>

                  <div className="stack-sm">
                    <p className="body-muted">{semanticInsight.writebackDetail}</p>
                    <DefinitionList
                      compact
                      items={[
                        { label: "Cache outcome", value: semanticInsight.title },
                        { label: "Writeback", value: semanticInsight.writebackStatus },
                        { label: "Reason", value: cachePath.title },
                      ]}
                    />
                  </div>
                </div>
              </Surface>
            ) : null}
          </div>
        ) : (
          <EmptyState title="No route data" detail="Run a request to inspect routing, cache path, and failover behavior." />
        )}
      </ShellSection>

      <ShellSection eyebrow="Evaluation" title="Provider candidates">
        {route?.candidates?.length ? (
          <div className="trace-timeline">
            {route.candidates.map((candidate, index) => (
              <article className="trace-timeline__item" key={`${candidate.provider}-${candidate.model}-${candidate.index ?? index}`}>
                <div className="trace-timeline__meta">
                  <StatusPill label={candidate.outcome || "unknown"} tone={routeOutcomeTone(candidate.outcome)} />
                  {candidate.health_status ? <StatusPill label={describeHealthStatus(candidate.health_status)} tone={healthStatusTone(candidate.health_status)} /> : null}
                  <span>candidate {candidate.index ?? index}</span>
                </div>
                <strong>{candidate.provider || "unknown provider"} · {candidate.model || "unknown model"}</strong>
                <p className="body-muted">
                  {candidate.outcome === "selected"
                    ? `Selected because ${describeRouteReason(candidate.reason).toLowerCase()}.`
                    : candidate.skip_reason
                      ? `Skipped: ${candidate.skip_reason}. ${describeRouteReason(candidate.reason)}.`
                      : describeRouteReason(candidate.reason)}
                </p>
                <DefinitionList
                  compact
                  items={[
                    { label: "Provider kind", value: candidate.provider_kind || "unknown" },
                    { label: "Health", value: candidate.health_status ? describeHealthStatus(candidate.health_status) : "No health signal" },
                    { label: "Preflight cost", value: candidate.estimated_usd ? formatUsd(candidate.estimated_usd) : "$0.00" },
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
