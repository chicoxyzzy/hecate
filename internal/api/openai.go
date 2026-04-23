package api

import "encoding/json"

type OpenAIChatCompletionRequest struct {
	Model        string              `json:"model"`
	Provider     string              `json:"provider,omitempty"`
	SessionID    string              `json:"session_id,omitempty"`
	SessionTitle string              `json:"session_title,omitempty"`
	Messages     []OpenAIChatMessage `json:"messages"`
	MaxTokens    int                 `json:"max_tokens,omitempty"`
	Temperature  float64             `json:"temperature,omitempty"`
	User         string              `json:"user,omitempty"`
	Tools        []OpenAITool        `json:"tools,omitempty"`
	ToolChoice   json.RawMessage     `json:"tool_choice,omitempty"`
	Stream       bool                `json:"stream,omitempty"`
}

type OpenAITool struct {
	Type     string             `json:"type"`
	Function OpenAIToolFunction `json:"function"`
}

type OpenAIToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type OpenAIToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function OpenAIToolCallFunction `json:"function"`
}

type OpenAIToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIChatMessage struct {
	Role       string           `json:"role"`
	Content    *string          `json:"content"`
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
}

type OpenAIChatCompletionResponse struct {
	ID      string                       `json:"id"`
	Object  string                       `json:"object"`
	Created int64                        `json:"created"`
	Model   string                       `json:"model"`
	Choices []OpenAIChatCompletionChoice `json:"choices"`
	Usage   OpenAIUsage                  `json:"usage"`
}

