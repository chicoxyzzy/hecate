type ProviderHealthStripProps = {
  label: string;
  summary: string;
  tone: "healthy" | "warning" | "neutral";
};

export function ProviderHealthStrip(props: ProviderHealthStripProps) {
  const toneClassName =
    props.tone === "healthy"
      ? "border-emerald-200 bg-emerald-50 text-emerald-800"
      : props.tone === "warning"
        ? "border-amber-200 bg-amber-50 text-amber-800"
        : "border-slate-200 bg-white/70 text-slate-700";

  return (
    <article className={`rounded-2xl border px-4 py-3 shadow-sm ${toneClassName}`}>
      <p className="text-xs font-semibold uppercase tracking-[0.18em]">{props.label}</p>
      <p className="mt-2 text-sm font-medium">{props.summary}</p>
    </article>
  );
}
