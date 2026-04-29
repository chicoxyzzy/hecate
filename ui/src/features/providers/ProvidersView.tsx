import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { buildConflictMap, providerDotColor, resolvedBaseURL } from "../../lib/provider-utils";
import { Dot, Icon, Icons, Toggle } from "../shared/ui";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

const PRESET_COLORS: Record<string, string> = {
  // Vendor-specific brand accents — sourced from each provider's
  // primary brand color, not an aesthetic guess. The rest fall back
  // to semantic tokens because the provider has no distinctive
  // brand color (xAI/OpenAI mark are monochrome black/white; Ollama
  // is grayscale; LMStudio/llama.cpp/LocalAI are community projects
  // without a strong color identity). Edit the --brand-* tokens in
  // styles.css to retune; do not put hex literals here.
  anthropic:   "var(--brand-anthropic)",
  openai:      "var(--brand-openai)",
  gemini:      "var(--brand-gemini)",
  mistral:     "var(--brand-mistral)",
  groq:        "var(--brand-groq)",
  deepseek:    "var(--teal)",
  together_ai: "var(--t2)",
  xai:         "var(--t0)",
  ollama:      "var(--teal)",
  lmstudio:    "var(--t2)",
  llamacpp:    "var(--t2)",
  localai:     "var(--t2)",
};

function iconColorByID(id: string): string {
  return PRESET_COLORS[id.toLowerCase()] ?? "var(--teal)";
}