type OpenAIChatCompletionChoice struct {
	Index        int               `json:"index"`
	Message      OpenAIChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type OpenAIModelsResponse struct {
	Object string            `json:"object"`
	Data   []OpenAIModelData `json:"data"`
}

type SessionResponse struct {
	Object string              `json:"object"`
	Data   SessionResponseItem `json:"data"`
}

type ChatSessionsResponse struct {
	Object string                   `json:"object"`
	Data   []ChatSessionSummaryItem `json:"data"`
}

type ChatSessionResponse struct {
	Object string          `json:"object"`
	Data   ChatSessionItem `json:"data"`
}

type CreateChatSessionRequest struct {
	Title string `json:"title"`
}

type UpdateChatSessionRequest struct {
	Title string `json:"title"`
}

type SessionResponseItem struct {
	Authenticated    bool     `json:"authenticated"`
	InvalidToken     bool     `json:"invalid_token"`
	Role             string   `json:"role"`
	Name             string   `json:"name,omitempty"`
	Tenant           string   `json:"tenant,omitempty"`
	Source           string   `json:"source,omitempty"`
	KeyID            string   `json:"key_id,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
}

type ChatSessionSummaryItem struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Tenant        string `json:"tenant,omitempty"`
	User          string `json:"user,omitempty"`
	TurnCount     int    `json:"turn_count"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	LastModel     string `json:"last_model,omitempty"`
	LastProvider  string `json:"last_provider,omitempty"`
	LastCostUSD   string `json:"last_cost_usd,omitempty"`
	LastRequestID string `json:"last_request_id,omitempty"`
}

type ChatSessionItem struct {
	ID        string                `json:"id"`
	Title     string                `json:"title"`
	Tenant    string                `json:"tenant,omitempty"`
	User      string                `json:"user,omitempty"`
	CreatedAt string                `json:"created_at,omitempty"`
	UpdatedAt string                `json:"updated_at,omitempty"`
	Turns     []ChatSessionTurnItem `json:"turns"`
}

type ChatSessionTurnItem struct {
	ID                string            `json:"id"`
	RequestID         string            `json:"request_id"`
	UserMessage       OpenAIChatMessage `json:"user_message"`
	AssistantMessage  OpenAIChatMessage `json:"assistant_message"`
	RequestedProvider string            `json:"requested_provider,omitempty"`
	Provider          string            `json:"provider"`
	ProviderKind      string            `json:"provider_kind,omitempty"`
	RequestedModel    string            `json:"requested_model,omitempty"`
	Model             string            `json:"model"`
	CostMicrosUSD     int64             `json:"cost_micros_usd"`
	CostUSD           string            `json:"cost_usd"`
	PromptTokens      int               `json:"prompt_tokens"`
	CompletionTokens  int               `json:"completion_tokens"`
	TotalTokens       int               `json:"total_tokens"`
	CreatedAt         string            `json:"created_at,omitempty"`
}

type OpenAIModelData struct {
	ID       string         `json:"id"`
	Object   string         `json:"object"`
	OwnedBy  string         `json:"owned_by"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ProviderStatusResponse struct {
	Object string                       `json:"object"`
	Data   []ProviderStatusResponseItem `json:"data"`
}

type ProviderPresetResponse struct {
	Object string                       `json:"object"`
	Data   []ProviderPresetResponseItem `json:"data"`
}

type TraceResponse struct {
	Object string            `json:"object"`
	Data   TraceResponseItem `json:"data"`
}

type TraceResponseItem struct {
	RequestID string                 `json:"request_id"`
	TraceID   string                 `json:"trace_id,omitempty"`
	StartedAt string                 `json:"started_at,omitempty"`
	Spans     []TraceSpanRecord      `json:"spans,omitempty"`
	Route     TraceRouteReportRecord `json:"route,omitempty"`
}

type TraceRouteReportRecord struct {
	FinalProvider     string                      `json:"final_provider,omitempty"`
	FinalProviderKind string                      `json:"final_provider_kind,omitempty"`
	FinalModel        string                      `json:"final_model,omitempty"`
	FinalReason       string                      `json:"final_reason,omitempty"`
	FallbackFrom      string                      `json:"fallback_from,omitempty"`
	Candidates        []TraceRouteCandidateRecord `json:"candidates,omitempty"`
	Failovers         []TraceRouteFailoverRecord  `json:"failovers,omitempty"`
}

type TraceRouteCandidateRecord struct {
	Provider           string `json:"provider,omitempty"`
	ProviderKind       string `json:"provider_kind,omitempty"`
	Model              string `json:"model,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Outcome            string `json:"outcome,omitempty"`
	SkipReason         string `json:"skip_reason,omitempty"`
	HealthStatus       string `json:"health_status,omitempty"`
	EstimatedMicrosUSD int64  `json:"estimated_micros_usd,omitempty"`
	EstimatedUSD       string `json:"estimated_usd,omitempty"`
	Attempt            int    `json:"attempt,omitempty"`
	RetryCount         int    `json:"retry_count,omitempty"`
	Retryable          bool   `json:"retryable,omitempty"`
	Index              int    `json:"index,omitempty"`
	LatencyMS          int64  `json:"latency_ms,omitempty"`
	FailoverFrom       string `json:"failover_from,omitempty"`
	FailoverTo         string `json:"failover_to,omitempty"`
	Detail             string `json:"detail,omitempty"`
	Timestamp          string `json:"timestamp,omitempty"`
}

type TraceRouteFailoverRecord struct {
	FromProvider string `json:"from_provider,omitempty"`
	FromModel    string `json:"from_model,omitempty"`
	ToProvider   string `json:"to_provider,omitempty"`
	ToModel      string `json:"to_model,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Timestamp    string `json:"timestamp,omitempty"`
}

type TraceSpanRecord struct {
	TraceID       string             `json:"trace_id"`
	SpanID        string             `json:"span_id"`
	ParentSpanID  string             `json:"parent_span_id,omitempty"`
	Name          string             `json:"name"`
	Kind          string             `json:"kind,omitempty"`
	StartTime     string             `json:"start_time,omitempty"`
	EndTime       string             `json:"end_time,omitempty"`
	Attributes    map[string]any     `json:"attributes,omitempty"`
	StatusCode    string             `json:"status_code,omitempty"`
	StatusMessage string             `json:"status_message,omitempty"`
	Events        []TraceEventRecord `json:"events,omitempty"`
}

type TraceEventRecord struct {
	Name       string         `json:"name"`
	Timestamp  string         `json:"timestamp"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type ProviderStatusResponseItem struct {
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	Healthy         bool     `json:"healthy"`
	Status          string   `json:"status"`
	DefaultModel    string   `json:"default_model,omitempty"`
	Models          []string `json:"models,omitempty"`
	DiscoverySource string   `json:"discovery_source,omitempty"`
	RefreshedAt     string   `json:"refreshed_at,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type ProviderPresetResponseItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	Protocol     string `json:"protocol"`
	BaseURL      string `json:"base_url"`
	APIKeyEnv    string `json:"api_key_env,omitempty"`
	APIVersion   string `json:"api_version,omitempty"`
	DefaultModel string `json:"default_model,omitempty"`
	DocsURL      string `json:"docs_url,omitempty"`
	Description  string `json:"description,omitempty"`
	EnvSnippet   string `json:"env_snippet,omitempty"`
}

type BudgetStatusResponse struct {
	Object string                   `json:"object"`
	Data   BudgetStatusResponseItem `json:"data"`
}

type AccountSummaryResponse struct {
	Object string                     `json:"object"`
	Data   AccountSummaryResponseItem `json:"data"`
}

type RequestLedgerResponse struct {
	Object string                `json:"object"`
	Data   []BudgetHistoryRecord `json:"data"`
}

type AccountSummaryResponseItem struct {
	Account   BudgetStatusResponseItem     `json:"account"`
	Estimates []AccountModelEstimateRecord `json:"estimates"`
}

type AccountModelEstimateRecord struct {
	Provider                        string `json:"provider"`
	ProviderKind                    string `json:"provider_kind"`
	Model                           string `json:"model"`
	Default                         bool   `json:"default,omitempty"`
	DiscoverySource                 string `json:"discovery_source,omitempty"`
	Priced                          bool   `json:"priced"`
	InputMicrosUSDPerMillionTokens  int64  `json:"input_micros_usd_per_million_tokens"`
	OutputMicrosUSDPerMillionTokens int64  `json:"output_micros_usd_per_million_tokens"`
	EstimatedRemainingPromptTokens  int64  `json:"estimated_remaining_prompt_tokens"`
	EstimatedRemainingOutputTokens  int64  `json:"estimated_remaining_output_tokens"`
}

type BudgetStatusResponseItem struct {
	Key                string                `json:"key"`
	Scope              string                `json:"scope"`
	Provider           string                `json:"provider,omitempty"`
	Tenant             string                `json:"tenant,omitempty"`
	Backend            string                `json:"backend"`
	BalanceSource      string                `json:"balance_source"`
	DebitedMicrosUSD   int64                 `json:"debited_micros_usd"`
	DebitedUSD         string                `json:"debited_usd"`
	CreditedMicrosUSD  int64                 `json:"credited_micros_usd"`
	CreditedUSD        string                `json:"credited_usd"`
	BalanceMicrosUSD   int64                 `json:"balance_micros_usd"`
	BalanceUSD         string                `json:"balance_usd"`
	AvailableMicrosUSD int64                 `json:"available_micros_usd"`
	AvailableUSD       string                `json:"available_usd"`
	Enforced           bool                  `json:"enforced"`
	Warnings           []BudgetWarningRecord `json:"warnings,omitempty"`
	History            []BudgetHistoryRecord `json:"history,omitempty"`
}

type BudgetWarningRecord struct {
	ThresholdPercent   int   `json:"threshold_percent"`
	ThresholdMicrosUSD int64 `json:"threshold_micros_usd"`
	BalanceMicrosUSD   int64 `json:"balance_micros_usd"`
	AvailableMicrosUSD int64 `json:"available_micros_usd"`
	Triggered          bool  `json:"triggered"`
}

type BudgetHistoryRecord struct {
	Type              string `json:"type"`
	Scope             string `json:"scope,omitempty"`
	Provider          string `json:"provider,omitempty"`
	Tenant            string `json:"tenant,omitempty"`
	Model             string `json:"model,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	Actor             string `json:"actor,omitempty"`
	Detail            string `json:"detail,omitempty"`
	AmountMicrosUSD   int64  `json:"amount_micros_usd"`
	AmountUSD         string `json:"amount_usd"`
	BalanceMicrosUSD  int64  `json:"balance_micros_usd"`
	BalanceUSD        string `json:"balance_usd"`
	CreditedMicrosUSD int64  `json:"credited_micros_usd"`
	CreditedUSD       string `json:"credited_usd"`
	DebitedMicrosUSD  int64  `json:"debited_micros_usd"`
	DebitedUSD        string `json:"debited_usd"`
	PromptTokens      int    `json:"prompt_tokens,omitempty"`
	CompletionTokens  int    `json:"completion_tokens,omitempty"`
	TotalTokens       int    `json:"total_tokens,omitempty"`
	Timestamp         string `json:"timestamp,omitempty"`
}

type RetentionRunData struct {
	StartedAt  string                     `json:"started_at"`
	FinishedAt string                     `json:"finished_at"`
	Trigger    string                     `json:"trigger"`
	Actor      string                     `json:"actor,omitempty"`
	RequestID  string                     `json:"request_id,omitempty"`
	Results    []RetentionRunResultRecord `json:"results"`
}

type RetentionRunResultRecord struct {
	Name     string `json:"name"`
	Deleted  int    `json:"deleted"`
	MaxAge   string `json:"max_age,omitempty"`
	MaxCount int    `json:"max_count"`
	Error    string `json:"error,omitempty"`
	Skipped  bool   `json:"skipped,omitempty"`
}

type RetentionRunResponse struct {
	Object string           `json:"object"`
	Data   RetentionRunData `json:"data"`
}

type RetentionRunsResponse struct {
	Object string             `json:"object"`
	Data   []RetentionRunData `json:"data"`
}

type BudgetResetRequest struct {
	Key      string `json:"key"`
	Scope    string `json:"scope"`
	Provider string `json:"provider"`
	Tenant   string `json:"tenant"`
}

type BudgetTopUpRequest struct {
	Key             string `json:"key"`
	Scope           string `json:"scope"`
	Provider        string `json:"provider"`
	Tenant          string `json:"tenant"`
	AmountMicrosUSD int64  `json:"amount_micros_usd"`
}

type BudgetBalanceRequest struct {
	Key              string `json:"key"`
	Scope            string `json:"scope"`
	Provider         string `json:"provider"`
	Tenant           string `json:"tenant"`
	BalanceMicrosUSD int64  `json:"balance_micros_usd"`
}

type ControlPlaneResponse struct {
	Object string                   `json:"object"`
	Data   ControlPlaneResponseItem `json:"data"`
}

type ControlPlaneResponseItem struct {
	Backend   string                         `json:"backend"`
	Path      string                         `json:"path,omitempty"`
	Tenants   []ControlPlaneTenantItem       `json:"tenants"`
	APIKeys   []ControlPlaneAPIKeyRecord     `json:"api_keys"`
	Providers []ControlPlaneProviderRecord   `json:"providers"`
	Events    []ControlPlaneAuditEventRecord `json:"events"`
}

type ControlPlaneTenantItem struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
	Enabled          bool     `json:"enabled"`
}

type ControlPlaneAPIKeyRecord struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Tenant           string   `json:"tenant,omitempty"`
	Role             string   `json:"role"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
	Enabled          bool     `json:"enabled"`
	KeyPreview       string   `json:"key_preview,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
}

