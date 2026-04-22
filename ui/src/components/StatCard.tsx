type StatCardProps = {
  label: string;
  value: string;
};

export function StatCard(props: StatCardProps) {
  return (
    <article className="metric-tile">
      <p className="metric-tile__label">{props.label}</p>
      <strong className="metric-tile__value">{props.value}</strong>
    </article>
  );
}
