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

export type ChatSessionSummaryRecord = {
  id: string;
  title: string;
  tenant?: string;
  user?: string;
  turn_count: number;
  created_at?: string;
  updated_at?: string;
  last_model?: string;
  last_provider?: string;
  last_cost_usd?: string;
  last_request_id?: string;
};

export type ChatSessionTurnRecord = {
  id: string;
  request_id: string;
  user_message: {
    role: string;
    content: string;
    name?: string;
  };
  assistant_message: {
    role: string;
    content: string | null;
    name?: string;
    tool_calls?: ToolCall[];
  };
  requested_provider?: string;
  provider: string;
  provider_kind?: string;
  requested_model?: string;
  model: string;
  cost_micros_usd: number;
  cost_usd: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  created_at?: string;
};

export type ChatSessionRecord = {
  id: string;
  title: string;
  tenant?: string;
  user?: string;
  created_at?: string;
  updated_at?: string;
  turns?: ChatSessionTurnRecord[];
};

export type ChatSessionsResponse = {
  object: string;
  data: ChatSessionSummaryRecord[];
};

export type ChatSessionResponse = {
  object: string;
  data: ChatSessionRecord;
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

export type ProviderPresetRecord = {
  id: string;
  name: string;
  kind: string;
  protocol: string;
  base_url: string;
  api_key_env?: string;
  api_version?: string;
  default_model?: string;
  example_models?: string[];
  docs_url?: string;
  description?: string;
  env_snippet?: string;
};

export type ProviderPresetResponse = {
  object: string;
  data: ProviderPresetRecord[];
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
  balance_source: string;
  debited_micros_usd: number;
  debited_usd: string;
  credited_micros_usd: number;
  credited_usd: string;
  balance_micros_usd: number;
  balance_usd: string;
  available_micros_usd: number;
  available_usd: string;
  enforced: boolean;
  warnings?: Array<{
    threshold_percent: number;
    threshold_micros_usd: number;
    balance_micros_usd: number;
    available_micros_usd: number;
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
    credited_micros_usd: number;
    credited_usd: string;
    debited_micros_usd: number;
    debited_usd: string;
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
    timestamp?: string;
  }>;
};

export type BudgetStatusResponse = {
  object: string;
  data: BudgetRecord;
};

export type AccountSummaryResponse = {
  object: string;
  data: {
    account: BudgetRecord;
    estimates: Array<{
      provider: string;
      provider_kind: string;
      model: string;
      default?: boolean;
      discovery_source?: string;
      priced: boolean;
      input_micros_usd_per_million_tokens: number;
      output_micros_usd_per_million_tokens: number;
      estimated_remaining_prompt_tokens: number;
      estimated_remaining_output_tokens: number;
    }>;
  };
};

export type RequestLedgerResponse = {
  object: string;
  data: NonNullable<BudgetRecord["history"]>;
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

export type ControlPlaneProviderRecord = {
  id: string;
  name: string;
  preset_id?: string;
  kind: string;
  protocol: string;
  base_url: string;
  api_version?: string;
  default_model?: string;
  models?: string[];
  allow_any_model: boolean;
  explicit_fields?: string[];
  inherited_fields?: string[];
  enabled: boolean;
  credential_configured: boolean;
  credential_preview?: string;
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
    providers: ControlPlaneProviderRecord[];
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

export type RetentionRunData = {
  started_at: string;
  finished_at: string;
  trigger: string;
  actor?: string;
  request_id?: string;
  results: RetentionRunResultRecord[];
};

export type RetentionRunResponse = {
  object: string;
  data: RetentionRunData;
};

export type RetentionRunsResponse = {
  object: string;
  data: RetentionRunData[];
};

export type ToolCallFunction = {
  name: string;
  arguments: string;
};

export type ToolCall = {
  id: string;
  type: string;
  function: ToolCallFunction;
};

export type ChatResponse = {
  id: string;
  model: string;
  choices: Array<{
    index: number;
    finish_reason: string;
    message: {
      role: string;
      content: string | null;
      tool_calls?: ToolCall[];
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
