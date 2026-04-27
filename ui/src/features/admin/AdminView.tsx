import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import type { ConfiguredAPIKeyRecord } from "../../types/runtime";
import { Badge, CopyBtn, Dot, Icon, Icons, InlineError, SlideOver } from "../shared/ui";
import { PricebookTab } from "./PricebookTab";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

type Tab = "keys" | "tenants" | "budget" | "usage" | "pricebook" | "retention";

const TAB_STORAGE_KEY = "hecate.adminTab";
const VALID_TABS: readonly Tab[] = ["keys", "tenants", "budget", "usage", "pricebook", "retention"];

export function AdminView({ state, actions }: Props) {
  // Persist the admin sub-tab so refreshing while on (say) Pricebook
  // returns the operator to Pricebook, not Keys. The lazy initializer
  // reads localStorage; the setter wrapper writes it back. Validating
  // against VALID_TABS guards against stale values from older builds
  // (e.g. a tab id that no longer exists).
  const [tab, setTabRaw] = useState<Tab>(() => {
    const saved = localStorage.getItem(TAB_STORAGE_KEY);
    return saved && (VALID_TABS as readonly string[]).includes(saved) ? (saved as Tab) : "keys";
  });
  const setTab = (next: Tab) => {
    localStorage.setItem(TAB_STORAGE_KEY, next);
    setTabRaw(next);
  };

  return (
    <div style={{ height: "100%", display: "flex", flexDirection: "column", overflow: "hidden" }}>
      {/* Admin bearer token */}
      <AdminToken state={state} actions={actions} />

      {/* Tab bar */}
      <div style={{ display: "flex", gap: 2, padding: "0 16px", borderBottom: "1px solid var(--border)", flexShrink: 0 }}>
        {(["keys", "tenants", "budget", "usage", "pricebook", "retention"] as Tab[]).map(t => (
          <button key={t} type="button"
            onClick={() => setTab(t)}
            style={{
              padding: "7px 12px",
              fontSize: 12,
              fontFamily: "var(--font-mono)",
              background: "none",
              border: "none",
              borderBottom: tab === t ? "2px solid var(--teal)" : "2px solid transparent",
              color: tab === t ? "var(--teal)" : "var(--t2)",
              cursor: "pointer",
              marginBottom: -1,
              textTransform: "uppercase",
              letterSpacing: "0.04em",
            }}>
            {t}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div style={{ flex: 1, overflowY: "auto", padding: 16 }}>
        {tab === "keys"      && <KeysTab state={state} actions={actions} />}
        {tab === "tenants"   && <TenantsTab state={state} actions={actions} />}
        {tab === "budget"    && <BudgetTab state={state} actions={actions} />}
        {tab === "usage"     && <UsageTab state={state} />}
        {tab === "pricebook" && <PricebookTab state={state} actions={actions} />}
        {tab === "retention" && <RetentionTab state={state} actions={actions} />}
      </div>
    </div>
  );
}

// ─── Admin bearer token ───────────────────────────────────────────────────────

function AdminToken({ state, actions }: Props) {
  const [visible, setVisible] = useState(false);

  return (
    <div className="card" style={{ margin: "12px 16px 0", padding: "10px 14px", flexShrink: 0 }}>
      <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
        <span style={{ fontSize: 12, fontWeight: 500, color: "var(--t1)", whiteSpace: "nowrap" }}>Admin token</span>
        <Badge status={state.authToken ? "healthy" : "down"} label={state.authToken ? "active" : "not set"} />
        <div style={{ flex: 1, background: "var(--bg0)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)", padding: "5px 10px", fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {visible ? (state.authToken || "not set") : "••••••••••••••••••••••••••••••••••••••••••••"}
        </div>
        <button className="btn btn-sm" onClick={() => setVisible(v => !v)}>{visible ? "Hide" : "Reveal"}</button>
        <button className="btn btn-sm" onClick={() => void actions.rotateAPIKey()}>
          <Icon d={Icons.refresh} size={13} /> Rotate
        </button>
      </div>
      <div style={{ fontSize: 10, color: "var(--t3)", marginTop: 4, fontFamily: "var(--font-mono)" }}>
        GATEWAY_ADMIN_TOKEN — required for control-plane operations
      </div>
    </div>
  );
}

// ─── Keys tab ────────────────────────────────────────────────────────────────

function KeysTab({ state, actions }: Props) {
  const [filterTenant, setFilterTenant] = useState("all");
  const [newKeyOpen, setNewKeyOpen] = useState(false);
  const [rotateOpen, setRotateOpen] = useState(false);
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
    actions.setAPIKeyFormRole("tenant");
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

  async function handleRotateKey() {
    if (!state.rotateAPIKeyID.trim()) return;
    await actions.rotateAPIKey();
    setRotateOpen(false);
  }

  return (
    <>
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
          <button className="btn btn-sm" onClick={() => setRotateOpen(true)}>
            <Icon d={Icons.refresh} size={13} /> Rotate key
          </button>
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

      {/* New key slide-over */}
      {newKeyOpen && (
        <SlideOver title="New API key" onClose={() => setNewKeyOpen(false)}
          footer={
            <>
              {createKeyError && <div style={{ marginBottom: 8 }}><InlineError message={createKeyError} /></div>}
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
            </>
          }>
          {createdKeyToken ? (
            <div style={{ padding: "20px 0" }}>
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
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              <Field label="KEY NAME">
                <input className="input" placeholder="e.g. eng-team-ci" value={state.apiKeyFormName}
                  onChange={e => actions.setAPIKeyFormName(e.target.value)} />
              </Field>
              <Field label="TENANT">
                <select className="select" style={{ width: "100%", padding: "7px 10px" }} value={state.apiKeyFormTenant}
                  onChange={e => actions.setAPIKeyFormTenant(e.target.value)}>
                  <option value="">— none —</option>
                  {tenants.map(t => <option key={t.id} value={t.name}>{t.name}</option>)}
                </select>
              </Field>
              <Field label="ROLE">
                <select className="select" style={{ width: "100%", padding: "7px 10px" }} value={state.apiKeyFormRole}
                  onChange={e => actions.setAPIKeyFormRole(e.target.value)}>
                  <option value="tenant">tenant</option>
                  <option value="gateway">gateway</option>
                  <option value="admin">admin</option>
                  <option value="readonly">readonly</option>
                </select>
              </Field>
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
        </SlideOver>
      )}

      {/* Rotate key slide-over */}
      {rotateOpen && (
        <SlideOver title="Rotate API key" onClose={() => setRotateOpen(false)}
          footer={
            <div style={{ display: "flex", gap: 8 }}>
              <button className="btn btn-primary" style={{ flex: 1, justifyContent: "center" }}
                disabled={!state.rotateAPIKeyID.trim()}
                onClick={() => void handleRotateKey()}>
                <Icon d={Icons.refresh} size={14} /> Rotate
              </button>
              <button className="btn" onClick={() => setRotateOpen(false)}>Cancel</button>
            </div>
          }>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <Field label="KEY ID">
              <select className="select" style={{ width: "100%", padding: "7px 10px" }}
                value={state.rotateAPIKeyID}
                onChange={e => actions.setRotateAPIKeyID(e.target.value)}>
                <option value="">— select key —</option>
                {apiKeys.map(k => <option key={k.id} value={k.id}>{k.name} ({k.id})</option>)}
              </select>
            </Field>
            <Field label="NEW SECRET (optional — leave blank to auto-generate)">
              <input className="input" type="text" value={state.rotateAPIKeySecret}
                onChange={e => actions.setRotateAPIKeySecret(e.target.value)}
                placeholder="leave blank to auto-generate"
                style={{ fontFamily: "var(--font-mono)", fontSize: 11 }} />
            </Field>
            <div style={{ fontSize: 11, color: "var(--amber)", fontFamily: "var(--font-mono)" }}>
              The old secret will be invalidated immediately.
            </div>
          </div>
        </SlideOver>
      )}
    </>
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
          <tr><th>Name</th><th>Preview</th><th>Role</th><th>Status</th><th>Created</th><th></th></tr>
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
              <td className="mono" style={{ color: "var(--t3)" }}>{k.created_at ? new Date(k.created_at).toLocaleDateString() : "—"}</td>
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

// ─── Tenants tab ─────────────────────────────────────────────────────────────

function TenantsTab({ state, actions }: Props) {
  const [newOpen, setNewOpen] = useState(false);
  const [createError, setCreateError] = useState("");

  const tenants = state.adminConfig?.tenants ?? [];

  async function handleCreate() {
    if (!state.tenantFormName.trim()) return;
    setCreateError("");
    try {
      await actions.upsertTenant();
      setNewOpen(false);
      actions.setTenantFormName("");
      actions.setTenantFormID("");
      actions.setTenantFormProviders("");
      actions.setTenantFormModels("");
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "Failed to create tenant.");
    }
  }

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
        <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Tenants</span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>{tenants.length} total</span>
        <button className="btn btn-primary btn-sm" style={{ marginLeft: "auto" }} onClick={() => {
          setNewOpen(true);
          setCreateError("");
          actions.setTenantFormName("");
          actions.setTenantFormID("");
          actions.setTenantFormProviders("");
          actions.setTenantFormModels("");
        }}>
          <Icon d={Icons.plus} size={13} /> New tenant
        </button>
      </div>

      {tenants.length > 0 ? (
        <div className="card" style={{ overflow: "hidden" }}>
          <table className="table">
            <thead>
              <tr><th>Name</th><th>ID</th><th>Status</th><th>Allowed providers</th><th>Allowed models</th><th></th></tr>
            </thead>
            <tbody>
              {tenants.map(t => (
                <tr key={t.id}>
                  <td style={{ color: "var(--t0)", fontWeight: 500 }}>{t.name}</td>
                  <td className="mono" style={{ color: "var(--t2)" }}>{t.id}</td>
                  <td>
                    <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                      <Badge status={t.enabled ? "enabled" : "disabled"} />
                      <button className="btn btn-ghost btn-sm" style={{ padding: "2px 5px", fontSize: 10 }}
                        onClick={() => void actions.setTenantEnabled(t.id, !t.enabled)}>
                        {t.enabled ? "Disable" : "Enable"}
                      </button>
                    </div>
                  </td>
                  <td className="mono" style={{ color: "var(--t2)" }}>{t.allowed_providers?.join(", ") || "all"}</td>
                  <td className="mono" style={{ color: "var(--t2)" }}>{(t as Record<string, unknown>).allowed_models as string ?? "all"}</td>
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
      ) : (
        <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
          No tenants. Create one above.
        </div>
      )}

      {newOpen && (
        <SlideOver title="New tenant" onClose={() => setNewOpen(false)}
          footer={
            <>
              {createError && <div style={{ marginBottom: 8 }}><InlineError message={createError} /></div>}
              <div style={{ display: "flex", gap: 8 }}>
                <button className="btn btn-primary" style={{ flex: 1, justifyContent: "center" }}
                  disabled={!state.tenantFormName.trim()}
                  onClick={() => void handleCreate()}>
                  <Icon d={Icons.plus} size={14} /> Create tenant
                </button>
                <button className="btn" onClick={() => setNewOpen(false)}>Cancel</button>
              </div>
            </>
          }>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            <Field label="NAME">
              <input className="input" placeholder="e.g. engineering" value={state.tenantFormName}
                onChange={e => actions.setTenantFormName(e.target.value)} />
            </Field>
            <Field label="ID (optional — auto-generated if blank)">
              <input className="input" placeholder="e.g. engineering" value={state.tenantFormID}
                onChange={e => actions.setTenantFormID(e.target.value)}
                style={{ fontFamily: "var(--font-mono)" }} />
            </Field>
            <Field label="ALLOWED PROVIDERS (comma-separated, blank = all)">
              <input className="input" placeholder="e.g. openai, anthropic"
                value={state.tenantFormProviders}
                onChange={e => actions.setTenantFormProviders(e.target.value)} />
            </Field>
            <Field label="ALLOWED MODELS (comma-separated, blank = all)">
              <input className="input" placeholder="e.g. gpt-4o, claude-3-5-sonnet"
                value={state.tenantFormModels}
                onChange={e => actions.setTenantFormModels(e.target.value)} />
            </Field>
          </div>
        </SlideOver>
      )}
    </>
  );
}

// ─── Budget tab ───────────────────────────────────────────────────────────────

function BudgetTab({ state, actions }: Props) {
  const [editingID, setEditingID] = useState<string | null>(null);
  const [editLimit, setEditLimit] = useState("");
  const [editWarn, setEditWarn] = useState("");

  const budget = state.budget;
  const accountSummary = state.accountSummary;

  function pct(debited: number, limit: number) {
    if (!limit) return 0;
    return Math.min(100, Math.round((debited / limit) * 100));
  }

  function barClass(p: number, warnThreshold: number): string {
    if (p >= 90) return "progress-red";
    if (p >= warnThreshold) return "progress-amber";
    return "progress-teal";
  }

  if (!budget) {
    return (
      <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
        Budget data unavailable. Admin access required.
      </div>
    );
  }

  return (
    <>
      <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", marginBottom: 10 }}>Account budget</div>
      <div className="card" style={{ padding: "14px 16px", marginBottom: 20 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>{budget.scope}</span>
          {budget.enforced ? <Badge status="enabled" label="enforced" /> : <Badge status="disabled" label="not enforced" />}
          <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
            {editingID === "account" ? (
              <>
                <button className="btn btn-primary btn-sm" onClick={() => { void actions.setBudgetLimit(); setEditingID(null); }}>Save</button>
                <button className="btn btn-sm" onClick={() => setEditingID(null)}>Cancel</button>
              </>
            ) : (
              <>
                <button className="btn btn-ghost btn-sm" onClick={() => void actions.topUpBudget()} style={{ gap: 4, fontSize: 11 }}>
                  <Icon d={Icons.plus} size={12} /> Top up
                </button>
                <button className="btn btn-ghost btn-sm" onClick={() => {
                  setEditingID("account");
                  setEditLimit(String(budget.credited_micros_usd / 1_000_000));
                  setEditWarn("80");
                }} style={{ gap: 4, fontSize: 11 }}>
                  <Icon d={Icons.edit} size={12} /> Adjust
                </button>
              </>
            )}
          </div>
        </div>

        <div style={{ marginBottom: 10 }}>
          <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 5 }}>
            <span style={{ fontSize: 11, color: "var(--t2)" }}>
              <span style={{ fontFamily: "var(--font-mono)", color: "var(--t0)", fontWeight: 500 }}>{budget.debited_usd}</span>{" "}spent of {budget.credited_usd} credited
            </span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)" }}>
              {pct(budget.debited_micros_usd, budget.credited_micros_usd)}%
            </span>
          </div>
          <div className="progress-wrap">
            <div className={`progress-bar ${barClass(pct(budget.debited_micros_usd, budget.credited_micros_usd), 75)}`}
              style={{ width: `${pct(budget.debited_micros_usd, budget.credited_micros_usd)}%` }} />
          </div>
          <div style={{ display: "flex", justifyContent: "space-between", marginTop: 3 }}>
            <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>balance: {budget.balance_usd}</span>
            <span style={{ fontSize: 10, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>available: {budget.available_usd}</span>
          </div>
        </div>

        {editingID === "account" && (
          <div style={{ display: "flex", gap: 10, marginBottom: 10, padding: 10, background: "var(--bg3)", borderRadius: "var(--radius-sm)", border: "1px solid var(--border)" }}>
            <div style={{ flex: 1 }}>
              <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>CREDIT AMOUNT ($)</label>
              <input className="input" type="number" value={editLimit}
                onChange={e => { setEditLimit(e.target.value); void actions.setBudgetAmountUsd(e.target.value); }}
                style={{ fontFamily: "var(--font-mono)" }} />
            </div>
            <div style={{ flex: 1 }}>
              <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>LIMIT ($)</label>
              <input className="input" type="number" value={editWarn}
                onChange={e => { setEditWarn(e.target.value); void actions.setBudgetLimitUsd(e.target.value); }}
                style={{ fontFamily: "var(--font-mono)" }} />
            </div>
          </div>
        )}

        {budget.warnings?.some(w => w.triggered) && (
          <div style={{ fontSize: 12, color: "var(--amber)", marginTop: 6 }}>
            <Icon d={Icons.warning} size={13} /> Warning threshold triggered at {budget.warnings.find(w => w.triggered)?.threshold_percent}%
          </div>
        )}
      </div>

      {/* Model cost estimates */}
      {accountSummary?.estimates && accountSummary.estimates.length > 0 && (
        <>
          <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", marginBottom: 10 }}>Model cost estimates</div>
          <div className="card" style={{ overflow: "hidden" }}>
            <table className="table" style={{ tableLayout: "fixed" }}>
              <colgroup>
                <col /><col style={{ width: 100 }} /><col style={{ width: 140 }} /><col style={{ width: 120 }} />
              </colgroup>
              <thead>
                <tr><th>Model</th><th>Provider</th><th>Est. prompt tokens</th><th>Est. output tokens</th></tr>
              </thead>
              <tbody>
                {accountSummary.estimates.slice(0, 10).map((e, i) => (
                  <tr key={`${e.provider}-${e.model}-${i}`}>
                    <td className="mono" style={{ color: "var(--t0)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{e.model}</td>
                    <td className="mono" style={{ color: "var(--t2)" }}>{e.provider}</td>
                    <td className="mono">{e.estimated_remaining_prompt_tokens?.toLocaleString() ?? "—"}</td>
                    <td className="mono">{e.estimated_remaining_output_tokens?.toLocaleString() ?? "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  );
}

// ─── Usage tab ────────────────────────────────────────────────────────────────

function UsageTab({ state }: { state: Props["state"] }) {
  const ledger = state.requestLedger ?? [];

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 10 }}>
        <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Token usage log</span>
        <span style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>live</span>
        <Dot color="green" />
      </div>
      {ledger.length > 0 ? (
        <div className="card" style={{ overflow: "hidden" }}>
          <table className="table" style={{ tableLayout: "fixed" }}>
            <colgroup>
              <col style={{ width: 80 }} /><col style={{ width: 90 }} /><col /><col style={{ width: 80 }} /><col style={{ width: 70 }} /><col style={{ width: 130 }} /><col style={{ width: 52 }} />
            </colgroup>
            <thead>
              <tr><th>Time</th><th>Tenant</th><th>Model</th><th>Tokens</th><th>Cost</th><th>Request ID</th><th></th></tr>
            </thead>
            <tbody>
              {ledger.slice(0, 100).map(e => (
                <tr key={e.request_id || e.timestamp}>
                  <td className="mono" style={{ color: "var(--t3)" }}>{e.timestamp ? new Date(e.timestamp).toLocaleTimeString() : "—"}</td>
                  <td className="mono" style={{ color: "var(--teal)" }}>{e.tenant || "—"}</td>
                  <td className="mono" style={{ color: "var(--t1)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{e.model || "—"}</td>
                  <td className="mono">{e.total_tokens?.toLocaleString() ?? "—"}</td>
                  <td className="mono" style={{ color: "var(--t0)", fontWeight: 500 }}>{e.amount_usd || "—"}</td>
                  <td className="mono" style={{ color: "var(--t2)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{e.request_id || "—"}</td>
                  <td>{e.request_id && <CopyBtn text={e.request_id} />}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
          No usage events recorded yet.
        </div>
      )}
    </>
  );
}

// ─── Retention tab ────────────────────────────────────────────────────────────

const KNOWN_SUBSYSTEMS = [
  "trace_snapshots",
  "budget_events",
  "audit_events",
  "exact_cache",
  "semantic_cache",
] as const;

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function RetentionTab({ state, actions }: Props) {
  const runs = state.retentionRuns ?? [];
  const lastRun = state.retentionLastRun;

  // Parse CSV state into a local Set for chip toggles
  const selectedSet = new Set(
    state.retentionSubsystems
      .split(",")
      .map(s => s.trim())
      .filter(s => KNOWN_SUBSYSTEMS.includes(s as typeof KNOWN_SUBSYSTEMS[number]))
  );

  function toggleSubsystem(name: string) {
    const next = new Set(selectedSet);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    actions.setRetentionSubsystems([...next].join(","));
  }

  const totalDeleted = lastRun?.results.filter(r => !r.skipped).reduce((n, r) => n + (r.deleted ?? 0), 0) ?? 0;
  const maxDeleted = Math.max(1, ...(lastRun?.results.map(r => r.deleted ?? 0) ?? []));

  return (
    <>
      {/* Controls */}
      <div className="card" style={{ padding: "14px 16px", marginBottom: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
          <span style={{ fontSize: 12, fontWeight: 500, color: "var(--t1)" }}>Subsystems to prune</span>
          <span style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)" }}>
            {selectedSet.size === 0 ? "all" : `${selectedSet.size} selected`}
          </span>
          <button className="btn btn-primary btn-sm" style={{ marginLeft: "auto" }}
            disabled={state.retentionLoading}
            onClick={() => void actions.runRetention()}>
            <Icon d={Icons.refresh} size={13} /> {state.retentionLoading ? "Running…" : "Run now"}
          </button>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
          {KNOWN_SUBSYSTEMS.map(name => {
            const active = selectedSet.has(name);
            return (
              <button key={name} type="button" onClick={() => toggleSubsystem(name)}
                style={{
                  padding: "4px 10px",
                  fontFamily: "var(--font-mono)",
                  fontSize: 11,
                  borderRadius: "var(--radius-sm)",
                  border: `1px solid ${active ? "var(--teal-border)" : "var(--border)"}`,
                  background: active ? "var(--teal-bg)" : "var(--bg3)",
                  color: active ? "var(--teal)" : "var(--t2)",
                  cursor: "pointer",
                  transition: "background 0.1s, color 0.1s, border-color 0.1s",
                }}>
                {name}
              </button>
            );
          })}
        </div>
        <div style={{ fontSize: 10, color: "var(--t3)", marginTop: 8 }}>
          No selection = prune all subsystems
        </div>
        {state.retentionError && <div style={{ marginTop: 8 }}><InlineError message={state.retentionError} /></div>}
      </div>

      {/* Last run summary */}
      {lastRun && (
        <div className="card" style={{ padding: "14px 16px", marginBottom: 16 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 12 }}>
            <span style={{ fontSize: 12, fontWeight: 500, color: "var(--t1)" }}>Last run</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>
              {relativeTime(lastRun.finished_at)}
            </span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>·</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)" }}>{lastRun.trigger}</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>·</span>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: totalDeleted > 0 ? "var(--teal)" : "var(--t3)" }}>
              {totalDeleted} deleted
            </span>
          </div>
          <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            {lastRun.results.map(r => (
              <div key={r.name} style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: r.skipped ? "var(--t3)" : "var(--t1)", width: 140, flexShrink: 0 }}>
                  {r.name}
                </span>
                {r.skipped ? (
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", fontStyle: "italic" }}>skipped</span>
                ) : r.error ? (
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--red)" }}>{r.error}</span>
                ) : (
                  <>
                    <div style={{ flex: 1, height: 4, background: "var(--bg3)", borderRadius: 2, overflow: "hidden" }}>
                      <div style={{
                        height: "100%",
                        width: `${Math.round((r.deleted / maxDeleted) * 100)}%`,
                        background: r.deleted > 0 ? "var(--teal)" : "var(--bg3)",
                        borderRadius: 2,
                      }} />
                    </div>
                    <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: r.deleted > 0 ? "var(--teal)" : "var(--t3)", width: 48, textAlign: "right", flexShrink: 0 }}>
                      {r.deleted} del
                    </span>
                  </>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* History */}
      <div style={{ fontSize: 12, fontWeight: 500, color: "var(--t1)", marginBottom: 8 }}>History</div>
      {runs.length > 0 ? (
        <div className="card" style={{ overflow: "hidden" }}>
          {runs.slice(0, 20).map((r, i) => {
            const del = r.results?.filter(s => !s.skipped).reduce((n, s) => n + (s.deleted ?? 0), 0) ?? 0;
            const errored = r.results?.some(s => s.error);
            return (
              <div key={i} style={{ display: "flex", alignItems: "center", gap: 10, padding: "8px 14px", borderBottom: i < Math.min(runs.length, 20) - 1 ? "1px solid var(--border)" : "none" }}>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t2)", width: 70, flexShrink: 0 }}>{r.trigger}</span>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>{relativeTime(r.finished_at)}</span>
                <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: del > 0 ? "var(--teal)" : "var(--t3)", marginLeft: "auto" }}>
                  {del} deleted
                </span>
                {errored && <Badge status="down" label="error" />}
              </div>
            );
          })}
        </div>
      ) : (
        <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12 }}>
          No retention runs yet.
        </div>
      )}
    </>
  );
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label style={{ fontSize: 11, color: "var(--t2)", display: "block", marginBottom: 4, fontFamily: "var(--font-mono)" }}>{label}</label>
      {children}
    </div>
  );
}

