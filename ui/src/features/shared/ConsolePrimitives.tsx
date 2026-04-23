import { useState } from "react";
import type { PropsWithChildren, ReactNode } from "react";

function joinClasses(...values: Array<string | false | null | undefined>): string {
  return values.filter(Boolean).join(" ");
}

export function ShellSection({
  eyebrow,
  title,
  description,
  actions,
  children,
}: PropsWithChildren<{
  eyebrow?: string;
  title: string;
  description?: string;
  actions?: ReactNode;
}>) {
  return (
    <section className="console-section">
      <header className="console-section__header">
        <div className="console-section__copy">
          {eyebrow ? <p className="console-eyebrow">{eyebrow}</p> : null}
          <h2 className="console-section__title">{title}</h2>
          {description ? <p className="console-section__description">{description}</p> : null}
        </div>
        {actions ? <div className="console-section__actions">{actions}</div> : null}
      </header>
      {children}
    </section>
  );
}

export function Surface({
  tone = "default",
  children,
  className,
}: PropsWithChildren<{
  tone?: "default" | "strong" | "danger";
  className?: string;
}>) {
  return <div className={joinClasses("console-surface", `console-surface--${tone}`, className)}>{children}</div>;
}

export function MetricTile({
  label,
  value,
  detail,
  tone = "neutral",
}: {
  label: string;
  value: string;
  detail?: string;
  tone?: "neutral" | "healthy" | "warning";
}) {
  return (
    <div className={joinClasses("metric-tile", `metric-tile--${tone}`)}>
      <p className="metric-tile__label">{label}</p>
      <p className="metric-tile__value">{value}</p>
      {detail ? <p className="metric-tile__detail">{detail}</p> : null}
    </div>
  );
}

export function StatusPill({
  label,
  tone = "neutral",
}: {
  label: string;
  tone?: "neutral" | "healthy" | "warning" | "danger";
}) {
  return <span className={joinClasses("status-pill", `status-pill--${tone}`)}>{label}</span>;
}

export function DefinitionList({
  items,
  compact = false,
}: {
  items: Array<{ label: string; value: ReactNode }>;
  compact?: boolean;
}) {
  return (
    <dl className={joinClasses("definition-list", compact && "definition-list--compact")}>
      {items.map((item) => (
        <div className="definition-list__row" key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function EmptyState({
  title,
  detail,
}: {
  title: string;
  detail: string;
}) {
  return (
    <div className="empty-state">
      <p className="empty-state__title">{title}</p>
      <p className="empty-state__detail">{detail}</p>
    </div>
  );
}

export function InlineNotice({
  tone,
  message,
}: {
  tone: "success" | "error";
  message: string;
}) {
  return <div className={joinClasses("inline-notice", `inline-notice--${tone}`)}>{message}</div>;
}

export function ToolbarButton({
  children,
  tone = "default",
  className,
  ...props
}: PropsWithChildren<{
  tone?: "default" | "primary" | "danger";
  className?: string;
} & React.ButtonHTMLAttributes<HTMLButtonElement>>) {
  return (
    <button
      className={joinClasses("toolbar-button", `toolbar-button--${tone}`, className)}
      type={props.type ?? "button"}
      {...props}
    >
      {children}
    </button>
  );
}

export function TextField({
  label,
  value,
  onChange,
  placeholder,
  type = "text",
  className,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  type?: string;
  className?: string;
}) {
  return (
    <label className={joinClasses("field", className)}>
      <span className="field__label">{label}</span>
      <input className="field__input" onChange={(event) => onChange(event.target.value)} placeholder={placeholder} type={type} value={value} />
    </label>
  );
}

export function TextAreaField({
  label,
  value,
  onChange,
  placeholder,
  rows = 5,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  rows?: number;
}) {
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      <textarea className="field__input field__input--textarea" onChange={(event) => onChange(event.target.value)} placeholder={placeholder} rows={rows} value={value} />
    </label>
  );
}

export function TokenField({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  const [visible, setVisible] = useState(false);
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      <div className="field__token-row">
        <input
          autoComplete="off"
          className="field__input field__input--token"
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder ?? "Paste bearer token"}
          spellCheck={false}
          type={visible ? "text" : "password"}
          value={value}
        />
        <button
          aria-label={visible ? "Hide token" : "Show token"}
          className="field__token-toggle"
          onClick={() => setVisible((v) => !v)}
          type="button"
        >
          {visible ? "Hide" : "Show"}
        </button>
      </div>
    </label>
  );
}

export function SelectField({
  label,
  value,
  onChange,
  children,
  disabled,
}: PropsWithChildren<{
  label: string;
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}>) {
  return (
    <label className="field">
      <span className="field__label">{label}</span>
      <select className="field__input" disabled={disabled} onChange={(event) => onChange(event.target.value)} value={value}>
        {children}
      </select>
    </label>
  );
}
