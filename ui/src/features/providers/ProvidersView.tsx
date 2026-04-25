import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { buildConflictMap, providerDotColor, resolvedBaseURL } from "../../lib/provider-utils";
import type { ConfiguredProviderRecord } from "../../types/runtime";
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

function iconColor(provider: ConfiguredProviderRecord): string {
  const key = (provider.preset_id ?? provider.name ?? "").toLowerCase();
  return PRESET_COLORS[key] ?? "var(--teal)";
}

function iconColorByName(name: string): string {
  return PRESET_COLORS[name.toLowerCase()] ?? "var(--teal)";
}


export function ProvidersView({ state, actions }: Props) {
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [testing, setTesting] = useState<string | null>(null);
  const [pendingKey, setPendingKey] = useState("");
  const [pendingToggles, setPendingToggles] = useState<Map<string, boolean>>(new Map());

  const configuredProviders = state.adminConfig?.providers ?? [];
  const healthyNames = new Set(state.providers.filter(p => p.healthy).map(p => p.name));
  const statusByName = new Map(state.providers.map(p => [p.name, p]));
  const configuredByName = new Map(configuredProviders.filter(p => p.base_url).map(p => [p.name, p]));

  const presetOrder = new Map(state.providerPresets.map((p, i) => [p.id, i]));
  const stableSort = (a: string, b: string) => {
    const ai = presetOrder.get(a) ?? 999;
    const bi = presetOrder.get(b) ?? 999;
    return ai !== bi ? ai - bi : a.localeCompare(b);
  };

  const allCloudNames = state.providers.filter(p => p.kind === "cloud" && p.name).map(p => p.name).sort(stableSort);
  const allLocalNames = state.providers.filter(p => p.kind === "local" && p.name).map(p => p.name).sort(stableSort);

  function runtimeEnabled(name: string): boolean {
    return statusByName.get(name)?.status !== "disabled";
  }

  function resolveEnabled(name: string): boolean {
    return pendingToggles.has(name) ? pendingToggles.get(name)! : runtimeEnabled(name);
  }

  const conflictMap = buildConflictMap(
    [...allCloudNames, ...allLocalNames],
    configuredByName,
    state.providerPresets,
  );

  const selectedConfig = selectedID ? configuredByName.get(selectedID) ?? null : null;
  const selectedStatus = selectedID ? statusByName.get(selectedID) : null;
  const selectedPreset = selectedID ? state.providerPresets.find(p => p.id === selectedID) : null;
  const selectedKind = selectedStatus?.kind || selectedPreset?.kind || "cloud";
  const selectedNeedsKey = !!(selectedID && !selectedConfig && selectedKind !== "local");

  function toggleProvider(name: string, enabled: boolean) {
    setPendingToggles(m => new Map(m).set(name, enabled));
    void actions.setProviderEnabled(name, enabled).then(() => {
      setPendingToggles(m => { const n = new Map(m); n.delete(name); return n; });
    });
  }

  async function testConnection(name: string) {
    setTesting(name);
    await actions.loadDashboard();
    setTesting(null);
  }

  const cloudEnabledCount = allCloudNames.filter(n => resolveEnabled(n)).length;
  const localEnabledCount = allLocalNames.filter(n => resolveEnabled(n)).length;

  return (
    <div style={{ display: "flex", height: "100%", overflow: "hidden" }}>
      {/* Provider list */}
      <div style={{ flex: 1, overflowY: "auto", padding: 16 }}>

        {/* Cloud */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
          <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Cloud providers</span>
          <span style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
            {`${cloudEnabledCount}/${allCloudNames.length} enabled`}
          </span>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px,1fr))", gap: 10, marginBottom: 24 }}>
          {allCloudNames.map(name => {
            const config = configuredByName.get(name);
            const rt = statusByName.get(name);
            const preset = state.providerPresets.find(p => p.id === name);
            const displayName = preset?.name || name;
            const description = preset?.description ?? "";
            const baseURL = resolvedBaseURL(name, config, state.providerPresets);
            const enabled = resolveEnabled(name);
            const conflicts = conflictMap.get(name) ?? [];
            if (config) return (
              <ProviderCard key={name}
                displayName={displayName}
                color={iconColor(config)}
                baseURL={baseURL}
                description={description}
                enabled={enabled}
                dotColor={providerDotColor(enabled, healthyNames.has(name))}
                modelCount={rt?.models?.length ?? 0}
                protocol={config.protocol}
                selected={selectedID === name}
                onSelect={() => setSelectedID(s => s === name ? null : name)}
                onToggle={v => toggleProvider(name, v)}
                conflicts={conflicts} />
            );
            return (
              <FallbackCard key={name} name={displayName}
                baseURL={baseURL}
                description={description}
                enabled={enabled}
                healthy={rt?.healthy ?? false}
                modelCount={rt?.models?.length ?? 0}
                selected={selectedID === name}
                onSelect={() => setSelectedID(s => s === name ? null : name)}
                onToggle={v => toggleProvider(name, v)}
                conflicts={conflicts} />
            );
          })}
        </div>

        {/* Local */}
        {allLocalNames.length > 0 && (
          <>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 14 }}>
              <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Local inference</span>
              <span style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
                {localEnabledCount}/{allLocalNames.length} connected
              </span>
            </div>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(280px,1fr))", gap: 10 }}>
              {allLocalNames.map(name => {
                const config = configuredByName.get(name);
                const rt = statusByName.get(name);
                const preset = state.providerPresets.find(p => p.id === name);
                const displayName = preset?.name || name;
                const description = preset?.description ?? "";
                const baseURL = resolvedBaseURL(name, config, state.providerPresets);
                const enabled = resolveEnabled(name);
                const conflicts = conflictMap.get(name) ?? [];
                if (config) return (
                  <ProviderCard key={name}
                    displayName={displayName}
                    color={iconColor(config)}
                    baseURL={baseURL}
                    description={description}
                    enabled={enabled}
                    dotColor={providerDotColor(enabled, healthyNames.has(name))}
                    modelCount={rt?.models?.length ?? 0}
                    selected={selectedID === name}
                    onSelect={() => setSelectedID(s => s === name ? null : name)}
                    onToggle={v => toggleProvider(name, v)}
                    isLocal
                    testing={testing === name}
                    onTest={() => void testConnection(name)}
                    conflicts={conflicts} />
                );
                return (
                  <FallbackCard key={name} name={displayName}
                    baseURL={baseURL}
                    description={description}
                    enabled={enabled}
                    healthy={rt?.healthy ?? false}
                    modelCount={rt?.models?.length ?? 0}
                    interactive={false}
                    selected={false}
                    onSelect={() => {}}
                    onToggle={v => toggleProvider(name, v)}
                    conflicts={conflicts} />
                );
              })}
            </div>
          </>
        )}

        {configuredProviders.length === 0 && state.providers.length === 0 && (
          <div style={{ textAlign: "center", padding: "48px 16px", color: "var(--t3)", fontSize: 12 }}>
            No providers configured. Click "Add provider" to get started.
          </div>
        )}
      </div>

      {/* Detail panel */}
      {selectedConfig && (
        <div style={{ width: 320, borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", flexShrink: 0, background: "var(--bg1)" }}>
          <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
            <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--bg3)", border: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "center", fontFamily: "var(--font-mono)", fontSize: 13, fontWeight: 600, color: iconColor(selectedConfig), flexShrink: 0 }}>
              {selectedConfig.name[0].toUpperCase()}
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>{state.providerPresets.find(p => p.id === selectedConfig.name || p.id === selectedConfig.preset_id)?.name || selectedConfig.name}</div>
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

          <div style={{ padding: "12px 14px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "space-between" }}>
            <div style={{ display: "flex", alignItems: "center", gap: 7 }}>
              <Dot color={providerDotColor(resolveEnabled(selectedConfig.name), healthyNames.has(selectedConfig.name))} />
              <span style={{ fontSize: 12, color: "var(--t1)" }}>
                {selectedStatus?.status || (resolveEnabled(selectedConfig.name) ? "unknown" : "disabled")}
              </span>
            </div>
            <Toggle on={resolveEnabled(selectedConfig.name)} onChange={v => toggleProvider(selectedConfig.name, v)} label={resolveEnabled(selectedConfig.name) ? "Enabled" : "Disabled"} />
          </div>

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

          <div style={{ padding: "10px 14px", borderTop: "1px solid var(--border)", display: "flex", flexDirection: "column", gap: 6 }}>
            {selectedConfig.kind === "local" && (
              <button className="btn btn-sm" style={{ justifyContent: "center" }}
                onClick={() => void testConnection(selectedConfig.name)}>
                <Icon d={Icons.activity} size={13} />
                {testing === selectedConfig.name ? "Testing…" : "Test connection"}
              </button>
            )}
            <button className="btn btn-sm" style={{ justifyContent: "center" }}
              onClick={() => {
                actions.setRotateProviderID(selectedConfig.name);
                void actions.rotateProviderCredential();
              }}>
              <Icon d={Icons.refresh} size={13} /> Rotate API key
            </button>
            <button className="btn btn-danger btn-sm" style={{ justifyContent: "center" }}
              onClick={() => void actions.deleteProvider(selectedConfig.name).then(() => setSelectedID(null))}>
              <Icon d={Icons.trash} size={13} /> Remove provider
            </button>
          </div>
        </div>
      )}

      {/* API key panel for env-configured cloud providers */}
      {selectedNeedsKey && !selectedConfig && (
        <div style={{ width: 320, borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", flexShrink: 0, background: "var(--bg1)" }}>
          <div style={{ padding: "10px 14px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
            <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--bg3)", border: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "center", fontFamily: "var(--font-mono)", fontSize: 13, fontWeight: 600, color: iconColorByName(selectedID!), flexShrink: 0 }}>
              {selectedID![0].toUpperCase()}
            </div>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>{selectedPreset?.name || selectedID}</div>
              {selectedPreset?.base_url && (
                <div style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>{selectedPreset.base_url}</div>
              )}
            </div>
            <button className="btn btn-ghost btn-sm" style={{ padding: "3px 6px" }} onClick={() => setSelectedID(null)}>
              <Icon d={Icons.x} size={13} />
            </button>
          </div>

          <div style={{ padding: "14px", flex: 1 }}>
            <label style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)", textTransform: "uppercase", letterSpacing: "0.05em", display: "block", marginBottom: 4 }}>API Key</label>
            <input className="input" type="password" placeholder="sk-…"
              value={pendingKey}
              onChange={e => setPendingKey(e.target.value)}
              style={{ fontFamily: "var(--font-mono)", letterSpacing: "0.1em", marginBottom: 6 }}
            />
            <div style={{ fontSize: 11, color: "var(--t3)", marginBottom: 12 }}>Stored encrypted at rest. Never logged.</div>
            <button className="btn btn-primary btn-sm" style={{ width: "100%", justifyContent: "center" }}
              disabled={!pendingKey.trim()}
              onClick={() => {
                void actions.saveProviderKey(selectedID!, pendingKey).then(() => setPendingKey(""));
              }}>
              <Icon d={Icons.check} size={13} /> Save API key
            </button>
          </div>
        </div>
      )}

    </div>
  );
}

function ProviderCard({ displayName, color, baseURL, description, enabled, dotColor, modelCount, protocol, selected, onSelect, onToggle, isLocal, testing, onTest, conflicts }: {
  displayName: string;
  color: string;
  baseURL: string;
  description: string;
  enabled: boolean;
  dotColor: "green" | "amber" | "red";
  modelCount: number;
  protocol?: string;
  selected: boolean;
  onSelect: () => void;
  onToggle: (v: boolean) => void;
  isLocal?: boolean;
  testing?: boolean;
  onTest?: () => void;
  conflicts: string[];
}) {
  const [hovered, setHovered] = useState(false);
  return (
    <div onClick={onSelect} onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
      style={{
        background: selected ? "var(--teal-bg)" : "var(--bg2)",
        border: `1px solid ${selected ? "var(--teal)" : conflicts.length > 0 ? "var(--amber-border)" : "var(--border)"}`,
        borderRadius: "var(--radius)", padding: "12px 14px", cursor: "pointer",
        transition: "border-color 0.1s, background 0.1s",
      }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: description ? 6 : 8 }}>
        <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--bg3)", border: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "center", fontFamily: "var(--font-mono)", fontSize: 12, fontWeight: 600, color, flexShrink: 0 }}>
          {displayName[0].toUpperCase()}
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: "var(--t0)" }}>{displayName}</div>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }} onClick={e => e.stopPropagation()}>
          {conflicts.length > 0 && <span style={{ fontSize: 11, color: "var(--amber)" }}>⚠</span>}
          <Dot color={dotColor} />
          <Toggle on={enabled} onChange={onToggle} />
        </div>
      </div>

      {description && (
        <div style={{ fontSize: 11, color: "var(--t3)", marginBottom: 8, lineHeight: 1.4 }}>{description}</div>
      )}
      {conflicts.length > 0 && hovered && (
        <div style={{ fontSize: 11, color: "var(--amber)", marginBottom: 8, lineHeight: 1.4 }}>
          Shares endpoint with {conflicts.join(", ")} — only one can serve requests at a time.
        </div>
      )}
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <span style={{ fontSize: 11, color: "var(--t2)" }}>
          <span style={{ fontFamily: "var(--font-mono)", color: "var(--t0)", fontWeight: 500 }}>{modelCount}</span> models
        </span>
        {baseURL ? (
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{baseURL}</span>
        ) : protocol ? (
          <span style={{ fontSize: 11, color: "var(--t2)", fontFamily: "var(--font-mono)" }}>{protocol}</span>
        ) : null}
        {isLocal && enabled && onTest && (
          <button className="btn btn-ghost btn-sm"
            style={{ marginLeft: "auto", padding: "2px 6px", fontSize: 10, flexShrink: 0 }}
            onClick={e => { e.stopPropagation(); onTest(); }}>
            {testing ? "testing…" : "test"}
          </button>
        )}
      </div>
    </div>
  );
}

