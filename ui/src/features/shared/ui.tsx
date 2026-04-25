import { useEffect, useRef, useState } from "react";
import type { ModelRecord } from "../../types/runtime";

// ─── Icon ────────────────────────────────────────────────────────────────────

type IconProps = { d: string | string[]; size?: number; strokeWidth?: number; fill?: string };
export function Icon({ d, size = 16, strokeWidth = 1.5, fill = "none" }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill={fill}
      stroke="currentColor" strokeWidth={strokeWidth} strokeLinecap="round" strokeLinejoin="round"
      style={{ flexShrink: 0 }}>
      {Array.isArray(d) ? d.map((p, i) => <path key={i} d={p} />) : <path d={d} />}
    </svg>
  );
}

export const Icons = {
  chat:     "M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z",
  tasks:    "M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4",
  providers:["M5 12h14","M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2","M9 10h.01","M9 16h.01"],
  budgets:  "M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z",
  keys:     "M15 7a2 2 0 012 2m4 0a6 6 0 01-7.743 5.743L11 17H9v2H7v2H4a1 1 0 01-1-1v-2.586a1 1 0 01.293-.707l5.964-5.964A6 6 0 1121 9z",
  observe:  "M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z",
  settings: ["M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z","M15 12a3 3 0 11-6 0 3 3 0 016 0z"],
  chevL:    "M15 19l-7-7 7-7",
  chevR:    "M9 5l7 7-7 7",
  chevD:    "M19 9l-7 7-7-7",
  plus:     "M12 4v16m8-8H4",
  copy:     "M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z",
  check:    "M5 13l4 4L19 7",
  x:        "M6 18L18 6M6 6l12 12",
  refresh:  "M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15",
  terminal: "M8 9l3 3-3 3m5 0h3M5 20h14a2 2 0 002-2V6a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z",
  send:     "M12 19l9 2-9-18-9 18 9-2zm0 0v-8",
  edit:     "M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z",
  trash:    "M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16",
  warning:  "M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z",
  info:     "M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z",
  activity: "M22 12h-4l-3 9L9 3l-3 9H2",
  approve:  "M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z",
  deny:     "M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z",
  retry:    "M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15",
  model:    "M21 16V8a2 2 0 00-1-1.73l-7-4a2 2 0 00-2 0l-7 4A2 2 0 003 8v8a2 2 0 001 1.73l7 4a2 2 0 002 0l7-4A2 2 0 0021 16z",
  branch:   ["M6 3v12","M18 9a3 3 0 100-6 3 3 0 000 6z","M6 21a3 3 0 100-6 3 3 0 000 6z","M18 9a9 9 0 01-9 9"],
  eye:      ["M15 12a3 3 0 11-6 0 3 3 0 016 0z","M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"],
  search:   "M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z",
};

// ─── Badge ───────────────────────────────────────────────────────────────────

type BadgeStatus = "queued" | "running" | "awaiting" | "done" | "failed" | "enabled" | "disabled" | "healthy" | "degraded" | "down" | "ok" | "warn" | "error";
export function Badge({ status, label }: { status: BadgeStatus | string; label?: string }) {
  const map: Record<string, { cls: string; text: string }> = {
    queued:           { text: label || "queued",   cls: "badge-muted"  },
    running:          { text: label || "running",  cls: "badge-teal"   },
    awaiting:         { text: label || "approval", cls: "badge-amber"  },
    awaiting_approval:{ text: label || "approval", cls: "badge-amber"  },
    done:             { text: label || "done",     cls: "badge-green"  },
    completed:        { text: label || "done",     cls: "badge-green"  },
    failed:           { text: label || "failed",   cls: "badge-red"    },
    cancelled:        { text: label || "failed",   cls: "badge-red"    },
    enabled:          { text: label || "enabled",  cls: "badge-green"  },
    disabled:         { text: label || "disabled", cls: "badge-muted"  },
    healthy:          { text: label || "healthy",  cls: "badge-green"  },
    degraded:         { text: label || "degraded", cls: "badge-amber"  },
    down:             { text: label || "down",     cls: "badge-red"    },
    ok:               { text: label || "ok",       cls: "badge-green"  },
    warn:             { text: label || "warn",     cls: "badge-amber"  },
    error:            { text: label || "error",    cls: "badge-red"    },
  };
  const { text, cls } = map[status] ?? { text: label || status, cls: "badge-muted" };
  return <span className={`badge ${cls}`}>{text}</span>;
}

// ─── Dot ─────────────────────────────────────────────────────────────────────

