export type GatewayErrorDiagnostic = {
  title: string;
  action: string;
  tone: "danger" | "warning";
};

const diagnostics: Record<string, GatewayErrorDiagnostic> = {
  provider_auth_failed: {
    title: "Provider credentials failed",
    action: "Update the provider API key or disable the provider until credentials are fixed.",
    tone: "danger",
  },
  provider_rate_limited: {
    title: "Provider rate limited the request",
    action: "Retry later, reduce concurrency, or route to another provider.",
    tone: "warning",
  },
  provider_unavailable: {
    title: "Provider is unavailable",
    action: "Check provider health, local runtime process, base URL, or fail over to another model.",
    tone: "danger",
  },
  unsupported_model: {
    title: "Model is not supported by this route",
    action: "Choose a model listed for the selected provider, or switch provider route back to Auto.",
    tone: "warning",
  },
  price_missing: {
    title: "Model price is missing",
    action: "Import or add a pricebook entry before sending cloud traffic for this model.",
    tone: "warning",
  },
  route_impossible: {
    title: "No route could serve this request",
    action: "Enable a healthy provider, discover models, or choose a different provider route.",
    tone: "danger",
  },
  budget_exceeded: {
    title: "Budget exhausted",
    action: "Top up the account, raise the limit, or choose a cheaper/local model.",
    tone: "warning",
  },
  rate_limit_exceeded: {
    title: "Gateway rate limit exceeded",
    action: "Wait for the bucket to refill or adjust the per-key rate limit.",
    tone: "warning",
  },
  forbidden: {
    title: "Request blocked by policy",
    action: "Review tenant key scope, provider/model allowlists, and policy rules.",
    tone: "warning",
  },
};

export function describeGatewayError(code?: string, status?: number): GatewayErrorDiagnostic | null {
  if (code && diagnostics[code]) {
    return diagnostics[code];
  }
  if (status === 401 || status === 403) {
    return diagnostics.forbidden;
  }
  if (status === 429) {
    return diagnostics.rate_limit_exceeded;
  }
  if (status && status >= 500) {
    return {
      title: "Gateway or upstream failed",
      action: "Open Observe for the request trace and inspect provider health.",
      tone: "danger",
    };
  }
  return null;
}

export function formatErrorCode(code?: string, status?: number): string {
  if (code && status) {
    return `${status} · ${code}`;
  }
  if (code) {
    return code;
  }
  if (status) {
    return String(status);
  }
  return "";
}
