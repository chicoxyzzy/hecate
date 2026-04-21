import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatUsd } from "../../lib/format";
import { EmptyState, InlineNotice, SelectField, ShellSection, StatusPill, Surface, TextAreaField, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function PlaygroundView({ state, actions }: Props) {
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
                <dl className="definition-list">
                  <div className="definition-list__row">
                    <dt>Route reason</dt>
                    <dd>{state.runtimeHeaders.routeReason || "Not returned"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Cache</dt>
                    <dd>{state.runtimeHeaders.cacheType || state.runtimeHeaders.cache || "miss"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Semantic strategy</dt>
                    <dd>{state.runtimeHeaders.semanticStrategy || "None"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Similarity</dt>
                    <dd>{state.runtimeHeaders.semanticSimilarity || "Not returned"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Attempts</dt>
                    <dd>{state.runtimeHeaders.attempts || "1"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Retries</dt>
                    <dd>{state.runtimeHeaders.retries || "0"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Fallback from</dt>
                    <dd>{state.runtimeHeaders.fallbackFrom || "None"}</dd>
                  </div>
                  <div className="definition-list__row">
                    <dt>Estimated cost</dt>
                    <dd>{formatUsd(state.runtimeHeaders.costUsd)}</dd>
                  </div>
                </dl>
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
                      {span.events?.length ? (
                        <ul className="trace-event-list">
                          {span.events.map((event) => (
                            <li key={`${span.span_id}-${event.timestamp}-${event.name}`}>
                              <strong>{event.name}</strong>
                              <span>{event.timestamp}</span>
                            </li>
                          ))}
                        </ul>
                      ) : (
                        <p className="body-muted">No events.</p>
                      )}
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
