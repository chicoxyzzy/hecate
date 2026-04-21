package types

import "time"

type ChatRequest struct {
	RequestID   string
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	Scope       RequestScope
}

type RequestScope struct {
	Tenant           string
	User             string
	ProviderHint     string
	AllowedProviders []string
	AllowedModels    []string
	Principal        PrincipalContext
}

type PrincipalContext struct {
	Role             string
	Tenant           string
	AllowedProviders []string
	AllowedModels    []string
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type ChatResponse struct {
	ID        string
	Model     string
	CreatedAt time.Time
	Choices   []ChatChoice
	Usage     Usage
	Cost      CostBreakdown
	Route     RouteDecision
}

type ChatChoice struct {
	Index        int
	Message      Message
	FinishReason string
}

type Usage struct {
	PromptTokens       int
	CompletionTokens   int
	TotalTokens        int
	CachedPromptTokens int
}

type CostBreakdown struct {
	Currency                  string
	InputMicrosUSD            int64
	OutputMicrosUSD           int64
	CachedInputMicrosUSD      int64
	TotalMicrosUSD            int64
	InputMicrosUSDPerMillion  int64
	OutputMicrosUSDPerMillion int64
}

type RouteDecision struct {
	Provider     string
	ProviderKind string
	Model        string
	Reason       string
}

type ModelInfo struct {
	ID              string
	Provider        string
	Kind            string
	OwnedBy         string
	Default         bool
	DiscoverySource string
}

type ProviderStatus struct {
	Name            string
	Kind            string
	Healthy         bool
	Status          string
	DefaultModel    string
	Models          []string
	DiscoverySource string
	RefreshedAt     time.Time
	Error           string
}

type BudgetStatus struct {
	Key                string
	Scope              string
	Provider           string
	Tenant             string
	Backend            string
	LimitSource        string
	SpentMicrosUSD     int64
	CurrentMicrosUSD   int64
	MaxMicrosUSD       int64
	RemainingMicrosUSD int64
	Enforced           bool
	Warnings           []BudgetWarning
	History            []BudgetHistoryEntry
}

type BudgetWarning struct {
	ThresholdPercent   int
	ThresholdMicrosUSD int64
	CurrentMicrosUSD   int64
	RemainingMicrosUSD int64
	Triggered          bool
}

type BudgetHistoryEntry struct {
	Type             string
	Scope            string
	Provider         string
	Tenant           string
	Model            string
	RequestID        string
	Actor            string
	Detail           string
	AmountMicrosUSD  int64
	BalanceMicrosUSD int64
	LimitMicrosUSD   int64
	Timestamp        time.Time
}

type TraceEvent struct {
	Name       string
	Timestamp  time.Time
	Attributes map[string]any
}

type TraceSpan struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Name          string
	Kind          string
	StartTime     time.Time
	EndTime       time.Time
	Attributes    map[string]any
	Events        []TraceEvent
	StatusCode    string
	StatusMessage string
}
