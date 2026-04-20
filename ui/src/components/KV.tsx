type KVProps = {
  label: string;
  value?: string | null;
};

export function KV(props: KVProps) {
  return (
    <div className="grid grid-cols-[132px_minmax(0,1fr)] gap-3 border-b border-slate-200/70 py-2 last:border-b-0">
      <dt className="text-sm text-slate-500">{props.label}</dt>
      <dd className="m-0 break-words font-mono text-sm text-slate-900">
        {props.value && props.value !== "" ? props.value : "n/a"}
      </dd>
    </div>
  );
}
