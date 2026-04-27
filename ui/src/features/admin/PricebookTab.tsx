import { useEffect, useMemo, useState } from "react";
import type { RuntimeConsoleViewModel } from "../../app/useRuntimeConsole";
import type {
  ConfiguredPricebookRecord,
  PricebookImportDiff,
} from "../../types/runtime";
import { Badge, Icon, Icons, InlineError } from "../shared/ui";

type Props = {
  state: RuntimeConsoleViewModel["state"];
  actions: RuntimeConsoleViewModel["actions"];
};

// formatPricePerMillion converts micros-USD-per-Mtok to a human "$0.150 / 1M"
// label. The display unit is dollars-per-million-tokens, with 3 decimals so
// sub-cent-per-Mtok prices (e.g. some Groq cached reads) don't collapse to "$0".
export function formatPricePerMillion(micros: number): string {
  if (!Number.isFinite(micros) || micros <= 0) return "—";
  const dollars = micros / 1_000_000;
  return `$${dollars.toFixed(3)} / 1M`;
}

// dollarsToMicros parses a dollar string from the form input back to micros.
// Accepts "0.15", "$0.15", " 0.15 / 1M ", etc. Returns null on invalid input
// so the caller can surface a validation error instead of silently writing 0.
//
// Implementation note: we extract the first decimal-looking number with a
// regex rather than stripping non-digit chars, so "0.15 / 1M" matches "0.15"
// and not "0.151" (which would happen if we naively strip the slash).
export function dollarsToMicros(input: string): number | null {
  const trimmed = input.trim();
  if (trimmed === "") return null;
  const match = trimmed.match(/-?\d+(?:\.\d+)?/);
  if (!match) return null;
  const n = Number(match[0]);
  if (!Number.isFinite(n) || n < 0) return null;
  return Math.round(n * 1_000_000);
}

function pricebookKey(provider: string, model: string): string {
  return `${provider}/${model}`;
}

