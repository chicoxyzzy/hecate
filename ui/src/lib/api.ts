import type {
  BudgetStatusResponse,
  AccountSummaryResponse,
  ChatResponse,
  ChatSessionResponse,
  ChatSessionsResponse,
  ControlPlaneResponse,
  HealthResponse,
  ModelResponse,
  ProviderPresetResponse,
  ProviderStatusResponse,
  RequestLedgerResponse,
  RuntimeHeaders,
  SessionResponse,
  TraceResponse,
  RetentionRunResponse,
  RetentionRunsResponse,
} from "../types/runtime";

type RequestOptions = {
  authToken?: string;
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  body?: unknown;
};

type ErrorPayload = {
  error?: {
    message?: string;
  };
};

export type ChatCompletionPayload = {
  model: string;
  provider: string;
  session_id?: string;
  session_title?: string;
  user: string;
  messages: Array<{
    role: string;
    content: string;
  }>;
};

export type TenantUpsertPayload = {
  id: string;
  name: string;
  allowed_providers: string[];
  allowed_models: string[];
  enabled: boolean;
};

export type APIKeyUpsertPayload = {
  id: string;
  name: string;
  key: string;
  tenant: string;
  role: string;
  allowed_providers: string[];
  allowed_models: string[];
  enabled: boolean;
};

export type ControlPlaneEnabledPayload = {
  id: string;
  enabled: boolean;
};

export type ControlPlaneDeletePayload = {
  id: string;
};

export type RotateAPIKeyPayload = {
  id: string;
  key: string;
};

export type ProviderUpsertPayload = {
  id: string;
  name: string;
  kind: string;
  protocol: string;
  base_url: string;
  api_version: string;
  default_model: string;
  models: string[];
  allow_any_model: boolean;
  enabled: boolean;
  key: string;
};

export type RetentionRunPayload = {
  subsystems: string[];
};

export type CreateChatSessionPayload = {
  title: string;
};

export async function getHealth(): Promise<HealthResponse> {
  return fetchJSON<HealthResponse>("/healthz");
}

export async function getSession(authToken?: string): Promise<SessionResponse> {
  return fetchJSON<SessionResponse>("/v1/whoami", { authToken });
}

export async function getModels(authToken?: string): Promise<ModelResponse> {
  return fetchJSON<ModelResponse>("/v1/models", { authToken });
}

export async function getProviders(authToken?: string): Promise<ProviderStatusResponse> {
  return fetchJSON<ProviderStatusResponse>("/admin/providers", { authToken });
}

export async function getProviderPresets(authToken?: string): Promise<ProviderPresetResponse> {
  return fetchJSON<ProviderPresetResponse>("/v1/provider-presets", { authToken });
}

export async function getTrace(requestID: string, authToken?: string): Promise<TraceResponse> {
  return fetchJSON<TraceResponse>(`/v1/traces?request_id=${encodeURIComponent(requestID)}`, { authToken });
}

export async function getBudget(query = "", authToken?: string): Promise<BudgetStatusResponse> {
  return fetchJSON<BudgetStatusResponse>(`/admin/budget${query}`, { authToken });
}

export async function getAccountSummary(query = "", authToken?: string): Promise<AccountSummaryResponse> {
  return fetchJSON<AccountSummaryResponse>(`/admin/accounts/summary${query}`, { authToken });
}

export async function getChatSessions(authToken?: string, limit = 20): Promise<ChatSessionsResponse> {
  return fetchJSON<ChatSessionsResponse>(`/v1/chat/sessions?limit=${encodeURIComponent(String(limit))}`, { authToken });
}

export async function createChatSession(payload: CreateChatSessionPayload, authToken?: string): Promise<ChatSessionResponse> {
  return fetchJSON<ChatSessionResponse>("/v1/chat/sessions", { authToken, method: "POST", body: payload });
}

export async function getChatSession(id: string, authToken?: string): Promise<ChatSessionResponse> {
  return fetchJSON<ChatSessionResponse>(`/v1/chat/sessions/${encodeURIComponent(id)}`, { authToken });
}

export async function deleteChatSession(id: string, authToken?: string): Promise<void> {
  await fetchJSON<unknown>(`/v1/chat/sessions/${encodeURIComponent(id)}`, { authToken, method: "DELETE" });
}

export async function updateChatSession(id: string, title: string, authToken?: string): Promise<ChatSessionResponse> {
  return fetchJSON<ChatSessionResponse>(`/v1/chat/sessions/${encodeURIComponent(id)}`, { authToken, method: "PATCH", body: { title } });
}

export async function getRequestLedger(authToken?: string, limit = 20): Promise<RequestLedgerResponse> {
  return fetchJSON<RequestLedgerResponse>(`/admin/requests?limit=${encodeURIComponent(String(limit))}`, { authToken });
}

export async function resetBudget(payload: Record<string, unknown>, authToken?: string): Promise<BudgetStatusResponse> {
  return fetchJSON<BudgetStatusResponse>("/admin/budget/reset", { authToken, method: "POST", body: payload });
}

export async function topUpBudget(payload: Record<string, unknown>, authToken?: string): Promise<BudgetStatusResponse> {
  return fetchJSON<BudgetStatusResponse>("/admin/budget/topup", { authToken, method: "POST", body: payload });
}

