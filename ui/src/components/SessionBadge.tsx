type SessionBadgeProps = {
  kind: "anonymous" | "tenant" | "admin" | "invalid";
  label: string;
};

export function SessionBadge(props: SessionBadgeProps) {
  return (
    <span className={badgeToneByKind[props.kind]}>
      Session: {props.label}
    </span>
  );
}

const badgeToneByKind: Record<SessionBadgeProps["kind"], string> = {
  admin: "rounded-full border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm font-medium text-emerald-900",
  tenant: "rounded-full border border-cyan-200 bg-cyan-50 px-3 py-2 text-sm font-medium text-cyan-900",
  invalid: "rounded-full border border-red-200 bg-red-50 px-3 py-2 text-sm font-medium text-red-800",
  anonymous: "rounded-full border border-slate-200 bg-white/75 px-3 py-2 text-sm font-medium text-slate-700",
};