type ControlPlaneProviderRecord struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	PresetID             string   `json:"preset_id,omitempty"`
	Kind                 string   `json:"kind"`
	Protocol             string   `json:"protocol"`
	BaseURL              string   `json:"base_url"`
	APIVersion           string   `json:"api_version,omitempty"`
	DefaultModel         string   `json:"default_model,omitempty"`
	ExplicitFields       []string `json:"explicit_fields,omitempty"`
	InheritedFields      []string `json:"inherited_fields,omitempty"`
	Enabled              bool     `json:"enabled"`
	CredentialConfigured bool     `json:"credential_configured"`
	CredentialPreview    string   `json:"credential_preview,omitempty"`
	CreatedAt            string   `json:"created_at,omitempty"`
	UpdatedAt            string   `json:"updated_at,omitempty"`
}

type ControlPlaneAuditEventRecord struct {
	Timestamp  string `json:"timestamp"`
	Actor      string `json:"actor"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Detail     string `json:"detail,omitempty"`
}

type ControlPlaneTenantUpsertRequest struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	AllowedProviders []string `json:"allowed_providers"`
	AllowedModels    []string `json:"allowed_models"`
	Enabled          bool     `json:"enabled"`
}

type ControlPlaneAPIKeyUpsertRequest struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Key              string   `json:"key"`
	Tenant           string   `json:"tenant"`
	Role             string   `json:"role"`
	AllowedProviders []string `json:"allowed_providers"`
	AllowedModels    []string `json:"allowed_models"`
	Enabled          bool     `json:"enabled"`
}

type ControlPlaneProviderUpsertRequest struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	PresetID     string  `json:"preset_id"`
	Kind         *string `json:"kind,omitempty"`
	Protocol     *string `json:"protocol,omitempty"`
	BaseURL      *string `json:"base_url,omitempty"`
	APIVersion   *string `json:"api_version,omitempty"`
	DefaultModel *string `json:"default_model,omitempty"`
	Enabled      bool    `json:"enabled"`
	Key          string  `json:"key"`
}

type ControlPlaneTenantLifecycleRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type ControlPlaneAPIKeyLifecycleRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Key     string `json:"key"`
}

type ControlPlaneProviderLifecycleRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Key     string `json:"key"`
}
