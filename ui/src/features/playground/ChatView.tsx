import { useEffect, useRef, useState } from "react";
import type { SyntheticEvent } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { parseInlineNodes, parseMarkdownBlocks } from "../../lib/markdown";
import type { ModelRecord } from "../../types/runtime";
import { CodeBlock, Icon, Icons, InlineError, ProviderPicker } from "../shared/ui";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function ChatView({ state, actions }: Props) {
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [syspromptOpen, setSyspromptOpen] = useState(false);
  const [renamingId, setRenamingId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [hoveredSessionId, setHoveredSessionId] = useState<string | null>(null);
  const [copiedMsgId, setCopiedMsgId] = useState<string | null>(null);
  const [atBottom, setAtBottom] = useState(true);
  const isMac = typeof navigator !== "undefined" && /mac/i.test(navigator.platform);
  const modKey = isMac ? "⌘" : "Ctrl";
  const [modEnterMode, setModEnterMode] = useState(
    () => localStorage.getItem("hecate.shiftEnterMode") !== "0"
  );
  const formRef = useRef<HTMLFormElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);
  const sidebarScrollRef = useRef<HTMLDivElement>(null);
  const userScrolledRef = useRef(false);

  const sessions = state.chatSessions ?? [];
  const turns = state.activeChatSession?.turns ?? [];
  const streaming = state.chatLoading;

  useEffect(() => {
    if (!userScrolledRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: "instant" });
    }
  }, [state.streamingContent, turns.length]);

  useEffect(() => {
    userScrolledRef.current = false;
    setAtBottom(true);
    bottomRef.current?.scrollIntoView({ behavior: "instant" });
    // Focus the message textarea on every session change — including
    // mount, since if ChatView is mounted the user is looking at the
    // chat tab and is one keystroke away from typing. Switching to a
    // session is almost always followed by a reply; auto-focus saves
    // a click. The New-session button onClick also focuses directly,
    // covering the case where activeChatSessionID is already "" and
    // this effect would not re-fire.
    textareaRef.current?.focus();
  }, [state.activeChatSessionID]);

  function handleScroll() {
    const el = scrollRef.current;
    if (!el) return;
    const threshold = 80;
    const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold;
    setAtBottom(isAtBottom);
    userScrolledRef.current = !isAtBottom;
  }

  function handleSidebarScroll() {
    const el = sidebarScrollRef.current;
    if (!el || !state.chatSessionsHasMore || state.chatSessionsLoadingMore) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    if (nearBottom) {
      void actions.loadMoreChatSessions();
    }
  }

  function scrollToBottom() {
    userScrolledRef.current = false;
    setAtBottom(true);
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }

  function copyMsg(id: string, text: string) {
    navigator.clipboard?.writeText(text).catch(() => {});
    setCopiedMsgId(id);
    setTimeout(() => setCopiedMsgId(null), 2000);
  }

  function toggleModEnterMode() {
    setModEnterMode(v => {
      const next = !v;
      localStorage.setItem("hecate.shiftEnterMode", next ? "1" : "0");
      return next;
    });
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key !== "Enter") return;
    const modPressed = isMac ? e.metaKey : e.ctrlKey;
    if (modEnterMode) {
      // ⌘/Ctrl+Enter sends; plain Enter is a newline (default behaviour)
      if (modPressed) { e.preventDefault(); formRef.current?.requestSubmit(); }
    } else {
      // Enter sends; Shift+Enter or ⌘/Ctrl+Enter inserts a newline
      if (e.shiftKey || modPressed) return;
      e.preventDefault();
      formRef.current?.requestSubmit();
    }
  }

  function handleSubmit(e: SyntheticEvent<HTMLFormElement>) {
    void actions.submitChat(e);
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  }

  return (
    <div style={{ display: "flex", height: "100%", overflow: "hidden" }}>
      {/* Conversation sidebar */}
      {sidebarOpen && (
        <div style={{ width: 220, borderRight: "1px solid var(--border)", display: "flex", flexDirection: "column", flexShrink: 0, background: "var(--bg1)" }}>
          <div style={{ padding: 8, borderBottom: "1px solid var(--border)", display: "flex", gap: 6 }}>
            <button
              className="btn btn-primary btn-sm"
              style={{ flex: 1, justifyContent: "center" }}
              onClick={() => {
                actions.createChatSession();
                textareaRef.current?.focus();
              }}
            >
              <Icon d={Icons.plus} size={13} /> New session
            </button>
            <button className="btn btn-ghost btn-sm" onClick={() => setSidebarOpen(false)} title="Close">
              <Icon d={Icons.chevL} size={13} />
            </button>
          </div>
          <div ref={sidebarScrollRef} onScroll={handleSidebarScroll} style={{ flex: 1, overflowY: "auto", padding: "6px 0" }}>
            {sessions.length === 0 && (
              <div style={{ padding: "16px 12px", fontSize: 12, color: "var(--t3)", textAlign: "center" }}>No sessions yet</div>
            )}
            {sessions.map(s => (
              <div key={s.id}
                onClick={() => renamingId !== s.id && void actions.selectChatSession(s.id)}
                onMouseEnter={() => setHoveredSessionId(s.id)}
                onMouseLeave={() => setHoveredSessionId(null)}
                style={{
                  padding: "8px 12px", cursor: "pointer",
                  background: state.activeChatSessionID === s.id ? "var(--teal-bg)" : "transparent",
                  borderLeft: state.activeChatSessionID === s.id ? "2px solid var(--teal)" : "2px solid transparent",
                  transition: "background 0.1s",
                }}>
                <div style={{ display: "flex", alignItems: "center", gap: 2, height: 18 }}>
                  {renamingId === s.id ? (
                    <input
                      autoFocus
                      value={renameValue}
                      onChange={e => setRenameValue(e.target.value)}
                      onClick={e => e.stopPropagation()}
                      onKeyDown={e => {
                        if (e.key === "Enter") { void actions.renameChatSession(s.id, renameValue); setRenamingId(null); }
                        if (e.key === "Escape") setRenamingId(null);
                      }}
                      onBlur={() => { void actions.renameChatSession(s.id, renameValue); setRenamingId(null); }}
                      style={{ flex: 1, minWidth: 0, height: 18, boxSizing: "border-box", fontSize: 12, background: "var(--bg3)", border: "1px solid var(--teal)", borderRadius: "var(--radius-sm)", color: "var(--t0)", padding: "0 4px", outline: "none", fontFamily: "var(--font-sans)", lineHeight: "16px" }}
                    />
                  ) : (
                    <>
                      <div style={{ flex: 1, minWidth: 0, fontSize: 12, lineHeight: "18px", color: state.activeChatSessionID === s.id ? "var(--t0)" : "var(--t1)", fontWeight: state.activeChatSessionID === s.id ? 500 : 400, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        {s.title || "Untitled"}
                      </div>
                      <div style={{ display: "flex", gap: 1, opacity: hoveredSessionId === s.id ? 1 : 0, transition: "opacity 0.15s", flexShrink: 0 }}>
                        <button
                          className="btn btn-ghost btn-sm"
                          onClick={e => { e.stopPropagation(); setRenamingId(s.id); setRenameValue(s.title || ""); }}
                          style={{ padding: "1px 3px" }}
                          title="Rename"
                        >
                          <Icon d={Icons.edit} size={10} />
                        </button>
                        <button
                          className="btn btn-ghost btn-sm"
                          onClick={e => { e.stopPropagation(); void actions.deleteChatSession(s.id); }}
                          style={{ padding: "1px 3px", color: "var(--red)" }}
                          title="Delete"
                        >
                          <Icon d={Icons.trash} size={10} />
                        </button>
                      </div>
                    </>
                  )}
                </div>
                <div style={{ fontSize: 10, color: "var(--t3)", marginTop: 1, fontFamily: "var(--font-mono)" }}>
                  {s.turn_count} turns{s.last_provider ? ` · ${s.last_provider}` : ""}
                </div>
              </div>
            ))}
            {state.chatSessionsLoadingMore && (
              <div style={{ padding: "8px 12px", fontSize: 11, color: "var(--t3)", textAlign: "center" }}>Loading…</div>
            )}
          </div>
        </div>
      )}

      {/* Chat main */}
      <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden", minWidth: 0, position: "relative" }}>
        {/* Topbar */}
        <div style={{ height: "var(--topbar-h)", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", padding: "0 12px", gap: 8, flexShrink: 0, background: "var(--bg1)" }}>
          {!sidebarOpen && (
            <button className="btn btn-ghost btn-sm" onClick={() => setSidebarOpen(true)} title="Open history">
              <Icon d={Icons.chevR} size={13} />
            </button>
          )}
          <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {state.activeChatSession?.title || (sessions.length === 0 ? "New conversation" : "Select a session")}
          </span>
          <ProviderPicker
            value={state.providerFilter}
            onChange={v => actions.setProviderFilter(v as typeof state.providerFilter)}
            options={(() => {
              const allowed = state.session.allowedProviders;
              return state.providers
                .filter(p => {
                  if (!p.healthy) return false;
                  if (allowed.length > 0 && !allowed.includes(p.name)) return false;
                  return true;
                })
                .filter(p => p.name)
                .map(p => {
                  const cfg = state.adminConfig?.providers.find(c => c.name === p.name);
                  // Three "disabled" reasons we surface to the operator
                  // via a key icon + tooltip rather than hiding the row:
                  //   * cloud provider with no credentials → "needs key"
                  //   * any provider explicitly disabled in admin config
                  // For tenant-key sessions (no adminConfig), `cfg` is
                  // undefined, so neither flag fires — the picker
                  // behaves as before for non-admin users.
                  const cloudUnconfigured = !!cfg && cfg.kind === "cloud" && !cfg.credential_configured;
                  const adminDisabled = !!cfg && !cfg.enabled;
                  let disabledReason: string | undefined;
                  if (cloudUnconfigured) {
                    disabledReason = `Configure ${cfg!.id.toUpperCase()} credentials in Admin → Providers`;
                  } else if (adminDisabled) {
                    disabledReason = "Disabled in Admin → Providers";
                  }
                  return {
                    id: p.name,
                    name: state.providerPresets.find(pr => pr.id === p.name)?.name || p.name,
                    healthy: p.healthy,
                    // `kind` drives whether we show a key indicator at
                    // all — local providers don't have an API-key
                    // concept, so they get no key icon regardless of
                    // their config state.
                    kind: cfg?.kind ?? state.providerPresets.find(pr => pr.id === p.name)?.kind,
                    configured: cfg ? cfg.credential_configured : undefined,
                    disabledReason,
                  };
                });
            })()}
            includeAuto
          />
          <ModelPicker
            value={state.model}
            onChange={actions.setModel}
            models={state.providerScopedModels}
            presets={state.providerPresets}
            // Show the provider suffix only when "All providers" is
            // selected — when a specific provider is filtered, the
            // suffix is redundant on every row.
            showProvider={state.providerFilter === "auto"}
            // Provider ids whose models should render as disabled rows
            // (with a key indicator). Same rules as the provider
            // picker: cloud + no credentials, or admin-disabled.
            disabledProviders={(() => {
              const out = new Map<string, string>();
              for (const cfg of state.adminConfig?.providers ?? []) {
                if (cfg.kind === "cloud" && !cfg.credential_configured) {
                  out.set(cfg.id, `Configure ${cfg.id.toUpperCase()} credentials in Admin → Providers`);
                } else if (!cfg.enabled) {
                  out.set(cfg.id, "Disabled in Admin → Providers");
                }
              }
              return out;
            })()}
          />
          <button className="btn btn-ghost btn-sm" onClick={() => setSyspromptOpen(o => !o)}
            style={{ color: syspromptOpen ? "var(--teal)" : "var(--t2)" }} title="System prompt">
            <Icon d={Icons.edit} size={13} />
            <span style={{ fontSize: 11 }}>system</span>
          </button>
        </div>

        {/* System prompt editor */}
        {syspromptOpen && (
          <div style={{ borderBottom: "1px solid var(--border)", padding: "10px 14px", background: "var(--bg2)" }}>
            <div style={{ display: "flex", alignItems: "center", marginBottom: 5, gap: 8 }}>
              <span style={{ fontSize: 11, color: "var(--t2)", fontFamily: "var(--font-mono)" }}>SYSTEM PROMPT</span>
              {turns.length > 0 && <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>locked — start a new session to change</span>}
            </div>
            <textarea
              value={state.systemPrompt}
              onChange={e => actions.setSystemPrompt(e.target.value)}
              disabled={turns.length > 0}
              style={{ width: "100%", background: "var(--bg3)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", color: turns.length > 0 ? "var(--t2)" : "var(--t0)", fontFamily: "var(--font-mono)", fontSize: 12, padding: "8px 10px", resize: "vertical", minHeight: 72, outline: "none", lineHeight: 1.5, opacity: turns.length > 0 ? 0.6 : 1 }}
            />
          </div>
        )}

        {/* Messages */}
        <div style={{ flex: 1, overflow: "hidden", position: "relative" }}>
        <div ref={scrollRef} onScroll={handleScroll} style={{ height: "100%", overflowY: "auto", padding: "16px 0" }}>
          {turns.map(turn => (
            <div key={turn.id}>
              <MessageRow
                id={`${turn.id}-user`}
                role="user"
                content={typeof turn.user_message.content === "string" ? turn.user_message.content : JSON.stringify(turn.user_message.content)}
                time={turn.created_at ? new Date(turn.created_at).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" }) : ""}
                onCopy={copyMsg}
                copied={copiedMsgId === `${turn.id}-user`}
              />
              {turn.assistant_message.content !== null && (
                <MessageRow
                  id={`${turn.id}-asst`}
                  role="assistant"
                  model={turn.model}
                  content={typeof turn.assistant_message.content === "string" ? turn.assistant_message.content : JSON.stringify(turn.assistant_message.content)}
                  time={turn.created_at ? new Date(turn.created_at).toLocaleTimeString("en-US", { hour: "2-digit", minute: "2-digit" }) : ""}
                  promptTokens={turn.prompt_tokens}
                  completionTokens={turn.completion_tokens}
                  costUsd={turn.cost_usd}
                  onCopy={copyMsg}
                  copied={copiedMsgId === `${turn.id}-asst`}
                />
              )}
            </div>
          ))}

          {/* Streaming */}
          {streaming && state.streamingContent !== null && (
            <div style={{ padding: "4px 16px 16px", maxWidth: 820, margin: "0 auto", width: "100%" }}>
              <div style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
                <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--teal-bg)", border: "1px solid var(--teal-border)", display: "flex", alignItems: "center", justifyContent: "center", flexShrink: 0, marginTop: 2 }}>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--teal)", fontWeight: 600 }}>{(state.model || "H")[0].toUpperCase()}</span>
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--teal)" }}>{state.model || "hecate"}</span>
                    <span className="badge badge-teal" style={{ animation: "pulse 1s ease-in-out infinite", fontSize: 10 }}>streaming</span>
                  </div>
                  <p style={{ fontSize: 13, color: "var(--t0)", lineHeight: 1.7, whiteSpace: "pre-wrap" }}>
                    {state.streamingContent}<span className="cursor-blink">▋</span>
                  </p>
                </div>
              </div>
            </div>
          )}

          {/* Pending tool calls */}
          {state.pendingToolCalls.length > 0 && (
            <div style={{ padding: "4px 16px 16px", maxWidth: 820, margin: "0 auto", width: "100%" }}>
              <div style={{ fontSize: 11, color: "var(--t2)", marginBottom: 8 }}>
                {state.pendingToolCalls.length} tool call{state.pendingToolCalls.length > 1 ? "s" : ""} pending
              </div>
              {state.pendingToolCalls.map((tc, i) => (
                <div key={tc.id} style={{ border: "1px solid var(--border)", borderRadius: "var(--radius)", padding: "10px 12px", background: "var(--bg2)", marginBottom: 8 }}>
                  <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, fontWeight: 600, color: "var(--teal)" }}>{tc.name}</span>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>{tc.id}</span>
                  </div>
                  <CodeBlock code={(() => { try { return JSON.stringify(JSON.parse(tc.arguments), null, 2); } catch { return tc.arguments; } })()} lang="json" />
                  <div style={{ marginTop: 8 }}>
                    <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4 }}>Result</label>
                    <textarea
                      className="input"
                      onChange={e => actions.updateToolResult(i, e.target.value)}
                      placeholder="Enter the tool result"
                      rows={3}
                      style={{ resize: "vertical" }}
                      value={tc.result}
                    />
                  </div>
                </div>
              ))}
              <button className="btn btn-primary btn-sm"
                disabled={state.chatLoading || state.pendingToolCalls.some(tc => !tc.result.trim())}
                onClick={() => void actions.submitToolResults()}>
                {state.chatLoading ? "Running…" : "Submit results"}
              </button>
            </div>
          )}

          {turns.length === 0 && !streaming && state.pendingToolCalls.length === 0 && (
            <div style={{ padding: "48px 16px", maxWidth: 820, margin: "0 auto", textAlign: "center" }}>
              <div style={{ fontSize: 13, color: "var(--t3)" }}>Send a message to start a conversation.</div>
            </div>
          )}
          <div ref={bottomRef} />
        </div>

        {!atBottom && (
          <button onClick={scrollToBottom} style={{
            position: "absolute", bottom: 16, left: "50%", transform: "translateX(-50%)",
            height: 28, padding: "0 12px", borderRadius: 14,
            background: "var(--bg3)", border: "1px solid var(--border)",
            cursor: "pointer", display: "flex", alignItems: "center", gap: 5,
            color: "var(--t1)", fontSize: 12, boxShadow: "var(--shadow-popover)",
            zIndex: 10, whiteSpace: "nowrap",
          }}>
            <Icon d={Icons.chevD} size={12} /> Scroll to bottom
          </button>
        )}
        </div>

        {/* Input bar */}
        <form ref={formRef} onSubmit={handleSubmit} style={{ borderTop: "1px solid var(--border)", padding: "10px 12px", background: "var(--bg1)", flexShrink: 0 }}>
          {state.chatError && (
            <div style={{ marginBottom: 8 }}>
              <InlineError message={`${state.runtimeHeaders?.provider ? `[${state.runtimeHeaders.provider}] ` : ""}${state.chatError}`} />
            </div>
          )}
          <div style={{ maxWidth: 820, margin: "0 auto", position: "relative" }}>
            <textarea
              ref={textareaRef}
              value={state.message}
              onChange={e => actions.setMessage(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={modEnterMode ? `Message… (${modKey}+Enter to send)` : "Message… (Shift+Enter for newline)"}
              rows={1}
              style={{
                width: "100%", background: "var(--bg3)", border: "1px solid var(--border)",
                borderRadius: "var(--radius)", color: "var(--t0)", fontFamily: "var(--font-sans)",
                fontSize: 13, padding: "10px 44px 10px 12px", outline: "none", resize: "none",
                lineHeight: 1.5, transition: "border-color 0.1s", minHeight: 42, maxHeight: 160, overflowY: "auto",
              }}
              onInput={e => {
                const el = e.target as HTMLTextAreaElement;
                el.style.height = "auto";
                el.style.height = Math.min(el.scrollHeight, 160) + "px";
              }}
              onFocus={e => (e.target.style.borderColor = "var(--teal)")}
              onBlur={e => (e.target.style.borderColor = "var(--border)")}
            />
            <button type="submit"
              disabled={!state.message.trim() || streaming}
              style={{
                position: "absolute", right: 8, top: "50%", transform: "translateY(-50%)",
                width: 28, height: 28, borderRadius: "var(--radius-sm)",
                background: state.message.trim() && !streaming ? "var(--teal)" : "var(--bg4)",
                border: "none", cursor: state.message.trim() && !streaming ? "pointer" : "default",
                display: "flex", alignItems: "center", justifyContent: "center",
                transition: "background 0.1s",
                color: state.message.trim() && !streaming ? "var(--bg0)" : "var(--t3)",
              }}>
              <Icon d={Icons.send} size={14} />
            </button>
          </div>
          <div style={{ maxWidth: 820, margin: "3px auto 0", display: "flex", justifyContent: "flex-end" }}>
            <button type="button" onClick={toggleModEnterMode} style={{
              fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)",
              background: "none", border: "none", cursor: "pointer", padding: 0,
            }}>
              {modEnterMode ? `${modKey}+↵ to send` : "↵ to send"}
            </button>
          </div>
        </form>
      </div>

      <style>{`
        .cursor-blink { color: var(--teal); }
        @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.5} }
      `}</style>
    </div>
  );
}

