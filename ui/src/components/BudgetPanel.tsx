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
      <dl className="mt-4">
        <KV label="Key" value={props.budget?.key} />
        <KV label="Scope" value={props.budget?.scope} />
        <KV label="Tenant" value={props.budget?.tenant} />
        <KV label="Provider" value={props.budget?.provider} />
        <KV label="Backend" value={props.budget?.backend} />
        <KV label="Limit Source" value={props.budget?.limit_source} />
        <KV label="Spent" value={props.budget?.spent_usd} />
        <KV label="Max" value={props.budget?.max_usd} />
        <KV label="Remaining" value={props.budget?.remaining_usd} />
      </dl>

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
