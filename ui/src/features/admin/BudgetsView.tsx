import { useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import { Badge, CopyBtn, Dot, Icon, Icons } from "../shared/ui";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

export function BudgetsView({ state, actions }: Props) {
  const [editingID, setEditingID] = useState<string | null>(null);
  const [editLimit, setEditLimit] = useState("");
  const [editWarn, setEditWarn] = useState("");

  const budget = state.budget;
  const accountSummary = state.accountSummary;
  const ledger = state.requestLedger ?? [];

  function pct(debited: number, limit: number) {
    if (!limit) return 0;
    return Math.min(100, Math.round((debited / limit) * 100));
  }

  function barClass(p: number, warnThreshold: number): string {
    if (p >= 90) return "progress-red";
    if (p >= warnThreshold) return "progress-amber";
    return "progress-teal";
  }

  return (
    <div style={{ height: "100%", overflowY: "auto", padding: 16 }}>
      {/* Account budget */}
      {budget ? (
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", marginBottom: 10 }}>Account budget</div>
          <div className="card" style={{ padding: "14px 16px" }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10 }}>
              <span style={{ fontFamily: "var(--font-mono)", fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>
                {budget.scope}
              </span>
              {budget.enforced && <Badge status="enabled" label="enforced" />}
              {!budget.enforced && <Badge status="disabled" label="not enforced" />}
              <div style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
                {editingID === "account" ? (
                  <>
                    <button className="btn btn-primary btn-sm" onClick={() => {
                      void actions.setBudgetLimit();
                      setEditingID(null);
                    }}>Save</button>
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

            {/* Balance bar */}
            <div style={{ marginBottom: 10 }}>
              <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 5 }}>
                <span style={{ fontSize: 11, color: "var(--t2)" }}>
                  <span style={{ fontFamily: "var(--font-mono)", color: "var(--t0)", fontWeight: 500 }}>{budget.debited_usd}</span> spent of {budget.credited_usd} credited
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
                  <input className="input" type="number" aria-label="Credit amount in USD" value={editLimit} onChange={e => { setEditLimit(e.target.value); void actions.setBudgetAmountUsd(e.target.value); }} style={{ fontFamily: "var(--font-mono)" }} />
                </div>
                <div style={{ flex: 1 }}>
                  <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>LIMIT ($)</label>
                  <input className="input" type="number" aria-label="Limit in USD" value={editWarn} onChange={e => { setEditWarn(e.target.value); void actions.setBudgetLimitUsd(e.target.value); }} style={{ fontFamily: "var(--font-mono)" }} />
                </div>
              </div>
            )}

            {budget.warnings && budget.warnings.some(w => w.triggered) && (
              <div style={{ fontSize: 12, color: "var(--amber)", marginTop: 6 }}>
                <Icon d={Icons.warning} size={13} /> Warning threshold triggered at {budget.warnings.find(w => w.triggered)?.threshold_percent}%
              </div>
            )}
          </div>
        </div>
      ) : (
        <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12, marginBottom: 20 }}>
          Budget data unavailable. Admin access required.
        </div>
      )}

      {/* Account summary — cost estimates */}
      {accountSummary?.estimates && accountSummary.estimates.length > 0 && (
        <div style={{ marginBottom: 20 }}>
          <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", marginBottom: 10 }}>Model cost estimates</div>
          <div className="card" style={{ overflow: "hidden" }}>
            <table className="table" style={{ tableLayout: "fixed" }}>
              <colgroup>
                <col /><col style={{ width: 100 }} /><col style={{ width: 120 }} /><col style={{ width: 100 }} />
              </colgroup>
              <thead>
                <tr>
                  <th>Model</th>
                  <th>Provider</th>
                  <th>Est. prompt tokens</th>
                  <th>Est. output tokens</th>
                </tr>
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
        </div>
      )}

      {/* Token usage log */}
      <div>
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
                <tr>
                  <th>Time</th><th>Tenant</th><th>Model</th><th>Tokens</th><th>Cost</th><th>Request ID</th><th></th>
                </tr>
              </thead>
              <tbody>
                {ledger.slice(0, 50).map(e => (
                  <tr key={e.request_id || e.timestamp}>
                    <td className="mono" style={{ color: "var(--t3)" }}>
                      {e.timestamp ? new Date(e.timestamp).toLocaleTimeString() : "—"}
                    </td>
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
      </div>

      {/* Retention info */}
      {state.retentionRuns && state.retentionRuns.length > 0 && (
        <div style={{ marginTop: 20 }}>
          <div style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)", marginBottom: 10 }}>Retention</div>
          <div className="card" style={{ padding: "12px 16px" }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 8 }}>
              <span style={{ fontSize: 12, color: "var(--t1)" }}>Last run: {state.retentionLastRun ? new Date(state.retentionLastRun.finished_at).toLocaleString() : "never"}</span>
              <button className="btn btn-sm btn-ghost" style={{ marginLeft: "auto" }}
                disabled={state.retentionLoading}
                onClick={() => void actions.runRetention()}>
                <Icon d={Icons.refresh} size={13} /> {state.retentionLoading ? "Running…" : "Run now"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
