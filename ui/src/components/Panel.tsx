import type { PropsWithChildren, ReactNode } from "react";

type PanelProps = PropsWithChildren<{
  eyebrow: string;
  title: string;
  actions?: ReactNode;
  className?: string;
}>;

export function Panel({ eyebrow, title, actions, className = "", children }: PanelProps) {
  return (
    <section className={`console-surface panel${className ? ` ${className}` : ""}`}>
      <div className="panel__header">
        <div>
          <p className="console-eyebrow">{eyebrow}</p>
          <h2 className="console-section__title">{title}</h2>
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}
