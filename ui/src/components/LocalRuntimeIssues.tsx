import type { LocalProviderIssue } from "../lib/provider-issues";

type LocalRuntimeIssuesProps = {
  copiedCommand: string;
  issues: LocalProviderIssue[];
  onCopyCommand: (command: string) => void | Promise<void>;
};

export function LocalRuntimeIssues(props: LocalRuntimeIssuesProps) {
  if (props.issues.length === 0) {
    return null;
  }

  return (
    <section className="mb-4 rounded-[24px] border border-amber-200 bg-amber-50/95 p-5 shadow-[0_18px_45px_rgba(41,67,84,0.08)]">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="mb-1 text-xs font-semibold uppercase tracking-[0.22em] text-amber-700">Local Runtime</p>
          <h2 className="font-serif text-3xl text-slate-900">Model availability hints</h2>
        </div>
      </div>
      <div className="mt-4 grid gap-3">
        {props.issues.map((issue) => (
          <article className="rounded-2xl border border-amber-200 bg-white/70 p-4" key={`${issue.provider}-${issue.model}`}>
            <p className="text-sm font-semibold text-slate-900">
              {issue.provider} is configured for <span className="font-mono">{issue.model}</span>, but that model is not currently discoverable.
            </p>
            <p className="mt-2 text-sm text-slate-600">{issue.message}</p>
            {issue.command ? (
              <div className="mt-3 rounded-2xl bg-slate-950 px-4 py-3 text-sm text-slate-100">
                <div className="flex items-center justify-between gap-3">
                  <code className="overflow-x-auto">{issue.command}</code>
                  <button
                    className="shrink-0 rounded-full border border-slate-700 bg-slate-900 px-3 py-1.5 text-xs font-medium text-slate-100 transition hover:bg-slate-800"
                    onClick={() => void props.onCopyCommand(issue.command!)}
                    type="button"
                  >
                    {props.copiedCommand === issue.command ? "Copied" : "Copy"}
                  </button>
                </div>
              </div>
            ) : null}
          </article>
        ))}
      </div>
    </section>
  );
}