function FallbackCard({ name, baseURL, description, enabled, healthy, modelCount, selected, onSelect, onToggle, interactive = true, conflicts }: {
  name: string; baseURL: string; description: string; enabled: boolean; healthy: boolean; modelCount: number;
  selected: boolean; onSelect: () => void; onToggle: (v: boolean) => void;
  interactive?: boolean; conflicts: string[];
}) {
  const [hovered, setHovered] = useState(false);
  return (
    <div onClick={interactive ? onSelect : undefined}
      onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}
      style={{
        background: selected ? "var(--teal-bg)" : "var(--bg2)",
        border: `1px solid ${selected ? "var(--teal)" : conflicts.length > 0 ? "var(--amber-border)" : "var(--border)"}`,
        borderRadius: "var(--radius)", padding: "12px 14px",
        cursor: interactive ? "pointer" : "default",
        opacity: interactive ? 1 : 0.7,
        transition: "border-color 0.1s, background 0.1s",
      }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <div style={{ width: 28, height: 28, borderRadius: "var(--radius-sm)", background: "var(--bg3)", border: "1px solid var(--border)", display: "flex", alignItems: "center", justifyContent: "center", fontFamily: "var(--font-mono)", fontSize: 12, fontWeight: 600, color: "var(--teal)", flexShrink: 0 }}>
          {name[0].toUpperCase()}
        </div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ fontSize: 12, fontWeight: 500, color: "var(--t0)" }}>{name}</div>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }} onClick={e => e.stopPropagation()}>
          {conflicts.length > 0 && <span style={{ fontSize: 11, color: "var(--amber)" }}>⚠</span>}
          <Dot color={!enabled ? "red" : healthy ? "green" : "amber"} />
          <Toggle on={enabled} onChange={onToggle} />
        </div>
      </div>
      {description && (
        <div style={{ fontSize: 11, color: "var(--t3)", marginBottom: 8, lineHeight: 1.4 }}>{description}</div>
      )}
      {conflicts.length > 0 && hovered && (
        <div style={{ fontSize: 11, color: "var(--amber)", marginBottom: 8, lineHeight: 1.4 }}>
          Shares endpoint with {conflicts.join(", ")} — only one can serve requests at a time.
        </div>
      )}
      <div style={{ display: "flex", gap: 12, alignItems: "center" }}>
        <span style={{ fontSize: 11, color: "var(--t2)" }}>
          <span style={{ fontFamily: "var(--font-mono)", color: "var(--t0)", fontWeight: 500 }}>{modelCount}</span> models
        </span>
        {baseURL && (
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", flex: 1 }}>{baseURL}</span>
        )}
      </div>
    </div>
  );
}
