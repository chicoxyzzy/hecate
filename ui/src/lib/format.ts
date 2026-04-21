export function formatDateTime(value?: string): string {
  if (!value) {
    return "Not available";
  }

  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) {
    return value;
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(parsed);
}

export function formatRelativeCount(label: string, value: number, total: number): string {
  if (total <= 0) {
    return `0 ${label}`;
  }
  return `${value}/${total} ${label}`;
}

export function formatUsd(value?: string): string {
  if (!value) {
    return "$0.00";
  }
  const parsed = Number.parseFloat(value);
  if (!Number.isFinite(parsed)) {
    return value;
  }
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: "USD",
    minimumFractionDigits: parsed >= 1 ? 2 : 4,
    maximumFractionDigits: 6,
  }).format(parsed);
}

export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) {
    return "0%";
  }
  return `${Math.round(value * 100)}%`;
}

export function titleFromKind(value?: string): string {
  if (!value) {
    return "Unknown";
  }
  return value.charAt(0).toUpperCase() + value.slice(1);
}
