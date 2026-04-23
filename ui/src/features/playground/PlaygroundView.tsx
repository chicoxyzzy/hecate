import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { formatDateTime, formatUsd } from "../../lib/format";
import { describeRouteReason, findProvider, providerStatusTone } from "../../lib/runtime-utils";
import { RouteWorkbench } from "./RouteWorkbench";
import { TraceWorkbench } from "./TraceWorkbench";
import { DefinitionList, EmptyState, InlineNotice, SelectField, ShellSection, StatusPill, Surface, TextAreaField, TextField, ToolbarButton } from "../shared/ConsolePrimitives";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

function formatToolArgs(args: string): string {
  try {
    return JSON.stringify(JSON.parse(args), null, 2);
  } catch {
    return args;
  }
}

export function PlaygroundView({ state, actions }: Props) {
  const activeProvider = findProvider(state.providers, state.runtimeHeaders?.provider);
  const routeReport = state.traceRoute;
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editingTitle, setEditingTitle] = useState("");

  function handleRenameCommit(id: string) {
    const trimmed = editingTitle.trim();
    if (trimmed && trimmed !== state.chatSessions.find((s) => s.id === id)?.title) {
      void actions.renameChatSession(id, trimmed);
    }
    setEditingId(null);
  }

  return (
    <div className="workspace-grid">
      <div className="workspace-main">
        <ShellSection eyebrow="Chat sessions" title="Session">
          <div className="two-column-grid">
            <Surface>
              <div className="stack-sm">
                <div className="action-row action-row--wide">
                  <StatusPill label={state.activeChatSession ? state.activeChatSession.title : "Unsaved draft"} tone="neutral" />
                  {state.activeChatSession ? <StatusPill label={`${(state.activeChatSession.turns?.length ?? 0)} turns`} tone="healthy" /> : null}
                </div>
                <div className="action-row">
                  <ToolbarButton onClick={() => void actions.createChatSession()} tone="primary">
                    Create session
                  </ToolbarButton>
                  <ToolbarButton onClick={actions.startNewChat}>New draft</ToolbarButton>
                </div>
                {state.chatSessions.length ? (
                  <div className="trace-inline-grid">
                    {state.chatSessions.map((chatSession) => (
                      <div
                        className={state.activeChatSessionID === chatSession.id ? "trace-inline-card trace-inline-card--active session-card" : "trace-inline-card session-card"}
                        key={chatSession.id}
                      >
                        <div className="session-card__header">
                          {editingId === chatSession.id ? (
                            <input
                              autoFocus
                              className="session-card__rename-input"
                              onBlur={() => handleRenameCommit(chatSession.id)}
                              onChange={(e) => setEditingTitle(e.target.value)}
                              onKeyDown={(e) => {
                                if (e.key === "Enter") handleRenameCommit(chatSession.id);
                                if (e.key === "Escape") setEditingId(null);
                              }}
                              type="text"
                              value={editingTitle}
                            />
                          ) : (
                            <button
                              className="session-card__title"
                              onClick={() => { setEditingId(chatSession.id); setEditingTitle(chatSession.title); }}
                              title="Click to rename"
                              type="button"
                            >
                              {chatSession.title}
                            </button>
                          )}
                          <button
                            className="session-card__delete"
                            onClick={() => void actions.deleteChatSession(chatSession.id)}
                            title="Delete session"
                            type="button"
                          >
                            ×
                          </button>
                        </div>
                        <button
                          className="session-card__body"
                          onClick={() => void actions.selectChatSession(chatSession.id)}
                          type="button"
                        >
                          <div className="action-row">
                            {chatSession.last_provider ? <StatusPill label={chatSession.last_provider} tone="neutral" /> : null}
                          </div>
                          <p className="body-muted">
                            {chatSession.turn_count} turns
                            {chatSession.updated_at ? ` · ${formatDateTime(chatSession.updated_at)}` : ""}
                          </p>
                          {chatSession.last_model ? <p className="body-muted">{chatSession.last_model}</p> : null}
                          {chatSession.last_cost_usd ? <p className="body-muted">Last turn {formatUsd(chatSession.last_cost_usd)}</p> : null}
                        </button>
                      </div>
                    ))}
                  </div>
                ) : (
                  <EmptyState title="No saved sessions" detail="Create a session to persist a transcript with provider, model, and spend per turn." />
                )}
              </div>
            </Surface>

            <Surface>
              {(state.activeChatSession?.turns?.length ?? 0) > 0 ? (
                <div className="stack-sm">
                  {(state.activeChatSession?.turns ?? []).map((turn) => (
                    <div className="budget-history-item" key={turn.id}>
                      <div className="budget-history-item__head">
                        <div className="action-row">
                          <StatusPill label={turn.provider} tone={turn.provider_kind === "local" ? "healthy" : "neutral"} />
                          <StatusPill label={turn.model} tone="neutral" />
                          <StatusPill label={formatUsd(turn.cost_usd)} tone="warning" />
                        </div>
                        <span className="body-muted">{formatDateTime(turn.created_at)}</span>
                      </div>
                      <div className="stack-sm">
                        <div className="response-preview">{turn.user_message.content}</div>
                        <div className="response-preview response-preview--large">{turn.assistant_message.content}</div>
                      </div>
                      <div className="budget-history-item__body">
                        <span>Prompt {turn.prompt_tokens}</span>
                        <span>Completion {turn.completion_tokens}</span>
                        <span>Total {turn.total_tokens}</span>
                        {turn.request_id ? <span>{turn.request_id}</span> : null}
                      </div>
                    </div>
                  ))}
                </div>
              ) : (
                <EmptyState title="No transcript yet" detail="The active chat session will build a persisted transcript here as you send requests." />
              )}
            </Surface>
          </div>
        </ShellSection>

        <ShellSection
          eyebrow="Request workspace"
          title="Request"
        >
          <Surface tone="strong">
            <form className="stack-lg" onSubmit={(event) => void actions.submitChat(event)}>
              <div className="form-grid form-grid--triple">
                <SelectField disabled={state.loading} label="Provider route" onChange={actions.setProviderFilter} value={state.providerFilter}>
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

                <SelectField disabled={state.loading} label="Model" onChange={actions.setModel} value={state.model}>
                  <option value="">{state.loading ? "Loading models…" : "Select a model"}</option>
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
                <ToolbarButton disabled={state.loading || state.chatLoading || !state.model || !state.message.trim()} tone="primary" type="submit">
                  {state.loading ? "Loading…" : state.chatLoading ? "Running request..." : "Run through Hecate"}
                </ToolbarButton>
                <StatusPill label={state.activeChatSession ? `Session: ${state.activeChatSession.title}` : "No active session"} tone="neutral" />
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
              {state.streamingContent !== null ? (
                <div className="stack-md">
                  <div className="action-row">
                    <StatusPill label="Streaming…" tone="neutral" />
                  </div>
                  <div className="response-preview response-preview--large response-preview--streaming">
                    {state.streamingContent}
                    <span className="streaming-cursor" />
                  </div>
                </div>
              ) : state.pendingToolCalls.length > 0 ? (
                <div className="stack-md">
                  <div className="action-row">
                    <StatusPill label="Tool calls" tone="neutral" />
                    <StatusPill label={`${state.pendingToolCalls.length} pending`} tone="warning" />
                  </div>
                  <p className="body-muted">The model wants to call these functions. Fill in the results and continue.</p>
                  <div className="stack-sm">
                    {state.pendingToolCalls.map((tc, i) => (
                      <div className="tool-call-card" key={tc.id}>
                        <div className="tool-call-card__header">
                          <span className="tool-call-card__name">{tc.name}</span>
                          <span className="tool-call-card__id">{tc.id}</span>
                        </div>
                        <pre className="tool-call-card__args">{formatToolArgs(tc.arguments)}</pre>
                        <label className="field">
                          <span className="field__label">Result</span>
                          <textarea
                            className="field__input"
                            onChange={(e) => actions.updateToolResult(i, e.target.value)}
                            placeholder="Enter the tool result (string or JSON)"
                            rows={3}
                            value={tc.result}
                          />
                        </label>
                      </div>
                    ))}
                  </div>
                  <div className="action-row">
                    <button
                      className="toolbar-button toolbar-button--primary"
                      disabled={state.chatLoading || state.pendingToolCalls.some((tc) => !tc.result.trim())}
                      onClick={() => void actions.submitToolResults()}
                      type="button"
                    >
                      {state.chatLoading ? "Running…" : "Submit tool results"}
                    </button>
                  </div>
                  {state.chatError ? <p className="body-muted" style={{ color: "var(--danger)" }}>{state.chatError}</p> : null}
                </div>
              ) : state.chatResult ? (
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
                      label={activeProvider ? `${activeProvider.name} ${activeProvider.status}` : routeReport?.final_provider || state.runtimeHeaders.provider || "unknown provider"}
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
                </div>
              ) : (
                <EmptyState title="No metadata" detail="Headers will appear after a response." />
              )}
            </Surface>
          </div>
        </ShellSection>

        <RouteWorkbench route={routeReport} runtimeHeaders={state.runtimeHeaders} spans={state.traceSpans} />
      </div>

      <aside className="workspace-rail">
        <TraceWorkbench error={state.traceError} loading={state.traceLoading} spans={state.traceSpans} traceStartedAt={state.traceStartedAt} />
      </aside>
    </div>
  );
}