export async function setBudgetLimit(payload: Record<string, unknown>, authToken?: string): Promise<BudgetStatusResponse> {
  return fetchJSON<BudgetStatusResponse>("/admin/budget/limit", { authToken, method: "POST", body: payload });
}

export async function getControlPlane(authToken?: string): Promise<ControlPlaneResponse> {
  return fetchJSON<ControlPlaneResponse>("/admin/control-plane", { authToken });
}

export async function upsertTenant(payload: TenantUpsertPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/tenants", { authToken, method: "POST", body: payload });
}

export async function upsertAPIKey(payload: APIKeyUpsertPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/api-keys", { authToken, method: "POST", body: payload });
}

export async function setTenantEnabled(payload: ControlPlaneEnabledPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/tenants/enabled", { authToken, method: "POST", body: payload });
}

export async function deleteTenant(payload: ControlPlaneDeletePayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/tenants/delete", { authToken, method: "POST", body: payload });
}

export async function setAPIKeyEnabled(payload: ControlPlaneEnabledPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/api-keys/enabled", { authToken, method: "POST", body: payload });
}

export async function rotateAPIKey(payload: RotateAPIKeyPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/api-keys/rotate", { authToken, method: "POST", body: payload });
}

export async function deleteAPIKey(payload: ControlPlaneDeletePayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/api-keys/delete", { authToken, method: "POST", body: payload });
}

export async function upsertProvider(payload: ProviderUpsertPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/providers", { authToken, method: "POST", body: payload });
}

export async function setProviderEnabled(payload: ControlPlaneEnabledPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/providers/enabled", { authToken, method: "POST", body: payload });
}

export async function rotateProviderSecret(payload: RotateAPIKeyPayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/providers/rotate-secret", { authToken, method: "POST", body: payload });
}

export async function deleteProvider(payload: ControlPlaneDeletePayload, authToken?: string): Promise<unknown> {
  return fetchJSON("/admin/control-plane/providers/delete", { authToken, method: "POST", body: payload });
}

export async function runRetention(payload: RetentionRunPayload, authToken?: string): Promise<RetentionRunResponse> {
  return fetchJSON<RetentionRunResponse>("/admin/retention/run", { authToken, method: "POST", body: payload });
}

export async function getRetentionRuns(authToken?: string, limit = 10): Promise<RetentionRunsResponse> {
  return fetchJSON<RetentionRunsResponse>(`/admin/retention/runs?limit=${encodeURIComponent(String(limit))}`, { authToken });
}

export async function chatCompletions(
  payload: ChatCompletionPayload,
  authToken?: string,
): Promise<{ data: ChatResponse; headers: RuntimeHeaders }> {
  const response = await fetch("/v1/chat/completions", buildRequestOptions({ authToken, method: "POST", body: payload }));
  if (!response.ok) {
    throw new Error(await errorMessage(response, "request failed"));
  }

  const data = (await response.json()) as ChatResponse;
  return {
    data,
    headers: {
      requestId: response.headers.get("X-Request-Id") ?? "",
      traceId: response.headers.get("X-Trace-Id") ?? "",
      spanId: response.headers.get("X-Span-Id") ?? "",
      provider: response.headers.get("X-Runtime-Provider") ?? "",
      providerKind: response.headers.get("X-Runtime-Provider-Kind") ?? "",
      routeReason: response.headers.get("X-Runtime-Route-Reason") ?? "",
      requestedModel: response.headers.get("X-Runtime-Requested-Model") ?? "",
      resolvedModel: response.headers.get("X-Runtime-Model") ?? "",
      cache: response.headers.get("X-Runtime-Cache") ?? "",
      cacheType: response.headers.get("X-Runtime-Cache-Type") ?? "",
      semanticStrategy: response.headers.get("X-Runtime-Semantic-Strategy") ?? "",
      semanticIndex: response.headers.get("X-Runtime-Semantic-Index") ?? "",
      semanticSimilarity: response.headers.get("X-Runtime-Semantic-Similarity") ?? "",
      attempts: response.headers.get("X-Runtime-Attempts") ?? "",
      retries: response.headers.get("X-Runtime-Retries") ?? "",
      fallbackFrom: response.headers.get("X-Runtime-Fallback-From") ?? "",
      costUsd: response.headers.get("X-Runtime-Cost-USD") ?? "",
    },
  };
}

export function buildRequestOptions(options: RequestOptions): RequestInit {
  const headers = new Headers();
  if (options.authToken) {
    headers.set("Authorization", `Bearer ${options.authToken}`);
  }
  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
  }

  return {
    method: options.method ?? "GET",
    headers,
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
  };
}

export async function fetchJSON<T>(url: string, options: RequestOptions = {}): Promise<T> {
  const response = await fetch(url, buildRequestOptions(options));
  if (!response.ok) {
    throw new Error(await errorMessage(response, "request failed"));
  }
  if (response.status === 204) {
    return undefined as T;
  }
  return (await response.json()) as T;
}

async function errorMessage(response: Response, fallback: string): Promise<string> {
  try {
    const payload = (await response.json()) as ErrorPayload;
    return payload.error?.message ?? fallback;
  } catch {
    return fallback;
  }
}
