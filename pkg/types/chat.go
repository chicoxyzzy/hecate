package types

import (
	"encoding/json"
	"time"
)

type ChatRequest struct {
	RequestID     string
	SessionID     string
	SessionTitle  string
	Model         string
	Messages      []Message
	MaxTokens     int
	Temperature   float64
	TopP          float64
	TopK          int
	StopSequences []string
	Scope         RequestScope
	Tools         []Tool
	ToolChoice    json.RawMessage
	Stream        bool
	// Extended thinking (Anthropic): {"type":"enabled","budget_tokens":N}
	Thinking json.RawMessage
	// Anthropic beta features (e.g. ["interleaved-thinking-2025-02-19"])
	Betas []string
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
	KeyID            string // API key ID; used for per-key budget and rate-limit scopes
	AllowedProviders []string
	AllowedModels    []string
}

type Tool struct {
	Type         string          `json:"type"`
	Function     ToolFunction    `json:"function"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"` // Anthropic prompt caching
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ContentBlock represents a single content block within a message, preserving
// provider-specific metadata such as cache_control for Anthropic prompt caching.
type ContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`            // tool_use
	Name         string          `json:"name,omitempty"`          // tool_use
	Input        json.RawMessage `json:"input,omitempty"`         // tool_use
	ToolUseID    string          `json:"tool_use_id,omitempty"`   // tool_result
	CacheControl json.RawMessage `json:"cache_control,omitempty"` // Anthropic prompt caching
	// Extended thinking fields (Anthropic)
	Thinking  string `json:"thinking,omitempty"`  // thinking block content
	Signature string `json:"signature,omitempty"` // thinking block signature (verified by Anthropic)
	Data      string `json:"data,omitempty"`      // redacted_thinking block opaque data
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type Message struct {
	Role          string         `json:"role"`
	Content       string         `json:"content"`
	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"` // set when rich block content is needed (e.g. cache_control)
	Name          string         `json:"name,omitempty"`
	ToolCallID    string         `json:"tool_call_id,omitempty"`
	ToolCalls     []ToolCall     `json:"tool_calls,omitempty"`
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
	BalanceSource      string
	DebitedMicrosUSD   int64
	CreditedMicrosUSD  int64
	BalanceMicrosUSD   int64
	AvailableMicrosUSD int64
	Enforced           bool
	Warnings           []BudgetWarning
	History            []BudgetHistoryEntry
}

type BudgetWarning struct {
	ThresholdPercent   int
	ThresholdMicrosUSD int64
	BalanceMicrosUSD   int64
	AvailableMicrosUSD int64
	Triggered          bool
}

type BudgetHistoryEntry struct {
	Type              string
	Scope             string
	Provider          string
	Tenant            string
	Model             string
	RequestID         string
	Actor             string
	Detail            string
	AmountMicrosUSD   int64
	BalanceMicrosUSD  int64
	CreditedMicrosUSD int64
	DebitedMicrosUSD  int64
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	Timestamp         time.Time
}

type ChatSession struct {
	ID    string
	Title string
	// SystemPrompt is prepended as a system-role message to chat
	// completions made against this session, unless the incoming request
	// already starts with a system message. Empty means no per-session
	// system prompt — clients fall back to whatever they send inline.
	SystemPrompt string
	Tenant       string
	User         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Turns        []ChatSessionTurn
}

type ChatSessionTurn struct {
	ID                string
	RequestID         string
	UserMessage       Message
	AssistantMessage  Message
	RequestedProvider string
	Provider          string
	ProviderKind      string
	RequestedModel    string
	Model             string
	CostMicrosUSD     int64
	PromptTokens      int
	CompletionTokens  int
	TotalTokens       int
	CreatedAt         time.Time
}

type AccountModelEstimate struct {
	Provider                        string
	ProviderKind                    string
	Model                           string
	Default                         bool
	DiscoverySource                 string
	Priced                          bool
	InputMicrosUSDPerMillionTokens  int64
	OutputMicrosUSDPerMillionTokens int64
	EstimatedRemainingPromptTokens  int64
	EstimatedRemainingOutputTokens  int64
}

type RouteDecisionReport struct {
	FinalProvider     string
	FinalProviderKind string
	FinalModel        string
	FinalReason       string
	FallbackFrom      string
	Candidates        []RouteCandidateReport
	Failovers         []RouteFailoverReport
}

type RouteCandidateReport struct {
	Provider           string
	ProviderKind       string
	Model              string
	Reason             string
	Outcome            string
	SkipReason         string
	HealthStatus       string
	EstimatedMicrosUSD int64
	Attempt            int
	RetryCount         int
	Retryable          bool
	Index              int
	LatencyMS          int64
	FailoverFrom       string
	FailoverTo         string
	Detail             string
	Timestamp          time.Time
}

type RouteFailoverReport struct {
	FromProvider string
	FromModel    string
	ToProvider   string
	ToModel      string
	Reason       string
	Timestamp    time.Time
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
