type SessionRestrictionsProps = {
  allowedModels: string[];
  allowedProviders: string[];
  className?: string;
  title?: string;
};

export function SessionRestrictions(props: SessionRestrictionsProps) {
  if (props.allowedProviders.length === 0 && props.allowedModels.length === 0) {
    return null;
  }

  return (
    <div className={props.className ?? "grid gap-3 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-4 text-sm text-amber-950"}>
      {props.title ? <h3 className="text-sm font-semibold uppercase tracking-[0.16em] text-amber-900">{props.title}</h3> : null}
      <div>
        <span className="font-semibold">Providers allowed:</span> {props.allowedProviders.length > 0 ? props.allowedProviders.join(", ") : "any"}
      </div>
      <div>
        <span className="font-semibold">Models allowed:</span> {props.allowedModels.length > 0 ? props.allowedModels.join(", ") : "any"}
      </div>
    </div>
  );
}
