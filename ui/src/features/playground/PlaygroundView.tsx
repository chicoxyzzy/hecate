import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatUsd } from "../../lib/format";
import { buildTraceTimeline, describeRouteReason, findProvider, providerStatusTone } from "../../lib/runtime-utils";
import { DefinitionList, EmptyState, InlineNotice, SelectField, ShellSection, StatusPill, Surface, TextAreaField, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function PlaygroundView({ state, actions }: Props) {
  const activeProvider = findProvider(state.providers, state.runtimeHeaders?.provider);
  const timeline = buildTraceTimeline(state.traceSpans, state.traceStartedAt);

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection
          eyebrow="Request workspace"
          title="Request"
        >
          <Surface tone="strong">
            <form className="stack-lg" onSubmit={(event) => void actions.submitChat(event)}>
              <div className="form-grid form-grid--triple">
                <SelectField label="Provider route" onChange={actions.setProviderFilter} value={state.providerFilter}>
                  <option value="auto">Auto-select</option>
                  {state.localProviders.length > 0 ? (
                    <optgroup label="Local">
                      {state.localProviders.map((provider) => (
                        <option key={provider.name} value={provider.name}>
                          {provider.name}
                        </option>
                      ))}
                    </optgroup>
                  ) : null}
                  {state.cloudProviders.length > 0 ? (
                    <optgroup label="Cloud">
                      {state.cloudProviders.map((provider) => (
                        <option key={provider.name} value={provider.name}>
                          {provider.name}
                        </option>
                      ))}
                    </optgroup>
                  ) : null}
                </SelectField>

                <SelectField label="Model" onChange={actions.setModel} value={state.model}>
                  <option value="">Select a model</option>
                  {state.providerFilter === "auto" ? (
                    <>
                      {state.localModels.length > 0 ? (
                        <optgroup label="Local models">
                          {state.localModels.map((entry) => (
                            <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                              {entry.id}
                            </option>
                          ))}
                        </optgroup>
                      ) : null}
                      {state.cloudModels.length > 0 ? (
                        <optgroup label="Cloud models">
                          {state.cloudModels.map((entry) => (
                            <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                              {entry.id}
                            </option>
                          ))}
                        </optgroup>
                      ) : null}
                    </>
                  ) : (
                    state.providerScopedModels.map((entry) => (
                      <option key={`${entry.metadata?.provider}-${entry.id}`} value={entry.id}>
                        {entry.id}
                      </option>
                    ))
                  )}
                </SelectField>

                <TextField
                  label="Tenant / user scope"
                  onChange={actions.setTenant}
                  placeholder="team-a"
                  value={state.tenant}
                />
              </div>

              <TextAreaField
                label="Prompt"
                onChange={actions.setMessage}
                placeholder="Describe the request you want to run through the gateway."
                rows={7}
                value={state.message}
              />

              {state.chatError ? <InlineNotice message={state.chatError} tone="error" /> : null}

              <div className="action-row">
                <ToolbarButton disabled={state.chatLoading || !state.model || !state.message.trim()} tone="primary" type="submit">
                  {state.chatLoading ? "Running request..." : "Run through Hecate"}
                </ToolbarButton>
                <StatusPill label={state.providerFilter === "auto" ? "Route mode: auto" : `Pinned: ${state.providerFilter}`} tone="neutral" />
                <StatusPill
                  label={state.session.kind === "tenant" ? `Tenant locked: ${state.session.tenant}` : "Tenant can be overridden"}
                  tone={state.session.kind === "tenant" ? "warning" : "neutral"}
                />
              </div>
            </form>
          </Surface>
        </ShellSection>

        <ShellSection
          eyebrow="Response"
          title="Output"
        >
          <div className="two-column-grid">
            <Surface>
              {state.chatResult ? (
                <div className="stack-md">
                  <div className="action-row">
                    <StatusPill label={state.runtimeHeaders?.provider || "Unknown provider"} tone="neutral" />
                    <StatusPill label={state.runtimeHeaders?.resolvedModel || state.chatResult.model} tone="neutral" />
                  </div>
                  <div className="response-preview response-preview--large">{state.chatResult.choices[0]?.message?.content || "No assistant content returned."}</div>
                  <dl className="definition-list definition-list--compact">
                    <div className="definition-list__row">
                      <dt>Prompt tokens</dt>
                      <dd>{state.chatResult.usage?.prompt_tokens ?? 0}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Completion tokens</dt>
                      <dd>{state.chatResult.usage?.completion_tokens ?? 0}</dd>
                    </div>
                    <div className="definition-list__row">
                      <dt>Total tokens</dt>
                      <dd>{state.chatResult.usage?.total_tokens ?? 0}</dd>
                    </div>
                  </dl>
                </div>
              ) : (
                <EmptyState title="No response" detail="Run a request." />
              )}
            </Surface>

            <Surface>
              {state.runtimeHeaders ? (
                <div className="stack-lg">
                  <div className="action-row action-row--wide">
                    <StatusPill label={describeRouteReason(state.runtimeHeaders.routeReason)} tone="neutral" />
                    <StatusPill label={`cache ${state.runtimeHeaders.cacheType || state.runtimeHeaders.cache || "miss"}`} tone={state.runtimeHeaders.cache === "true" ? "healthy" : "neutral"} />
                    <StatusPill
                      label={activeProvider ? `${activeProvider.name} ${activeProvider.status}` : state.runtimeHeaders.provider || "unknown provider"}
                      tone={providerStatusTone(activeProvider ?? undefined)}
                    />
                    {state.runtimeHeaders.fallbackFrom ? <StatusPill label={`fallback from ${state.runtimeHeaders.fallbackFrom}`} tone="warning" /> : null}
                  </div>

                  <DefinitionList
                    items={[
                      { label: "Provider", value: state.runtimeHeaders.provider || "unknown" },
                      { label: "Provider kind", value: state.runtimeHeaders.providerKind || "unknown" },
                      { label: "Requested model", value: state.runtimeHeaders.requestedModel || state.model || "n/a" },
                      { label: "Resolved model", value: state.runtimeHeaders.resolvedModel || "n/a" },
                      { label: "Attempts", value: state.runtimeHeaders.attempts || "1" },
                      { label: "Retries", value: state.runtimeHeaders.retries || "0" },
                      { label: "Estimated cost", value: formatUsd(state.runtimeHeaders.costUsd) },
                    ]}
                  />

                  <div className="trace-inline-grid">
                    <div className="trace-inline-card">
                      <p className="label-muted">Semantic cache</p>
                      <p className="trace-inline-card__title">{state.runtimeHeaders.semanticStrategy || "No semantic match returned"}</p>
                      <p className="body-muted">
                        {state.runtimeHeaders.semanticStrategy
                          ? `Similarity ${state.runtimeHeaders.semanticSimilarity || "n/a"} via ${state.runtimeHeaders.semanticIndex || "unknown index"}.`
                          : "This request returned no semantic retrieval metadata."}
                      </p>
                    </div>
                    <div className="trace-inline-card">
                      <p className="label-muted">Request identity</p>
                      <p className="trace-inline-card__title">{state.runtimeHeaders.requestId}</p>
                      <p className="body-muted">Trace {state.runtimeHeaders.traceId || "not returned"} · Span {state.runtimeHeaders.spanId || "not returned"}</p>
                    </div>
                  </div>
                </div>
              ) : (
                <EmptyState title="No metadata" detail="Headers will appear after a response." />
              )}
            </Surface>
          </div>
        </ShellSection>
      </div>

      <aside className="workspace-rail">
        <ShellSection eyebrow="Trace" title="Spans">
          <Surface>
            {state.traceLoading ? (
              <p className="body-muted">Loading trace...</p>
            ) : state.traceError ? (
              <InlineNotice message={state.traceError} tone="error" />
            ) : state.traceSpans.length > 0 ? (
              <div className="stack-sm">
                {timeline.length > 0 ? (
                  <div className="trace-timeline">
                    {timeline.map((event, index) => (
                      <article className="trace-timeline__item" key={`${event.timestamp}-${event.name}-${index}`}>
                        <div className="trace-timeline__meta">
                          <StatusPill label={event.phase} tone={event.phase === "provider" ? "warning" : event.phase === "response" ? "healthy" : "neutral"} />
                          <span>{event.offsetLabel}</span>
                        </div>
                        <strong>{event.name}</strong>
                        <p className="body-muted">
                          {event.spanName} · {new Date(event.timestamp).toLocaleTimeString()}
                        </p>
                        {event.attributes && Object.keys(event.attributes).length > 0 ? (
                          <dl className="definition-list definition-list--compact">
                            {Object.entries(event.attributes)
                              .slice(0, 4)
                              .map(([key, value]) => (
                                <div className="definition-list__row" key={key}>
                                  <dt>{key}</dt>
                                  <dd>{String(value)}</dd>
                                </div>
                              ))}
                          </dl>
                        ) : null}
                      </article>
                    ))}
                  </div>
                ) : null}
                {state.traceSpans.map((span) => (
                  <details className="trace-item" key={span.span_id} open={span.span_id === state.traceSpans[0]?.span_id}>
                    <summary className="trace-item__summary">
                      <span>{span.name}</span>
                      <span>{span.status_code || "unset"}</span>
                    </summary>
                    <div className="trace-item__body">
                      <p className="body-muted">
                        {span.start_time || "Unknown start"} {span.end_time ? `-> ${span.end_time}` : ""}
                      </p>
                      <p className="body-muted">{span.events?.length ?? 0} events</p>
                    </div>
                  </details>
                ))}
              </div>
            ) : (
              <EmptyState title="No trace" detail="Run a request first." />
            )}
          </Surface>
        </ShellSection>
      </aside>
    </div>
  );
}
