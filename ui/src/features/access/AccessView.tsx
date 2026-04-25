import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import type { ConfiguredAPIKeyRecord } from "../../types/runtime";
import { Badge, CopyBtn, Icon, Icons } from "../shared/ui";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function AccessView({ state, actions }: Props) {
  const [tokenVisible, setTokenVisible] = useState(false);
  const [filterTenant, setFilterTenant] = useState("all");
  const [newKeyOpen, setNewKeyOpen] = useState(false);
  const [createdKeyToken, setCreatedKeyToken] = useState<string | null>(null);
  const [createKeyError, setCreateKeyError] = useState("");

  const apiKeys = state.adminConfig?.api_keys ?? [];
  const tenants = state.adminConfig?.tenants ?? [];
  const tenantNames = [...new Set(apiKeys.map(k => k.tenant).filter(Boolean))] as string[];

  const filteredKeys = filterTenant === "all" ? apiKeys : apiKeys.filter(k => k.tenant === filterTenant);
  const grouped = tenantNames.map(t => ({ tenant: t, keys: filteredKeys.filter(k => k.tenant === t) })).filter(g => g.keys.length > 0);
  const ungrouped = filteredKeys.filter(k => !k.tenant);

  function generateSecret(): string {
    const bytes = new Uint8Array(24);
    crypto.getRandomValues(bytes);
    return "hct_sk_" + Array.from(bytes).map(b => b.toString(16).padStart(2, "0")).join("");
  }

  function openNewKey() {
    setNewKeyOpen(true);
    setCreatedKeyToken(null);
    setCreateKeyError("");
    actions.setAPIKeyFormName("");
    actions.setAPIKeyFormTenant("");
    actions.setAPIKeyFormSecret(generateSecret());
  }

  async function handleCreateKey() {
    if (!state.apiKeyFormName.trim() || !state.apiKeyFormSecret.trim()) return;
    const secret = state.apiKeyFormSecret;
    setCreateKeyError("");
    try {
      await actions.upsertAPIKey();
      setCreatedKeyToken(secret);
    } catch (err) {
      setCreateKeyError(err instanceof Error ? err.message : "Failed to create key.");
    }
  }

  async function handleRotateAdminKey() {
    void actions.rotateAPIKey();
  }

  return (
    <div style={{ height: "100%", overflowY: "auto", padding: 16 }}>
      {/* Admin bearer token */}
      <div className="card" style={{ padding: "14px 16px", marginBottom: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
          <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Admin bearer token</span>
          <Badge status={state.authToken ? "healthy" : "down"} label={state.authToken ? "active" : "not set"} />
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <div style={{ flex: 1, background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "7px 12px", fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--t2)", letterSpacing: "0.08em", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {tokenVisible ? (state.authToken || "not set") : "••••••••••••••••••••••••••••••••••••••••••••"}
          </div>
          <button className="btn btn-sm" onClick={() => setTokenVisible(v => !v)}>
            {tokenVisible ? "Hide" : "Reveal"}
          </button>
          <button className="btn btn-sm" onClick={() => handleRotateAdminKey()}>
            <Icon d={Icons.refresh} size={13} /> Rotate
          </button>
        </div>
        <div style={{ fontSize: 11, color: "var(--t3)", marginTop: 5, fontFamily: "var(--font-mono)" }}>
          Set as GATEWAY_ADMIN_TOKEN. Used for control-plane operations.
        </div>
      </div>

      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
        <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>API keys</span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>{apiKeys.length} total</span>
        <div style={{ marginLeft: "auto", display: "flex", gap: 8 }}>
          {tenantNames.length > 0 && (
            <select className="select" value={filterTenant} onChange={e => setFilterTenant(e.target.value)}>
              <option value="all">All tenants</option>
              {tenantNames.map(t => <option key={t} value={t}>{t}</option>)}
            </select>
          )}
          <button className="btn btn-primary btn-sm" onClick={openNewKey}>
            <Icon d={Icons.plus} size={13} /> New key
          </button>
        </div>
      </div>

      {/* Keys grouped by tenant */}
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        {grouped.map(group => (
          <div key={group.tenant}>
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--teal)", fontWeight: 500 }}>{group.tenant}</span>
              <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>{group.keys.length} key{group.keys.length !== 1 ? "s" : ""}</span>
            </div>
            <KeyTable keys={group.keys} onDelete={id => void actions.deleteAPIKey(id)} onToggle={(id, enabled) => void actions.setAPIKeyEnabled(id, enabled)} />
          </div>
        ))}
        {ungrouped.length > 0 && (
          <div>
            <div style={{ marginBottom: 6 }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--t2)" }}>no tenant</span>
            </div>
            <KeyTable keys={ungrouped} onDelete={id => void actions.deleteAPIKey(id)} onToggle={(id, enabled) => void actions.setAPIKeyEnabled(id, enabled)} />
          </div>
        )}
        {apiKeys.length === 0 && (
          <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
            No API keys. Create one above.
          </div>
        )}
      </div>

      {/* Tenants section */}
      {tenants.length > 0 && (
        <div style={{ marginTop: 24 }}>
          <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", marginBottom: 12 }}>Tenants</div>
          <div className="card" style={{ overflow: "hidden" }}>
            <table className="table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>ID</th>
                  <th>Status</th>
                  <th>Providers</th>
                  <th></th>
                </tr>
              </thead>
              <tbody>
                {tenants.map(t => (
                  <tr key={t.id}>
                    <td style={{ color: "var(--t0)", fontWeight: 500 }}>{t.name}</td>
                    <td className="mono" style={{ color: "var(--t2)" }}>{t.id}</td>
                    <td><Badge status={t.enabled ? "enabled" : "disabled"} /></td>
                    <td className="mono" style={{ color: "var(--t2)" }}>{t.allowed_providers?.join(", ") || "all"}</td>
                    <td>
                      <button className="btn btn-ghost btn-sm" style={{ color: "var(--red)", padding: "3px 6px" }}
                        onClick={() => void actions.deleteTenant(t.id)}>
                        <Icon d={Icons.trash} size={13} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Create key slide-over */}
      {newKeyOpen && (
        <div style={{ position: "fixed", inset: 0, zIndex: 50, display: "flex", background: "oklch(0 0 0 / 0.5)" }} onClick={() => setNewKeyOpen(false)}>
          <div style={{ marginLeft: "auto", width: 400, background: "var(--bg1)", borderLeft: "1px solid var(--border)", display: "flex", flexDirection: "column", height: "100%" }} onClick={e => e.stopPropagation()}>
            <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ fontWeight: 500, fontSize: 13 }}>New API key</span>
              <button className="btn btn-ghost btn-sm" style={{ marginLeft: "auto", padding: "3px 6px" }} onClick={() => setNewKeyOpen(false)}>
                <Icon d={Icons.x} size={14} />
              </button>
            </div>

            {createdKeyToken ? (
              <div style={{ padding: "20px 16px", flex: 1 }}>
                <div style={{ textAlign: "center", marginBottom: 20 }}>
                  <div style={{ width: 40, height: 40, borderRadius: "50%", background: "var(--green-bg)", border: "1px solid var(--green-border)", display: "flex", alignItems: "center", justifyContent: "center", margin: "0 auto 10px" }}>
                    <Icon d={Icons.check} size={20} />
                  </div>
                  <div style={{ fontSize: 14, fontWeight: 500, color: "var(--t0)" }}>Key created</div>
                  <div style={{ fontSize: 12, color: "var(--red)", marginTop: 4 }}>Copy this now — it won't be shown again.</div>
                </div>
                <div style={{ background: "var(--bg0)", border: "1px solid var(--teal-border)", borderRadius: "var(--radius-sm)", padding: "10px 12px", fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--teal)", wordBreak: "break-all", marginBottom: 10 }}>
                  {createdKeyToken}
                </div>
                <CopyBtn text={createdKeyToken} />
              </div>
            ) : (
              <div style={{ padding: 16, flex: 1, display: "flex", flexDirection: "column", gap: 12 }}>
                <div>
                  <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>KEY NAME</label>
                  <input className="input" placeholder="e.g. eng-team-ci" value={state.apiKeyFormName}
                    onChange={e => actions.setAPIKeyFormName(e.target.value)} />
                </div>
                <div>
                  <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>TENANT</label>
                  <select className="select" style={{ width: "100%", padding: "7px 10px" }} value={state.apiKeyFormTenant}
                    onChange={e => actions.setAPIKeyFormTenant(e.target.value)}>
                    <option value="">— none —</option>
                    {tenants.map(t => <option key={t.id} value={t.name}>{t.name}</option>)}
                  </select>
                </div>
                <div>
                  <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>ROLE</label>
                  <select className="select" style={{ width: "100%", padding: "7px 10px" }} value={state.apiKeyFormRole}
                    onChange={e => actions.setAPIKeyFormRole(e.target.value)}>
                    <option value="gateway">gateway</option>
                    <option value="admin">admin</option>
                    <option value="readonly">readonly</option>
                  </select>
                </div>
                <div>
                  <div style={{ display: "flex", alignItems: "center", marginBottom: 4 }}>
                    <label style={{ fontSize: 11, color: "var(--t2)", fontFamily: "var(--font-mono)", flex: 1 }}>SECRET</label>
                    <button className="btn btn-ghost btn-sm" style={{ fontSize: 10, padding: "2px 6px" }}
                      onClick={() => actions.setAPIKeyFormSecret(generateSecret())}>Regenerate</button>
                  </div>
                  <input className="input" type="text" value={state.apiKeyFormSecret}
                    onChange={e => actions.setAPIKeyFormSecret(e.target.value)}
                    style={{ fontFamily: "var(--font-mono)", fontSize: 11 }} />
                  <div style={{ fontSize: 10, color: "var(--t3)", marginTop: 3 }}>Auto-generated. You can replace with your own value.</div>
                </div>
              </div>
            )}

            <div style={{ padding: "12px 16px", borderTop: "1px solid var(--border)" }}>
              {createKeyError && (
                <div style={{ fontSize: 11, color: "var(--red)", fontFamily: "var(--font-mono)", marginBottom: 8 }}>
                  {createKeyError}
                </div>
              )}
              <div style={{ display: "flex", gap: 8 }}>
                {!createdKeyToken ? (
                  <>
                    <button className="btn btn-primary" style={{ flex: 1, justifyContent: "center" }}
                      disabled={!state.apiKeyFormName.trim() || !state.apiKeyFormSecret.trim()}
                      onClick={() => void handleCreateKey()}>
                      <Icon d={Icons.plus} size={14} /> Create key
                    </button>
                    <button className="btn" onClick={() => setNewKeyOpen(false)}>Cancel</button>
                  </>
                ) : (
                  <button className="btn btn-primary" style={{ flex: 1, justifyContent: "center" }} onClick={() => setNewKeyOpen(false)}>Done</button>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function KeyTable({ keys, onDelete, onToggle }: {
  keys: ConfiguredAPIKeyRecord[];
  onDelete: (id: string) => void;
  onToggle: (id: string, enabled: boolean) => void;
}) {
  return (
    <div className="card" style={{ overflow: "hidden" }}>
      <table className="table">
        <thead>
          <tr>
            <th>Name</th>
            <th>Preview</th>
            <th>Role</th>
            <th>Status</th>
            <th>Created</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {keys.map(k => (
            <tr key={k.id}>
              <td className="mono" style={{ color: "var(--t0)", fontWeight: 500 }}>{k.name}</td>
              <td>
                <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", maxWidth: 140 }}>
                    {k.key_preview || "••••••••"}
                  </span>
                  {k.key_preview && <CopyBtn text={k.key_preview} />}
                </div>
              </td>
              <td>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, background: "var(--bg3)", padding: "2px 6px", borderRadius: 3, border: "1px solid var(--border)", color: "var(--t1)" }}>
                  {k.role}
                </span>
              </td>
              <td><Badge status={k.enabled ? "enabled" : "disabled"} /></td>
              <td className="mono" style={{ color: "var(--t3)" }}>
                {k.created_at ? new Date(k.created_at).toLocaleDateString() : "—"}
              </td>
              <td>
                <div style={{ display: "flex", gap: 4 }}>
                  <button className="btn btn-ghost btn-sm" style={{ padding: "3px 6px" }}
                    onClick={() => onToggle(k.id, !k.enabled)} title={k.enabled ? "Disable" : "Enable"}>
                    <Icon d={k.enabled ? Icons.eye : Icons.check} size={12} />
                  </button>
                  <button className="btn btn-ghost btn-sm" style={{ color: "var(--red)", padding: "3px 6px" }}
                    onClick={() => onDelete(k.id)}>
                    <Icon d={Icons.trash} size={13} />
                  </button>
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
