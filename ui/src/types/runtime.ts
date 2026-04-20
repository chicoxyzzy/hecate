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
  costUsd: string;
};

export type ModelFilter = "all" | "local" | "cloud";
export type ProviderFilter = "auto" | string;
