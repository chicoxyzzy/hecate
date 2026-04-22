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
    <div className={props.className ?? "info-block info-block--warning"}>
      {props.title ? <h3 className="info-block__title">{props.title}</h3> : null}
      <div className="stack-sm" style={{ marginTop: props.title ? "0.75rem" : undefined }}>
        <div>
          <span className="font-semibold">Providers allowed:</span> {props.allowedProviders.length > 0 ? props.allowedProviders.join(", ") : "any"}
        </div>
        <div>
          <span className="font-semibold">Models allowed:</span> {props.allowedModels.length > 0 ? props.allowedModels.join(", ") : "any"}
        </div>
      </div>
    </div>
  );
}