export function PricebookTab({ state, actions }: Props) {
  const rows = state.adminConfig?.pricebook ?? [];
  const [importOpen, setImportOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<string | null>(null);

  // Provider list: built-in presets first, then anything else discovered
  // through the runtime providers list — so admins can price providers
  // they've added beyond the bundled set.
  const providerOptions = useMemo(() => {
    const set = new Set<string>();
    for (const p of state.providerPresets ?? []) set.add(p.id);
    for (const p of state.adminConfig?.providers ?? []) set.add(p.id);
    return [...set].sort();
  }, [state.providerPresets, state.adminConfig?.providers]);

  return (
    <>
      <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 12 }}>
        <span style={{ fontSize: 13, fontWeight: 500, color: "var(--t0)" }}>Pricebook</span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>{rows.length} entries</span>
        <button
          className="btn btn-primary btn-sm"
          style={{ marginLeft: "auto" }}
          onClick={() => setImportOpen(true)}>
          <Icon d={Icons.refresh} size={13} /> Import latest from LiteLLM
        </button>
      </div>

      {state.adminConfigError && (
        <div style={{ marginBottom: 8 }}>
          <InlineError message={state.adminConfigError} />
        </div>
      )}

      {rows.length > 0 ? (
        <div className="card" style={{ overflow: "hidden", marginBottom: 16 }}>
          <table className="table">
            <thead>
              <tr>
                <th>Provider</th>
                <th>Model</th>
                <th>Input ($/1M)</th>
                <th>Output ($/1M)</th>
                <th>Cached ($/1M)</th>
                <th>Source</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {rows.map(row => {
                const key = pricebookKey(row.provider, row.model);
                const editing = editingKey === key;
                return editing ? (
                  <PricebookEditRow
                    key={key}
                    row={row}
                    onCancel={() => setEditingKey(null)}
                    onSave={async patch => {
                      await actions.upsertPricebookEntry(patch);
                      setEditingKey(null);
                    }}
                  />
                ) : (
                  <PricebookViewRow
                    key={key}
                    row={row}
                    onEdit={() => setEditingKey(key)}
                    onDelete={() => void actions.deletePricebookEntry(row.provider, row.model)}
                  />
                );
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="card" style={{ padding: "24px", textAlign: "center", color: "var(--t3)", fontSize: 12, marginBottom: 16 }}>
          No pricebook entries. Add one below or import from LiteLLM.
        </div>
      )}

      <PricebookAddForm providerOptions={providerOptions} onSubmit={async entry => actions.upsertPricebookEntry(entry)} />

      {importOpen && (
        <PricebookImportModal
          onClose={() => setImportOpen(false)}
          loadDiff={() => actions.previewPricebookImport()}
          applyDiff={keys => actions.applyPricebookImport(keys)}
        />
      )}
    </>
  );
}

// ─── View row ────────────────────────────────────────────────────────────────

function PricebookViewRow({
  row,
  onEdit,
  onDelete,
}: {
  row: ConfiguredPricebookRecord;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const sourceLabel = row.source === "imported" ? "imported" : "manual";
  return (
    <tr>
      <td className="mono" style={{ color: "var(--t1)" }}>{row.provider}</td>
      <td className="mono" style={{ color: "var(--t0)" }}>{row.model}</td>
      <td className="mono">{formatPricePerMillion(row.input_micros_usd_per_million_tokens)}</td>
      <td className="mono">{formatPricePerMillion(row.output_micros_usd_per_million_tokens)}</td>
      <td className="mono">{formatPricePerMillion(row.cached_input_micros_usd_per_million_tokens)}</td>
      <td>
        <Badge status={sourceLabel === "imported" ? "enabled" : "healthy"} label={sourceLabel} />
      </td>
      <td>
        <div style={{ display: "flex", gap: 4 }}>
          <button className="btn btn-ghost btn-sm" style={{ padding: "3px 6px" }} onClick={onEdit} aria-label={`Edit ${row.provider}/${row.model}`}>
            <Icon d={Icons.edit} size={12} />
          </button>
          <button
            className="btn btn-ghost btn-sm"
            style={{ color: "var(--red)", padding: "3px 6px" }}
            onClick={onDelete}
            aria-label={`Delete ${row.provider}/${row.model}`}>
            <Icon d={Icons.trash} size={13} />
          </button>
        </div>
      </td>
    </tr>
  );
}

// ─── Edit row ────────────────────────────────────────────────────────────────

function PricebookEditRow({
  row,
  onCancel,
  onSave,
}: {
  row: ConfiguredPricebookRecord;
  onCancel: () => void;
  onSave: (patch: ConfiguredPricebookRecord) => Promise<void>;
}) {
  const [input, setInput] = useState((row.input_micros_usd_per_million_tokens / 1_000_000).toFixed(3));
  const [output, setOutput] = useState((row.output_micros_usd_per_million_tokens / 1_000_000).toFixed(3));
  const [cached, setCached] = useState((row.cached_input_micros_usd_per_million_tokens / 1_000_000).toFixed(3));
  const [error, setError] = useState("");

  async function save() {
    const inputMicros = dollarsToMicros(input);
    const outputMicros = dollarsToMicros(output);
    const cachedMicros = dollarsToMicros(cached);
    if (inputMicros === null || outputMicros === null || cachedMicros === null) {
      setError("Prices must be non-negative numbers.");
      return;
    }
    setError("");
    // Edits always promote to manual — operator intent overrides the import.
    await onSave({
      provider: row.provider,
      model: row.model,
      input_micros_usd_per_million_tokens: inputMicros,
      output_micros_usd_per_million_tokens: outputMicros,
      cached_input_micros_usd_per_million_tokens: cachedMicros,
      source: "manual",
    });
  }

  return (
    <tr>
      <td className="mono" style={{ color: "var(--t1)" }}>{row.provider}</td>
      <td className="mono" style={{ color: "var(--t0)" }}>{row.model}</td>
      <td><input className="input" style={{ fontFamily: "var(--font-mono)", width: 80 }} value={input} onChange={e => setInput(e.target.value)} /></td>
      <td><input className="input" style={{ fontFamily: "var(--font-mono)", width: 80 }} value={output} onChange={e => setOutput(e.target.value)} /></td>
      <td><input className="input" style={{ fontFamily: "var(--font-mono)", width: 80 }} value={cached} onChange={e => setCached(e.target.value)} /></td>
      <td>
        <Badge status="healthy" label="manual" />
      </td>
      <td>
        <div style={{ display: "flex", gap: 4 }}>
          <button className="btn btn-primary btn-sm" style={{ padding: "3px 6px" }} onClick={() => void save()}>Save</button>
          <button className="btn btn-ghost btn-sm" style={{ padding: "3px 6px" }} onClick={onCancel}>Cancel</button>
        </div>
        {error && <div style={{ fontSize: 10, color: "var(--red)", marginTop: 3 }}>{error}</div>}
      </td>
    </tr>
  );
}

// ─── Add form ────────────────────────────────────────────────────────────────

function PricebookAddForm({
  providerOptions,
  onSubmit,
}: {
  providerOptions: string[];
  onSubmit: (entry: ConfiguredPricebookRecord) => Promise<void>;
}) {
  const [provider, setProvider] = useState(providerOptions[0] ?? "");
  const [model, setModel] = useState("");
  const [input, setInput] = useState("");
  const [output, setOutput] = useState("");
  const [cached, setCached] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  async function submit() {
    if (!provider || !model.trim()) {
      setError("Provider and model are required.");
      return;
    }
    const inputMicros = dollarsToMicros(input || "0");
    const outputMicros = dollarsToMicros(output || "0");
    const cachedMicros = dollarsToMicros(cached || "0");
    if (inputMicros === null || outputMicros === null || cachedMicros === null) {
      setError("Prices must be non-negative numbers.");
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      await onSubmit({
        provider,
        model: model.trim(),
        input_micros_usd_per_million_tokens: inputMicros,
        output_micros_usd_per_million_tokens: outputMicros,
        cached_input_micros_usd_per_million_tokens: cachedMicros,
        source: "manual",
      });
      setModel("");
      setInput("");
      setOutput("");
      setCached("");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="card" style={{ padding: "12px 14px" }}>
      <div style={{ fontSize: 12, fontWeight: 500, color: "var(--t1)", marginBottom: 10 }}>Add entry</div>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-end" }}>
        <div style={{ minWidth: 140 }}>
          <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>PROVIDER</label>
          <select className="select" style={{ width: "100%" }} value={provider} onChange={e => setProvider(e.target.value)}>
            {providerOptions.map(p => <option key={p} value={p}>{p}</option>)}
          </select>
        </div>
        <div style={{ flex: 2, minWidth: 180 }}>
          <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>MODEL</label>
          <input className="input" style={{ fontFamily: "var(--font-mono)" }} placeholder="e.g. gpt-4o-mini" value={model} onChange={e => setModel(e.target.value)} />
        </div>
        <div style={{ width: 110 }}>
          <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>INPUT $/1M</label>
          <input className="input" style={{ fontFamily: "var(--font-mono)" }} placeholder="0.150" value={input} onChange={e => setInput(e.target.value)} />
        </div>
        <div style={{ width: 110 }}>
          <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>OUTPUT $/1M</label>
          <input className="input" style={{ fontFamily: "var(--font-mono)" }} placeholder="0.600" value={output} onChange={e => setOutput(e.target.value)} />
        </div>
        <div style={{ width: 110 }}>
          <label style={{ fontSize: 10, color: "var(--t3)", display: "block", marginBottom: 3, fontFamily: "var(--font-mono)" }}>CACHED $/1M</label>
          <input className="input" style={{ fontFamily: "var(--font-mono)" }} placeholder="0.075" value={cached} onChange={e => setCached(e.target.value)} />
        </div>
        <button className="btn btn-primary btn-sm" disabled={submitting} onClick={() => void submit()}>
          <Icon d={Icons.plus} size={13} /> Add
        </button>
      </div>
      {error && <div style={{ marginTop: 8 }}><InlineError message={error} /></div>}
    </div>
  );
}

// ─── Import modal ────────────────────────────────────────────────────────────

function PricebookImportModal({
  onClose,
  loadDiff,
  applyDiff,
}: {
  onClose: () => void;
  loadDiff: () => Promise<PricebookImportDiff>;
  applyDiff: (keys: string[]) => Promise<PricebookImportDiff>;
}) {
  const [diff, setDiff] = useState<PricebookImportDiff | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [applying, setApplying] = useState(false);

  // Load the diff once on mount. We deliberately don't include loadDiff in
  // the dep array — the parent recreates the closure every render, which
  // would re-fire the network call on every parent re-render.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    (async () => {
      try {
        const result = await loadDiff();
        if (cancelled) return;
        setDiff(result);
        const initial = new Set<string>();
        for (const r of result.added ?? []) initial.add(pricebookKey(r.provider, r.model));
        for (const u of result.updated ?? []) initial.add(pricebookKey(u.entry.provider, u.entry.model));
        setSelected(initial);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : "Failed to load preview.");
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function toggle(key: string) {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function retry() {
    setError("");
    setLoading(true);
    try {
      const result = await loadDiff();
      setDiff(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load preview.");
    } finally {
      setLoading(false);
    }
  }

  async function apply() {
    if (!diff) return;
    setApplying(true);
    try {
      await applyDiff([...selected]);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to apply import.");
    } finally {
      setApplying(false);
    }
  }

  const added = diff?.added ?? [];
  const updated = diff?.updated ?? [];
  const skipped = diff?.skipped ?? [];

  return (
    <div
      style={{ position: "fixed", inset: 0, zIndex: 50, display: "flex", alignItems: "center", justifyContent: "center", background: "oklch(0 0 0 / 0.5)" }}
      onClick={onClose}>
      <div
        role="dialog"
        aria-label="Import LiteLLM pricing"
        style={{ width: 560, maxHeight: "80vh", overflow: "hidden", background: "var(--bg1)", border: "1px solid var(--border)", borderRadius: "var(--radius)", display: "flex", flexDirection: "column" }}
        onClick={e => e.stopPropagation()}>
        <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)", display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ fontWeight: 500, fontSize: 13 }}>Import LiteLLM pricing</span>
          <button className="btn btn-ghost btn-sm" style={{ marginLeft: "auto", padding: "3px 6px" }} onClick={onClose} aria-label="Close">
            <Icon d={Icons.x} size={14} />
          </button>
        </div>
        <div style={{ padding: 16, flex: 1, overflowY: "auto" }}>
          {loading && <div style={{ color: "var(--t2)", fontSize: 12 }}>Loading preview…</div>}
          {error && (
            <div>
              <InlineError message={error} />
              <button className="btn btn-sm" style={{ marginTop: 8 }} onClick={() => void retry()}>Retry</button>
            </div>
          )}
          {diff && !error && (
            <>
              <ImportSection title="Added" count={added.length}>
                {added.map(r => (
                  <ImportRow
                    key={pricebookKey(r.provider, r.model)}
                    label={`${r.provider}/${r.model}`}
                    detail={`${formatPricePerMillion(r.input_micros_usd_per_million_tokens)} → ${formatPricePerMillion(r.output_micros_usd_per_million_tokens)}`}
                    checked={selected.has(pricebookKey(r.provider, r.model))}
                    onToggle={() => toggle(pricebookKey(r.provider, r.model))}
                  />
                ))}
              </ImportSection>

              <ImportSection title="Updated" count={updated.length}>
                {updated.map(u => (
                  <ImportRow
                    key={pricebookKey(u.entry.provider, u.entry.model)}
                    label={`${u.entry.provider}/${u.entry.model}`}
                    detail={`${formatPricePerMillion(u.previous.input_micros_usd_per_million_tokens)} → ${formatPricePerMillion(u.entry.input_micros_usd_per_million_tokens)} input`}
                    checked={selected.has(pricebookKey(u.entry.provider, u.entry.model))}
                    onToggle={() => toggle(pricebookKey(u.entry.provider, u.entry.model))}
                  />
                ))}
              </ImportSection>

              <ImportSection title="Skipped (manual)" count={skipped.length} hint="Manual rows are protected from import. Delete them first if you want LiteLLM's price.">
                {skipped.map(r => (
                  <div key={pricebookKey(r.provider, r.model)} style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)", padding: "4px 0" }}>
                    {r.provider}/{r.model}
                  </div>
                ))}
              </ImportSection>

              {diff.unchanged > 0 && (
                <div style={{ fontSize: 11, color: "var(--t3)", fontFamily: "var(--font-mono)", marginTop: 12 }}>
                  {diff.unchanged} unchanged
                </div>
              )}
            </>
          )}
        </div>
        <div style={{ padding: "12px 16px", borderTop: "1px solid var(--border)", display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button className="btn" onClick={onClose}>Cancel</button>
          <button
            className="btn btn-primary"
            disabled={!diff || applying || selected.size === 0}
            onClick={() => void apply()}>
            {applying ? "Applying…" : `Apply selected (${selected.size})`}
          </button>
        </div>
      </div>
    </div>
  );
}

function ImportSection({ title, count, hint, children }: { title: string; count: number; hint?: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div style={{ display: "flex", alignItems: "baseline", gap: 6, marginBottom: 6 }}>
        <span style={{ fontSize: 12, fontWeight: 500, color: "var(--t1)" }}>{title}</span>
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t3)" }}>({count})</span>
      </div>
      {hint && <div style={{ fontSize: 10, color: "var(--t3)", marginBottom: 6 }}>{hint}</div>}
      {count === 0 ? (
        <div style={{ fontSize: 11, color: "var(--t3)", fontStyle: "italic", padding: "4px 0" }}>none</div>
      ) : (
        <div>{children}</div>
      )}
    </div>
  );
}

function ImportRow({
  label,
  detail,
  checked,
  onToggle,
}: {
  label: string;
  detail: string;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <label style={{ display: "flex", alignItems: "center", gap: 8, padding: "4px 0", cursor: "pointer" }}>
      <input type="checkbox" checked={checked} onChange={onToggle} aria-label={label} />
      <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--t1)" }}>{label}</span>
      <span style={{ fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--t3)", marginLeft: "auto" }}>{detail}</span>
    </label>
  );
}

// Re-exported for tests so they can verify modal behavior in isolation.
export { PricebookImportModal };
