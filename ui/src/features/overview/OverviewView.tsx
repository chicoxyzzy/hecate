import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatDateTime, formatRelativeCount, formatUsd } from "../../lib/format";
import { EmptyState, MetricTile, ShellSection, StatusPill, Surface, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
  onOpenWorkspace: (workspace: "playground" | "providers" | "access" | "admin") => void;
};

export function OverviewView({ state, actions, onOpenWorkspace }: Props) {
  const lastResponse = state.chatResult?.choices[0]?.message?.content ?? "";
  const nextStepLabel =
    state.session.kind === "anonymous"
      ? "Authenticate"
      : state.localProviderIssues.length > 0
        ? "Fix local runtime"
        : state.runtimeHeaders?.requestId
          ? "Inspect latest request"
          : "Run first request";

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection eyebrow="Workspace" title="Overview">
          <div className="metric-grid metric-grid--wide">
            <MetricTile
              label="Provider health"
              tone={state.healthyProviders === state.providers.length && state.providers.length > 0 ? "healthy" : "warning"}
              value={formatRelativeCount("healthy", state.healthyProviders, state.providers.length)}
            />
            <MetricTile
              label="Local providers"
              tone={state.healthyLocalProviders === state.localProviders.length && state.localProviders.length > 0 ? "healthy" : "warning"}
              value={formatRelativeCount("healthy", state.healthyLocalProviders, state.localProviders.length)}
            />
            <MetricTile
              detail={state.models.length > 0 ? `${state.localModels.length} local / ${state.cloudModels.length} cloud` : undefined}
              label="Discovered models"
              value={`${state.models.length}`}
            />
            <MetricTile
              detail={state.runtimeHeaders?.provider ? `${state.runtimeHeaders.provider} -> ${state.runtimeHeaders.resolvedModel}` : undefined}
              label="Latest routing"
              value={state.runtimeHeaders?.routeReason || "Awaiting request"}
            />
          </div>
        </ShellSection>

        <ShellSection eyebrow="Actions" title={nextStepLabel}>
          <Surface tone="strong">
            <div className="action-row action-row--wide">
              <ToolbarButton onClick={() => onOpenWorkspace("playground")} tone="primary">
                Playground
              </ToolbarButton>
              <ToolbarButton onClick={() => onOpenWorkspace("providers")}>Providers</ToolbarButton>
              <ToolbarButton onClick={() => onOpenWorkspace("access")}>Access</ToolbarButton>
              {state.session.isAdmin ? <ToolbarButton onClick={() => onOpenWorkspace("admin")}>Admin</ToolbarButton> : null}
              <StatusPill
                label={state.session.label}
                tone={
                  state.session.kind === "admin"
                    ? "healthy"
                    : state.session.kind === "tenant"
                      ? "neutral"
                      : state.session.kind === "invalid"
                        ? "danger"
                        : "warning"
                }
              />
            </div>
          </Surface>
        </ShellSection>

        <ShellSection eyebrow="Latest request" title="Latest request">
          <div className="two-column-grid two-column-grid--compact">
            <Surface>
              {state.runtimeHeaders?.requestId ? (
                <div className="stack-md">
                  <div className="action-row">
                    <StatusPill label={`Provider: ${state.runtimeHeaders.provider || "unknown"}`} tone="neutral" />
                    <StatusPill
                      label={`Cache: ${state.runtimeHeaders.cacheType || state.runtimeHeaders.cache || "miss"}`}
                      tone={state.runtimeHeaders.cache === "true" ? "healthy" : "neutral"}
                    />
                  </div>
                  <dl className="definition-list">
                    <div className="definition-list__row">
                      <dt>Request ID</dt>
                      <dd>{state.runtimeHeaders.requestId}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Trace ID</dt>
                      <dd>{state.runtimeHeaders.traceId || "Not returned"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Resolved model</dt>
                      <dd>{state.runtimeHeaders.resolvedModel || state.runtimeHeaders.requestedModel || state.model || "Not set"}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Estimated cost</dt>
                      <dd>{formatUsd(state.runtimeHeaders.costUsd)}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Retries</dt>
                      <dd>{state.runtimeHeaders.retries || "0"}</dd>
                    </div>
                  </dl>
                </div>
              ) : (
                <EmptyState detail="Run a request in Playground." title="No request yet" />
              )}
            </Surface>

            <Surface>
              {lastResponse ? (
                <div className="stack-md">
                  <blockquote className="response-preview">{lastResponse}</blockquote>
                  {state.traceStartedAt ? <p className="body-muted">{formatDateTime(state.traceStartedAt)}</p> : null}
                </div>
              ) : (
                <EmptyState
                  detail="The next response will appear here."
                  title="No output"
                />
              )}
            </Surface>
          </div>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Access" title="Session">
          <Surface>
            <dl className="definition-list definition-list--compact">
              <div className="definition-list__row">
                <dt>Role</dt>
                <dd>{state.session.role}</dd>
              </div>
              <div className="definition-list__row">
                <dt>Tenant</dt>
                <dd>{state.session.tenant || "None"}</dd>
              </div>
              <div className="definition-list__row">
                <dt>Source</dt>
                <dd>{state.session.source || "Unauthenticated"}</dd>
              </div>
              <div className="definition-list__row">
                <dt>Key ID</dt>
                <dd>{state.session.keyID || "None"}</dd>
              </div>
            </dl>
          </Surface>
        </ShellSection>

        <ShellSection eyebrow="Local runtime" title="Issues">
          {state.localProviderIssues.length > 0 ? (
            <div className="stack-sm">
              {state.localProviderIssues.map((issue) => (
                <Surface key={`${issue.provider}-${issue.model}`} tone="danger">
                  <div className="stack-sm">
                    <div className="action-row">
                      <StatusPill label={issue.provider} tone="warning" />
                      <StatusPill label={issue.model} tone="danger" />
                    </div>
                    <p className="body-muted">{issue.message}</p>
                    {issue.command ? (
                      <ToolbarButton onClick={() => void actions.copyCommand(issue.command!)}>{state.copiedCommand === issue.command ? "Copied command" : issue.command}</ToolbarButton>
                    ) : null}
                  </div>
                </Surface>
              ))}
            </div>
          ) : (
            <Surface>
              <EmptyState title="No issues" detail="Local providers look healthy." />
            </Surface>
          )}
        </ShellSection>
      </aside>
    </div>
  );
}
