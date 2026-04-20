type StatCardProps = {
  label: string;
  value: string;
};

export function StatCard(props: StatCardProps) {
  return (
    <article className="rounded-[24px] border border-slate-200/70 bg-white/75 p-5 shadow-[0_18px_45px_rgba(41,67,84,0.08)] backdrop-blur">
      <p className="mb-2 text-sm text-slate-500">{props.label}</p>
      <strong className="text-2xl font-semibold text-slate-900">{props.value}</strong>
    </article>
  );
}
