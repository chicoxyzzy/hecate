type SessionBadgeProps = {
  kind: "anonymous" | "tenant" | "admin" | "invalid";
  label: string;
};

export function SessionBadge(props: SessionBadgeProps) {
  return (
    <span className={`session-badge session-badge--${props.kind}`}>
      Session: {props.label}
    </span>
  );
}
