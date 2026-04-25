import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { buildConflictMap, providerDotColor, resolvedBaseURL } from "../../lib/provider-utils";
import { Dot, Icon, Icons, Toggle } from "../shared/ui";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

const PRESET_COLORS: Record<string, string> = {
  anthropic:  "#c084fc",
  openai:     "var(--t0)",
  google:     "#4ade80",
  deepseek:   "var(--teal)",
  mistral:    "var(--amber)",
  groq:       "var(--amber)",
  together:   "var(--t2)",
  xai:        "var(--t0)",
  ollama:     "var(--teal)",
  lmstudio:   "var(--t2)",
  llamacpp:   "var(--t2)",
  localai:    "var(--t2)",
};

function iconColorByID(id: string): string {
  return PRESET_COLORS[id.toLowerCase()] ?? "var(--teal)";
}

export function ProvidersView({ state, actions }: Props) {
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
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

  function runtimeEnabled(id: string): boolean {
    return statusByName.get(id)?.status !== "disabled";
  }

  function resolveEnabled(id: string): boolean {
    return pendingToggles.has(id) ? pendingToggles.get(id)! : runtimeEnabled(id);
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

  function toggleProvider(id: string, enabled: boolean) {
    setPendingToggles(m => new Map(m).set(id, enabled));
    void actions.setProviderEnabled(id, enabled).then(() => {
      setPendingToggles(m => { const n = new Map(m); n.delete(id); return n; });
    });
  }

  async function testConnection(id: string) {
    setTesting(id);
    await actions.loadDashboard();
    setTesting(null);
  }

  const cloudEnabledCount = allCloudIDs.filter(id => resolveEnabled(id)).length;
  const localEnabledCount = allLocalIDs.filter(id => resolveEnabled(id)).length;

  function renderCard(id: string, isLocal?: boolean) {
    const cp = configuredByID.get(id);
    const rt = statusByName.get(id);
    const preset = state.providerPresets.find(p => p.id === id);
    const displayName = cp?.name || preset?.name || id;
    const description = preset?.description ?? "";
    const baseURL = resolvedBaseURL(id, cp ?? undefined, state.providerPresets);
    const enabled = resolveEnabled(id);
    const healthy = healthyNames.has(id);
    const modelCount = rt?.models?.length ?? 0;
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
            <Toggle on={enabled} onChange={v => toggleProvider(id, v)} />
          </div>
        </div>
        {description && (
          <div style={{ fontSize: 11, color: "var(--t3)", marginBottom: 8, lineHeight: 1.4 }}>{description}</div>
        )}
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <span style={{ fontSize: 11, color: "var(--t2)" }}>
            <span style={{ fontFamily: "var(--font-mono)", color: "var(--t0)", fontWeight: 500 }}>{modelCount}</span> models
          </span>
          {baseURL && (
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{baseURL}</span>
          )}
          {isLocal && enabled && (
            <button className="btn btn-ghost btn-sm"
              style={{ marginLeft: "auto", padding: "2px 6px", fontSize: 10, flexShrink: 0 }}
              onClick={e => { e.stopPropagation(); void testConnection(id); }}>
              {testing === id ? "testing…" : "test"}
            </button>
          )}
        </div>
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
              {allLocalIDs.map(id => renderCard(id, true))}
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
            <button className="btn btn-ghost btn-sm" style={{ padding: "3px 6px" }} onClick={() => setSelectedID(null)}>
              <Icon d={Icons.x} size={13} />
            </button>
          </div>

          {/* Stats grid */}
          <div style={{ padding: "12px 14px", borderBottom: "1px solid var(--border)", display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
            {([
              ["Models",        selectedStatus?.models?.length ?? (selectedConfig.default_model ? "1+" : "0")],
              ["Protocol",      selectedConfig.protocol || "—"],
              ["Kind",          selectedConfig.kind],
              ["Default model", selectedConfig.default_model || "—"],
            ] as [string, string | number][]).map(([label, val]) => (
              <div key={label}>
                <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 2, letterSpacing: "0.05em", textTransform: "uppercase" }}>{label}</div>
                <div style={{ fontSize: 14, fontWeight: 500, color: "var(--t0)", fontFamily: "var(--font-mono)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{val}</div>
              </div>
            ))}
          </div>

          {/* API key (cloud) / test connection (local) */}
          <div style={{ padding: "12px 14px", borderBottom: "1px solid var(--border)", display: "flex", flexDirection: "column", gap: 8 }}>
            {selectedConfig.kind === "local" ? (
              <button className="btn btn-sm" style={{ justifyContent: "center" }}
                onClick={() => void testConnection(selectedID)}>
                <Icon d={Icons.activity} size={13} />
                {testing === selectedID ? "Testing…" : "Test connection"}
              </button>
            ) : (
              <>
                <label style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)", textTransform: "uppercase", letterSpacing: "0.05em", display: "flex", gap: 6 }}>
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
                  onClick={() => void actions.saveProviderKey(selectedID, pendingKey).then(() => setPendingKey(""))}>
                  <Icon d={Icons.check} size={13} />
                  {selectedConfig.credential_configured ? "Update API key" : "Save API key"}
                </button>
                {selectedConfig.credential_source === "vault" && (
                  <button className="btn btn-danger btn-sm" style={{ justifyContent: "center" }}
                    onClick={() => void actions.deleteProviderCredential(selectedID)}>
                    <Icon d={Icons.trash} size={13} /> Delete API key
                  </button>
                )}
              </>
            )}
          </div>

          {/* Model list */}
          {selectedStatus?.models && selectedStatus.models.length > 0 && (
            <div style={{ flex: 1, overflowY: "auto", padding: "10px 14px" }}>
              <div style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)", marginBottom: 6, letterSpacing: "0.06em", textTransform: "uppercase" }}>Models</div>
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