function ModelPicker({ value, onChange, models, presets, disabledProviders, showProvider }: {
  value: string;
  onChange: (v: string) => void;
  models: ModelRecord[];
  presets: import("../../types/runtime").ProviderPresetRecord[];
  // Provider ids whose models render as disabled (greyed, not
  // clickable, with a key indicator). Map value is the tooltip
  // explaining why ("Configure X credentials in Admin → Providers"
  // or "Disabled in Admin → Providers").
  disabledProviders?: Map<string, string>;
  // Render the per-row "(provider name)" suffix. False when the
  // outer provider filter is already pinned to a single provider —
  // every row would carry the same suffix, which is just noise.
  showProvider?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) { setOpen(false); setFilter(""); }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  useEffect(() => {
    if (open) setTimeout(() => inputRef.current?.focus(), 0);
    else setFilter("");
  }, [open]);

  const providerName = (id: string) => presets.find(p => p.id === id)?.name || id;
  const matchedFilter = filter ? models.filter(m => m.id.toLowerCase().includes(filter.toLowerCase())) : models;
  // Sort usable models above disabled ones — within each bucket the
  // source order is preserved (provider-grouped, alphabetical-ish).
  // Stable partition via two passes is simpler than a comparator and
  // avoids accidentally reordering rows whose disabled state is the
  // same. Without this, the operator scrolls past 8 disabled claude/
  // gpt rows before finding a llama they can actually use.
  const filtered = (() => {
    const usable: ModelRecord[] = [];
    const disabled: ModelRecord[] = [];
    for (const m of matchedFilter) {
      const provider = m.metadata?.provider;
      if (provider && disabledProviders?.has(provider)) {
        disabled.push(m);
      } else {
        usable.push(m);
      }
    }
    return [...usable, ...disabled];
  })();
  const label = value || (models[0]?.id ?? "model");

  return (
    <div className="dropdown-wrap" ref={ref}>
      {/* Fixed width matches the provider picker trigger so the two
          dropdowns next to each other read as a stable pair, not a
          ragged set. Long model ids truncate via text-overflow:ellipsis
          on the inner span (flex truncation needs an inner box with
          min-width:0 — the .btn flex container alone won't ellipsize
          a bare string child). */}
      <button className="btn btn-ghost btn-sm" onClick={() => setOpen(o => !o)}
        style={{ fontFamily: "var(--font-mono)", fontSize: 11, gap: 5, color: "var(--t1)", width: 220 }}>
        <Icon d={Icons.model} size={13} />
        <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textAlign: "left" }} title={label}>
          {label}
        </span>
        <Icon d={Icons.chevD} size={11} />
      </button>
      {open && (
        <div className="dropdown-menu" style={{ minWidth: 300, right: 0, left: "auto" }}>
          <div style={{ padding: "6px 8px", borderBottom: "1px solid var(--border)" }}>
            <input
              ref={inputRef}
              className="input"
              style={{ fontSize: 12, padding: "4px 8px", fontFamily: "var(--font-mono)" }}
              placeholder="Filter models…"
              value={filter}
              onChange={e => setFilter(e.target.value)}
              onClick={e => e.stopPropagation()}
            />
          </div>
          <div style={{ maxHeight: 300, overflowY: "auto", overflowX: "hidden" }}>
            {filtered.length === 0 && (
              <div style={{ padding: "10px 12px", fontSize: 12, color: "var(--t3)" }}>No models match</div>
            )}
            {filtered.map(m => {
              const provider = m.metadata?.provider;
              const reason = provider ? disabledProviders?.get(provider) : undefined;
              const disabled = !!reason;
              return (
                <div
                  key={m.id}
                  className={`dropdown-item ${m.id === value ? "selected" : ""}`}
                  title={reason}
                  style={disabled ? { cursor: "not-allowed" } : undefined}
                  onClick={() => {
                    if (disabled) return;
                    onChange(m.id);
                    setOpen(false);
                  }}>
                  {/* Only the model id dims when disabled. Provider
                      name keeps its t3 color so the right column
                      reads consistently across enabled + disabled
                      rows (CSS opacity inheritance would otherwise
                      drag the muted text into something unreadable
                      against the dark background). */}
                  <span
                    style={{
                      flex: 1,
                      fontFamily: "var(--font-mono)",
                      fontSize: 12,
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                      opacity: disabled ? 0.5 : 1,
                    }}>
                    {m.id}
                  </span>
                  {/* Model row order: id → provider-name → key (last).
                      Hide the provider suffix when the outer filter
                      pinned a single provider (every row would carry
                      the same suffix). */}
                  {showProvider && provider && (
                    <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", flexShrink: 0, marginLeft: 6 }}>
                      {providerName(provider)}
                    </span>
                  )}
                  {/* Reserve a fixed slot whether or not a key icon
                      renders — keeps the right edge aligned across
                      rows so the model-id and provider-name columns
                      stay coherent. */}
                  <span style={{ display: "inline-flex", flexShrink: 0, marginLeft: 6, width: 11, justifyContent: "center" }}>
                    {disabled && (
                      <span aria-label="credentials missing" style={{ color: "var(--red)", display: "inline-flex" }}>
                        <Icon d={Icons.keys} size={11} />
                      </span>
                    )}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

function MessageRow({ id, role, model, content, time, promptTokens, completionTokens, costUsd, onCopy, copied }: {
  id: string; role: "user" | "assistant"; model?: string; content: string;
  time: string; promptTokens?: number; completionTokens?: number; costUsd?: string;
  onCopy: (id: string, text: string) => void; copied: boolean;
}) {
  const [hovered, setHovered] = useState(false);
  const isAssistant = role === "assistant";
  const hasTokenData = isAssistant && (promptTokens ?? 0) > 0;

  return (
    <div onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
      style={{ padding: "4px 16px 12px", maxWidth: 820, margin: "0 auto", width: "100%" }}>
      <div style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
        <div style={{
          width: 28, height: 28, borderRadius: "var(--radius-sm)", flexShrink: 0, marginTop: 2,
          background: isAssistant ? "var(--teal-bg)" : "var(--bg3)",
          border: `1px solid ${isAssistant ? "var(--teal-border)" : "var(--border)"}`,
          display: "flex", alignItems: "center", justifyContent: "center",
        }}>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: isAssistant ? "var(--teal)" : "var(--t1)", fontWeight: 600 }}>
            {isAssistant ? (model || "H")[0].toUpperCase() : "U"}
          </span>
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 5 }}>
            {isAssistant
              ? <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--teal)" }}>{model || "hecate"}</span>
              : <span style={{ fontSize: 11, color: "var(--t2)", fontWeight: 500 }}>You</span>
            }
            <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>{time}</span>
            {hasTokenData && (
              <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
                {promptTokens}↑ {completionTokens}↓
                {costUsd && costUsd !== "0" ? ` · $${Number(costUsd).toFixed(5)}` : ""}
              </span>
            )}
            <div style={{ marginLeft: "auto", display: "flex", gap: 4, opacity: hovered ? 1 : 0, transition: "opacity 0.15s" }}>
              <button className="btn btn-ghost btn-sm" style={{ padding: "2px 6px", gap: 4 }}
                onClick={() => onCopy(id, content)}>
                <Icon d={copied ? Icons.check : Icons.copy} size={12} />
              </button>
            </div>
          </div>
          <Markdown content={content} />
        </div>
      </div>
    </div>
  );
}

function Markdown({ content }: { content: string }) {
  const blocks = parseMarkdownBlocks(content);
  return (
    <div style={{ fontSize: 13, color: "var(--t0)", lineHeight: 1.7 }}>
      {blocks.map((block, i) => {
        if (block.type === "code") {
          return <CodeBlock key={i} code={block.text} lang={block.lang ?? ""} />;
        }
        if (block.type === "heading") {
          const sizes: Record<number, string> = { 1: "16px", 2: "14px", 3: "13px" };
          return (
            <div key={i} style={{ fontWeight: 600, fontSize: sizes[block.level ?? 1] ?? "13px", margin: "10px 0 4px", color: "var(--t0)" }}>
              {renderInline(block.text)}
            </div>
          );
        }
        if (block.type === "ul") {
          return (
            <ul key={i} style={{ margin: "4px 0 4px 20px", padding: 0 }}>
              {block.items!.map((item, j) => (
                <li key={j} style={{ marginBottom: 2 }}>{renderInline(item)}</li>
              ))}
            </ul>
          );
        }
        if (block.type === "ol") {
          return (
            <ol key={i} style={{ margin: "4px 0 4px 20px", padding: 0 }}>
              {block.items!.map((item, j) => (
                <li key={j} style={{ marginBottom: 2 }}>{renderInline(item)}</li>
              ))}
            </ol>
          );
        }
        if (block.type === "hr") {
          return <hr key={i} style={{ border: "none", borderTop: "1px solid var(--border)", margin: "10px 0" }} />;
        }
        // paragraph
        return (
          <p key={i} style={{ margin: "0 0 6px", whiteSpace: "pre-wrap" }}>
            {renderInline(block.text)}
          </p>
        );
      })}
    </div>
  );
}

function renderInline(text: string): React.ReactNode {
  return parseInlineNodes(text).map((node, i) => {
    if (node.t === "bold") return <strong key={i}>{node.v}</strong>;
    if (node.t === "italic") return <em key={i}>{node.v}</em>;
    if (node.t === "code") return (
      <code key={i} style={{ fontFamily: "var(--font-mono)", fontSize: "0.9em", background: "var(--bg3)", padding: "1px 4px", borderRadius: "var(--radius-sm)", color: "var(--teal)" }}>
        {node.v}
      </code>
    );
    return node.v;
  });
}