export function Dot({ color = "green", pulse = false }: { color?: "green" | "amber" | "red" | "muted"; pulse?: boolean }) {
  const cls = { green: "dot-green", amber: "dot-amber", red: "dot-red", muted: "dot-muted" }[color];
  return <span className={`dot ${cls}`} style={pulse ? { animation: "dot-pulse 2s infinite" } : {}} />;
}

// ─── Toggle ──────────────────────────────────────────────────────────────────

export function Toggle({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label?: string }) {
  return (
    <label className="toggle-wrap" onClick={() => onChange(!on)}>
      <div className={`toggle ${on ? "on" : ""}`} />
      {label && <span style={{ fontSize: 12, color: "var(--t1)" }}>{label}</span>}
    </label>
  );
}

// ─── CopyBtn ─────────────────────────────────────────────────────────────────

export function CopyBtn({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard?.writeText(text).catch(() => {});
    setCopied(true);
    setTimeout(() => setCopied(false), 1800);
  };
  return (
    <button className="btn btn-ghost btn-sm" onClick={copy} style={{ gap: 4, padding: "3px 6px" }}>
      <Icon d={copied ? Icons.check : Icons.copy} size={12} />
      {copied ? "copied" : "copy"}
    </button>
  );
}

// ─── CodeBlock ───────────────────────────────────────────────────────────────

export function InlineError({ message }: { message: string }) {
  if (!message) return null;
  return (
    <div style={{
      display: "flex", alignItems: "flex-start", gap: 8,
      padding: "7px 10px", borderRadius: "var(--radius-sm)",
      background: "var(--red-bg)", border: "1px solid var(--red-border)",
      color: "var(--red)", fontSize: 12, fontFamily: "var(--font-mono)", lineHeight: 1.4,
    }}>
      <span style={{ flexShrink: 0, marginTop: 1 }}>✕</span>
      <span>{message}</span>
    </div>
  );
}

export function CodeBlock({ code, lang = "bash" }: { code: string; lang?: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    navigator.clipboard?.writeText(code).catch(() => {});
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  return (
    <div className="code-block">
      <div className="code-block-header">
        <span className="code-lang">{lang}</span>
        <button className="code-copy-btn" onClick={copy}>
          <Icon d={copied ? Icons.check : Icons.copy} size={12} />
          {copied ? "copied" : "copy"}
        </button>
      </div>
      <pre className="code-pre"><code>{code}</code></pre>
    </div>
  );
}

// ─── ModelPicker ─────────────────────────────────────────────────────────────

function groupModelRecords(models: ModelRecord[]): Array<{ provider: string; models: ModelRecord[] }> {
  const map = new Map<string, ModelRecord[]>();
  for (const m of models) {
    const provider = m.metadata?.provider ?? m.owned_by ?? "unknown";
    if (!map.has(provider)) map.set(provider, []);
    map.get(provider)!.push(m);
  }
  return Array.from(map.entries()).map(([provider, models]) => ({ provider, models }));
}

export function ModelPicker({ value, onChange, models }: {
  value: string;
  onChange: (v: string) => void;
  models: ModelRecord[];
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const groups = groupModelRecords(models);
  const selectedLabel = value || (models[0]?.id ?? "auto");

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  return (
    <div className="dropdown-wrap" ref={ref}>
      <button className="btn btn-ghost btn-sm" onClick={() => setOpen(o => !o)}
        style={{ fontFamily: "var(--font-mono)", fontSize: 11, gap: 5, color: "var(--t1)" }}>
        <Icon d={Icons.model} size={13} />
        {selectedLabel}
        <Icon d={Icons.chevD} size={11} />
      </button>
      {open && (
        <div className="dropdown-menu" style={{ minWidth: 280, maxHeight: 360, overflowY: "auto" }}>
          {groups.length === 0 && (
            <div style={{ padding: "10px 12px", fontSize: 12, color: "var(--t3)" }}>No models available</div>
          )}
          {groups.map(group => (
            <div key={group.provider}>
              <div className="dropdown-section-label">{group.provider}</div>
              {group.models.map(m => (
                <div key={m.id} className={`dropdown-item ${m.id === value ? "selected" : ""}`}
                  onClick={() => { onChange(m.id); setOpen(false); }}>
                  <span style={{ flex: 1, fontFamily: "var(--font-mono)", fontSize: 12, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{m.id}</span>
                  {m.metadata?.default && <span style={{ fontSize: 9, color: "var(--teal)", fontFamily: "var(--font-mono)", marginLeft: 6 }}>default</span>}
                  {m.id === value && <Icon d={Icons.check} size={12} />}
                </div>
              ))}
              <div className="dropdown-divider" />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
