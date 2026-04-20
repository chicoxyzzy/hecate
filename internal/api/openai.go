package api

type OpenAIChatCompletionRequest struct {
	Model       string              `json:"model"`
	Provider    string              `json:"provider,omitempty"`
	Messages    []OpenAIChatMessage `json:"messages"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
	Temperature float64             `json:"temperature,omitempty"`
	User        string              `json:"user,omitempty"`
}

type OpenAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
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

type BudgetStatusResponse struct {
	Object string                   `json:"object"`
	Data   BudgetStatusResponseItem `json:"data"`
}

type BudgetStatusResponseItem struct {
	Key                string `json:"key"`
	Scope              string `json:"scope"`
	Provider           string `json:"provider,omitempty"`
	Tenant             string `json:"tenant,omitempty"`
	Backend            string `json:"backend"`
	LimitSource        string `json:"limit_source"`
	SpentMicrosUSD     int64  `json:"spent_micros_usd"`
	SpentUSD           string `json:"spent_usd"`
	CurrentMicrosUSD   int64  `json:"current_micros_usd"`
	CurrentUSD         string `json:"current_usd"`
	MaxMicrosUSD       int64  `json:"max_micros_usd"`
	MaxUSD             string `json:"max_usd"`
	RemainingMicrosUSD int64  `json:"remaining_micros_usd"`
	RemainingUSD       string `json:"remaining_usd"`
	Enforced           bool   `json:"enforced"`
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

type BudgetLimitRequest struct {
	Key            string `json:"key"`
	Scope          string `json:"scope"`
	Provider       string `json:"provider"`
	Tenant         string `json:"tenant"`
	LimitMicrosUSD int64  `json:"limit_micros_usd"`
}

type ControlPlaneResponse struct {
	Object string                   `json:"object"`
	Data   ControlPlaneResponseItem `json:"data"`
}

type ControlPlaneResponseItem struct {
	Backend string                         `json:"backend"`
	Path    string                         `json:"path,omitempty"`
	Tenants []ControlPlaneTenantItem       `json:"tenants"`
	APIKeys []ControlPlaneAPIKeyRecord     `json:"api_keys"`
	Events  []ControlPlaneAuditEventRecord `json:"events"`
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

type ControlPlaneTenantLifecycleRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
}

type ControlPlaneAPIKeyLifecycleRequest struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Key     string `json:"key"`
}
