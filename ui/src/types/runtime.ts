export type HealthResponse = {
  status: string;
  time: string;
};

export type ModelRecord = {
  id: string;
  owned_by: string;
  metadata?: {
    provider?: string;
    provider_kind?: string;
    default?: boolean;
    discovery_source?: string;
  };
};

export type ModelResponse = {
  object: string;
  data: ModelRecord[];
};

export type SessionResponse = {
  object: string;
  data: {
    authenticated: boolean;
    invalid_token: boolean;
    role: string;
    name?: string;
    tenant?: string;
    source?: string;
    key_id?: string;
    allowed_providers?: string[];
    allowed_models?: string[];
  };
};

export type ProviderRecord = {
  name: string;
  kind: string;
  healthy: boolean;
  status: string;
  default_model?: string;
  models?: string[];
  discovery_source?: string;
  refreshed_at?: string;
  error?: string;
};

export type ProviderStatusResponse = {
  object: string;
  data: ProviderRecord[];
};

export type TraceEventRecord = {
  name: string;
  timestamp: string;
  attributes?: Record<string, unknown>;
};

export type TraceSpanRecord = {
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  name: string;
  kind?: string;
  start_time?: string;
  end_time?: string;
  attributes?: Record<string, unknown>;
  status_code?: string;
  status_message?: string;
  events?: TraceEventRecord[];
};

export type TraceResponse = {
  object: string;
  data: {
    request_id: string;
    trace_id?: string;
    started_at?: string;
    spans?: TraceSpanRecord[];
    route?: {
      final_provider?: string;
      final_provider_kind?: string;
      final_model?: string;
      final_reason?: string;
      fallback_from?: string;
      candidates?: Array<{
        provider?: string;
        provider_kind?: string;
        model?: string;
        reason?: string;
        outcome?: string;
        skip_reason?: string;
        health_status?: string;
        estimated_micros_usd?: number;
        estimated_usd?: string;
        attempt?: number;
        retry_count?: number;
        retryable?: boolean;
        index?: number;
        latency_ms?: number;
        failover_from?: string;
        failover_to?: string;
        detail?: string;
        timestamp?: string;
      }>;
      failovers?: Array<{
        from_provider?: string;
        from_model?: string;
        to_provider?: string;
        to_model?: string;
        reason?: string;
        timestamp?: string;
      }>;
    };
  };
};

export type BudgetRecord = {
  key: string;
  scope: string;
  provider?: string;
  tenant?: string;
  backend: string;
  limit_source: string;
  spent_micros_usd: number;
  spent_usd: string;
  current_micros_usd: number;
  current_usd: string;
  max_micros_usd: number;
  max_usd: string;
  remaining_micros_usd: number;
  remaining_usd: string;
  enforced: boolean;
  warnings?: Array<{
    threshold_percent: number;
    threshold_micros_usd: number;
    current_micros_usd: number;
    remaining_micros_usd: number;
    triggered: boolean;
  }>;
  history?: Array<{
    type: string;
    scope?: string;
    provider?: string;
    tenant?: string;
    model?: string;
    request_id?: string;
    actor?: string;
    detail?: string;
    amount_micros_usd: number;
    amount_usd: string;
    balance_micros_usd: number;
    balance_usd: string;
    limit_micros_usd: number;
    limit_usd: string;
    timestamp?: string;
  }>;
};

export type BudgetStatusResponse = {
  object: string;
  data: BudgetRecord;
};

export type ControlPlaneTenantRecord = {
  id: string;
  name: string;
  description?: string;
  allowed_providers?: string[];
  allowed_models?: string[];
  enabled: boolean;
};

export type ControlPlaneAPIKeyRecord = {
  id: string;
  name: string;
  tenant?: string;
  role: string;
  allowed_providers?: string[];
  allowed_models?: string[];
  enabled: boolean;
  key_preview?: string;
  created_at?: string;
  updated_at?: string;
};

export type ControlPlaneAuditEventRecord = {
  timestamp?: string;
  actor: string;
  action: string;
  target_type: string;
  target_id: string;
  detail?: string;
};

export type ControlPlaneResponse = {
  object: string;
  data: {
    backend: string;
    path?: string;
    tenants: ControlPlaneTenantRecord[];
    api_keys: ControlPlaneAPIKeyRecord[];
    events: ControlPlaneAuditEventRecord[];
  };
};

export type RetentionRunResultRecord = {
  name: string;
  deleted: number;
  max_age?: string;
  max_count: number;
  error?: string;
  skipped?: boolean;
};

export type RetentionRunResponse = {
  object: string;
  data: {
    started_at: string;
    finished_at: string;
    trigger: string;
    results: RetentionRunResultRecord[];
  };
};

export type ChatResponse = {
  id: string;
  model: string;
  choices: Array<{
    index: number;
    finish_reason: string;
    message: {
      role: string;
      content: string;
    };
  }>;
  usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  };
};

export type RuntimeHeaders = {
  requestId: string;
  traceId: string;
  spanId: string;
  provider: string;
  providerKind: string;
  routeReason: string;
  requestedModel: string;
  resolvedModel: string;
  cache: string;
  cacheType: string;
  semanticStrategy: string;
  semanticIndex: string;
  semanticSimilarity: string;
  attempts: string;
  retries: string;
  fallbackFrom: string;
  costUsd: string;
};

export type ModelFilter = "all" | "local" | "cloud";
export type ProviderFilter = "auto" | string;