export function ProvidersView({ state, actions }: Props) {
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [pendingKey, setPendingKey] = useState("");
  const [pendingToggles, setPendingToggles] = useState<Map<string, boolean>>(new Map());

  const configuredProviders = state.adminConfig?.providers ?? [];
  const healthyNames = new Set(state.providers.filter(p => p.healthy).map(p => p.name));
  const statusByName = new Map(state.providers.map(p => [p.name, p]));
  const configuredByID = new Map(configuredProviders.map(p => [p.id, p]));

  const presetOrder = new Map(state.providerPresets.map((p, i) => [p.id, i]));
  const stableSort = (a: string, b: string) => {
    const ai = presetOrder.get(a) ?? 999;
    const bi = presetOrder.get(b) ?? 999;
    return ai !== bi ? ai - bi : a.localeCompare(b);
  };

  const allCloudIDs = configuredProviders.filter(p => p.kind === "cloud").map(p => p.id).sort(stableSort);
  const allLocalIDs = configuredProviders.filter(p => p.kind === "local").map(p => p.id).sort(stableSort);

  // The CP response is the source of truth for enabled state — it has been
  // conflict-resolved server-side. Runtime status (state.providers) is only used
  // for health, not for the toggle itself.
  function configuredEnabled(id: string): boolean {
    return configuredByID.get(id)?.enabled ?? true;
  }

  function resolveEnabled(id: string): boolean {
    return pendingToggles.has(id) ? pendingToggles.get(id)! : configuredEnabled(id);
  }

  const configuredByName = new Map(configuredProviders.filter(p => p.base_url).map(p => [p.name, p]));
  const conflictMap = buildConflictMap(
    [...allCloudIDs, ...allLocalIDs],
    configuredByName,
    state.providerPresets,
  );

  const selectedConfig = selectedID ? configuredByID.get(selectedID) ?? null : null;
  const selectedStatus = selectedID ? statusByName.get(selectedID) : null;
  const selectedPreset = selectedID ? state.providerPresets.find(p => p.id === selectedID) : null;

  // Optimistically reflect mutual exclusion in the UI: enabling a provider flips
  // any conflicting providers to disabled in the pending overlay so the toggle
  // visually updates without waiting for the dashboard refresh. Backend enforces
  // the same constraint authoritatively.
  function toggleProvider(id: string, enabled: boolean) {
    const conflicts = enabled ? (conflictMap.get(id) ?? []) : [];
    setPendingToggles(m => {
      const n = new Map(m);
      n.set(id, enabled);
      for (const cid of conflicts) n.set(cid, false);
      return n;
    });
    void actions.setProviderEnabled(id, enabled).then(() => {
      setPendingToggles(m => {
        const n = new Map(m);
        n.delete(id);
        for (const cid of conflicts) n.delete(cid);
        return n;
      });
    });
  }

  const cloudEnabledCount = allCloudIDs.filter(id => resolveEnabled(id)).length;
  const localEnabledCount = allLocalIDs.filter(id => resolveEnabled(id)).length;

  function renderCard(id: string) {
    const cp = configuredByID.get(id);
    const rt = statusByName.get(id);
    const preset = state.providerPresets.find(p => p.id === id);
    const displayName = preset?.name || cp?.name || id;
    const description = preset?.description ?? "";
    const baseURL = rt?.base_url || resolvedBaseURL(id, cp ?? undefined, state.providerPresets);
    const enabled = resolveEnabled(id);
    const healthy = healthyNames.has(id);
    const modelCount = rt?.model_count ?? rt?.models?.length ?? 0;
    const statusLabel = rt?.status || (enabled ? "unknown" : "disabled");
    const lastError = rt?.last_error || rt?.error;
    const routingReady = rt?.routing_ready ?? (enabled && healthy);
    const routingBlocked = rt?.routing_blocked_reason;
    const conflicts = conflictMap.get(id) ?? [];
    const conflictTitle = conflicts.length > 0
      ? `Shares endpoint with ${conflicts.join(", ")} — only one can serve requests at a time.`
      : undefined;

    return (
      <div key={id} onClick={() => setSelectedID(s => s === id ? null : id)}
        style={{
          background: selectedID === id ? "var(--teal-bg)" : "var(--bg2)",
          border: `1px solid ${selectedID === id ? "var(--teal)" : conflicts.length > 0 ? "var(--amber-border)" : "var(--border)"}`,
          borderRadius: "var(--radius)", padding: "12px 14px", cursor: "pointer",
          transition: "border-color 0.1s, background 0.1s",
        }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: description ? 6 : 8 }}>
          <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--bg3)", border: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "center", fontFamily: "var(--font-mono)", fontSize: 12, fontWeight: 600, color: iconColorByID(id), flexShrink: 0 }}>
            {displayName[0].toUpperCase()}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 12, fontWeight: 500, color: "var(--t0)" }}>{displayName}</div>
          </div>
          <div style={{ display: "flex", alignItems: "center", gap: 6 }} onClick={e => e.stopPropagation()}>
            {conflicts.length > 0 && <span title={conflictTitle} style={{ fontSize: 11, color: "var(--amber)", cursor: "help" }}>⚠</span>}
            <Dot color={providerDotColor(enabled, healthy)} />
            <Toggle on={enabled} onChange={v => toggleProvider(id, v)} ariaLabel={`Enable ${displayName}`} />
          </div>
        </div>
        {description && (
          <div style={{ fontSize: 11, color: "var(--t3)", marginBottom: 8, lineHeight: 1.4 }}>{description}</div>
        )}
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span style={{ fontSize: 11, color: "var(--t2)" }}>
            <span style={{ fontFamily: "var(--font-mono)", color: "var(--t0)", fontWeight: 500 }}>{modelCount}</span> models
          </span>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: healthy ? "var(--green)" : enabled ? "var(--red)" : "var(--t3)" }}>
            {statusLabel}
          </span>
          {enabled && !routingReady && routingBlocked && (
            <span title={routingBlocked} style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--amber)" }}>
              blocked
            </span>
          )}
          {baseURL && (
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{baseURL}</span>
          )}
        </div>
        {lastError && (
          <div style={{ marginTop: 8, paddingTop: 7, borderTop: "1px solid var(--border)", fontSize: 11, color: "var(--red)", lineHeight: 1.4 }}>
            {lastError}
          </div>
        )}
      </div>
    );
  }

  return (
    <div style={{ display: "flex", height: "100%", overflow: "hidden" }}>
      {/* Provider list */}
      <div style={{ flex: 1, overflowY: "auto", padding: 16 }}>

        {/* Cloud */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
          <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Cloud providers</span>
          <span style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
            {cloudEnabledCount}/{allCloudIDs.length} enabled
          </span>
        </div>
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px,1fr))", gap: 10, marginBottom: 24 }}>
          {allCloudIDs.map(id => renderCard(id))}
        </div>

        {/* Local */}
        {allLocalIDs.length > 0 && (
          <>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
              <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Local inference</span>
              <span style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
                {localEnabledCount}/{allLocalIDs.length} connected
              </span>
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px,1fr))", gap: 10 }}>
              {allLocalIDs.map(id => renderCard(id))}
            </div>
          </>
        )}
      </div>

      {/* Detail panel */}
      {selectedID && selectedConfig && (
        <div style={{ width: 320, borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", flexShrink: 0, background: "var(--bg1)" }}>
          {/* Header */}
          <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
            <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--bg3)", border: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "center", fontFamily: "var(--font-mono)", fontSize: 13, fontWeight: 600, color: iconColorByID(selectedID), flexShrink: 0 }}>
              {(selectedConfig.name || selectedID)[0].toUpperCase()}
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>
                {selectedPreset?.name || selectedConfig.name || selectedID}
              </div>
              {selectedConfig.base_url && (
                <div style={{ fontSize: 11, color: "var(--t3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontFamily: "var(--font-mono)" }}>
                  {selectedConfig.base_url}
                </div>
              )}
            </div>
            <button
              className="btn btn-ghost btn-sm"
              style={{ padding: "3px 6px" }}
              onClick={() => setSelectedID(null)}
              aria-label="Close provider details"
              title="Close">
              <Icon d={Icons.x} size={13} />
            </button>
          </div>

          {/* Stats grid */}
          <div style={{ padding: "12px 14px", borderBottom: "1px solid var(--border)", display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
            {([
              ["Models",        selectedStatus?.model_count ?? selectedStatus?.models?.length ?? (selectedConfig.default_model ? "1+" : "0")],
              ["Protocol",      selectedConfig.protocol || "—"],
              ["Kind",          selectedConfig.kind],
              ["Health",        selectedStatus?.status || "unknown"],
              ["Credentials",   selectedStatus?.credential_state || (selectedConfig.credential_configured ? "configured" : selectedConfig.kind === "local" ? "not_required" : "missing")],
              ["Route",         selectedStatus?.routing_ready === false ? selectedStatus.routing_blocked_reason || "blocked" : "ready"],
              ["Default model", selectedConfig.default_model || "—"],
            ] as [string, string | number][]).map(([label, val]) => (
              <div key={label}>
                <div className="kicker" style={{ marginBottom: 2 }}>{label}</div>
                <div style={{ fontSize: 14, fontWeight: 500, color: "var(--t0)", fontFamily: "var(--font-mono)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{val}</div>
              </div>
            ))}
          </div>

          {((selectedStatus?.last_error || selectedStatus?.error) || selectedStatus?.last_checked_at || selectedStatus?.refreshed_at || selectedStatus?.discovery_source) && (
            <div style={{ padding: "12px 14px", borderBottom: "1px solid var(--border)" }}>
              <div className="kicker" style={{ marginBottom: 6 }}>Diagnostics</div>
              <div style={{ display: "flex", flexDirection: "column", gap: 5 }}>
                {(selectedStatus?.last_error || selectedStatus?.error) && (
                  <div style={{ fontSize: 12, color: "var(--red)", lineHeight: 1.45 }}>{selectedStatus.last_error || selectedStatus.error}</div>
                )}
                {selectedStatus?.discovery_source && (
                  <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                    discovery: <span style={{ color: "var(--t1)" }}>{selectedStatus.discovery_source}</span>
                  </div>
                )}
                {(selectedStatus?.last_checked_at || selectedStatus?.refreshed_at) && (
                  <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)" }}>
                    checked: <span style={{ color: "var(--t1)" }}>{formatProviderTime(selectedStatus.last_checked_at || selectedStatus.refreshed_at || "")}</span>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* API key (cloud only — local providers don't need credentials) */}
          {selectedConfig.kind !== "local" && (
            <div style={{ padding: "12px 14px", borderBottom: "1px solid var(--border)", display: "flex", flexDirection: "column", gap: 8 }}>
              <>
                <label className="kicker-lg" style={{ display: "flex", gap: 6 }}>
                  API Key
                  {selectedConfig.credential_source === "env" && !pendingKey && (
                    <span style={{ color: "var(--teal)", fontWeight: 400, textTransform: "none" }}>from env</span>
                  )}
                </label>
                <input className="input" type="password"
                  placeholder={selectedConfig.credential_configured ? "••••••••" : "sk-…"}
                  value={pendingKey}
                  onChange={e => setPendingKey(e.target.value)}
                  style={{ fontFamily: "var(--font-mono)", letterSpacing: "0.1em" }}
                />
                {!selectedConfig.credential_configured && (
                  <div style={{ fontSize: 11, color: "var(--t3)" }}>Stored encrypted at rest. Never logged.</div>
                )}
                <button className="btn btn-primary btn-sm" style={{ justifyContent: "center" }}
                  disabled={!pendingKey.trim()}
                  onClick={() => void actions.setProviderAPIKey(selectedID, pendingKey).then(() => setPendingKey(""))}>
                  <Icon d={Icons.check} size={13} />
                  {selectedConfig.credential_configured ? "Update API key" : "Save API key"}
                </button>
                {selectedConfig.credential_source === "vault" && (
                  <button className="btn btn-danger btn-sm" style={{ justifyContent: "center" }}
                    onClick={() => void actions.setProviderAPIKey(selectedID, "")}>
                    <Icon d={Icons.trash} size={13} /> Delete API key
                  </button>
                )}
              </>
            </div>
          )}

          {/* Model list */}
          {selectedStatus?.models && selectedStatus.models.length > 0 && (
            <div style={{ flex: 1, overflowY: "auto", padding: "10px 14px" }}>
              <div className="kicker" style={{ marginBottom: 6 }}>Models</div>
              {selectedStatus.models.map(m => (
                <div key={m} style={{ display: "flex", alignItems: "center", padding: "6px 0", borderBottom: "1px solid var(--border)" }}>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--t0)", flex: 1 }}>{m}</span>
                  {m === selectedConfig.default_model && <span className="badge badge-teal" style={{ fontSize: 9 }}>default</span>}
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function formatProviderTime(value: string): string {
  const parsed = Date.parse(value);
  if (!Number.isFinite(parsed)) {
    return value;
  }
  return new Date(parsed).toLocaleTimeString();
}
