import { KV } from "./KV";
import { Panel } from "./Panel";
import type { BudgetStatusResponse } from "../types/runtime";

type BudgetPanelProps = {
  budget: BudgetStatusResponse["data"] | null;
  budgetActionError: string;
  budgetAmountUsd: string;
  budgetLimitUsd: string;
  inputClassName: string;
  onBudgetAmountChange: (value: string) => void;
  onBudgetLimitChange: (value: string) => void;
  onReset: () => void | Promise<void>;
  onSetLimit: () => void | Promise<void>;
  onTopUp: () => void | Promise<void>;
};

export function BudgetPanel(props: BudgetPanelProps) {
  return (
    <Panel
      eyebrow="Budget"
      title="Current scope"
      actions={
        <button
          className="inline-flex rounded-full border border-slate-200/80 bg-white/75 px-4 py-3 text-sm font-medium text-slate-900 transition hover:-translate-y-0.5"
          onClick={() => void props.onReset()}
          type="button"
        >
          Reset
        </button>
      }
    >
      {props.budget ? (
        <>
          <div className="mt-4 grid gap-3 md:grid-cols-3">
            <BudgetHeroCard label="Remaining" value={props.budget.remaining_usd} tone="emerald" />
            <BudgetHeroCard label="Spent" value={props.budget.spent_usd} tone="amber" />
            <BudgetHeroCard label="Max" value={props.budget.max_usd} tone="slate" />
          </div>

          <div className="mt-4 rounded-2xl bg-slate-50/90 p-4">
            <p className="text-xs font-semibold uppercase tracking-[0.16em] text-slate-500">Scope summary</p>
            <p className="mt-2 text-lg font-semibold text-slate-900">
              {props.budget.scope}
              {props.budget.tenant ? ` · tenant ${props.budget.tenant}` : ""}
              {props.budget.provider ? ` · provider ${props.budget.provider}` : ""}
            </p>
            <p className="mt-1 text-sm text-slate-600">
              Backend: {props.budget.backend} · Limit source: {props.budget.limit_source}
            </p>
          </div>

          <dl className="mt-4">
            <KV label="Key" value={props.budget.key} />
            <KV label="Scope" value={props.budget.scope} />
            <KV label="Tenant" value={props.budget.tenant} />
            <KV label="Provider" value={props.budget.provider} />
            <KV label="Backend" value={props.budget.backend} />
            <KV label="Limit Source" value={props.budget.limit_source} />
          </dl>
        </>
      ) : (
        <div className="mt-4 rounded-2xl border border-slate-200/80 bg-slate-50/90 px-4 py-4 text-sm text-slate-600">
          Budget details will appear here for admin sessions with budget access.
        </div>
      )}

      <div className="mt-5 grid gap-3">
        <label>
          <span className="mb-2 block text-sm text-slate-600">Top-up amount (USD)</span>
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              className={props.inputClassName}
              value={props.budgetAmountUsd}
              onChange={(event) => props.onBudgetAmountChange(event.target.value)}
            />
            <button
              className="inline-flex shrink-0 rounded-full bg-slate-900 px-4 py-3 text-sm font-semibold text-white transition hover:-translate-y-0.5"
              onClick={() => void props.onTopUp()}
              type="button"
            >
              Top up
            </button>
          </div>
        </label>

        <label>
          <span className="mb-2 block text-sm text-slate-600">Set absolute limit (USD)</span>
          <div className="flex flex-col gap-2 sm:flex-row">
            <input
              className={props.inputClassName}
              value={props.budgetLimitUsd}
              onChange={(event) => props.onBudgetLimitChange(event.target.value)}
            />
            <button
              className="inline-flex shrink-0 rounded-full border border-slate-200/80 bg-white/90 px-4 py-3 text-sm font-semibold text-slate-900 transition hover:-translate-y-0.5"
              onClick={() => void props.onSetLimit()}
              type="button"
            >
              Set limit
            </button>
          </div>
        </label>

        {props.budgetActionError ? (
          <div className="rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
            {props.budgetActionError}
          </div>
        ) : null}
      </div>
    </Panel>
  );
}

function BudgetHeroCard(props: { label: string; tone: "emerald" | "amber" | "slate"; value: string }) {
  const toneClass =
    props.tone === "emerald"
      ? "border-emerald-200 bg-emerald-50 text-emerald-950"
      : props.tone === "amber"
        ? "border-amber-200 bg-amber-50 text-amber-950"
        : "border-slate-200 bg-white text-slate-950";

  return (
    <div className={`rounded-2xl border px-4 py-4 ${toneClass}`}>
      <p className="text-xs font-semibold uppercase tracking-[0.16em] opacity-75">{props.label}</p>
      <p className="mt-2 text-2xl font-semibold">{props.value}</p>
    </div>
  );
}
