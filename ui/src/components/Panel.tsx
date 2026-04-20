import type { PropsWithChildren, ReactNode } from "react";

type PanelProps = PropsWithChildren<{
  eyebrow: string;
  title: string;
  actions?: ReactNode;
  className?: string;
}>;

export function Panel({ eyebrow, title, actions, className = "", children }: PanelProps) {
  return (
    <section className={`rounded-[24px] border border-slate-200/70 bg-white/75 p-5 shadow-[0_18px_45px_rgba(41,67,84,0.08)] backdrop-blur ${className}`.trim()}>
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.22em] text-amber-700">{eyebrow}</p>
          <h2 className="font-serif text-3xl">{title}</h2>
        </div>
        {actions}
      </div>
      {children}
    </section>
  );
}
