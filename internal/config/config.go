package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server    ServerConfig
	Router    RouterConfig
	Provider  ProviderConfig
	Chat      ChatConfig
	OTel      OTelConfig
	Governor  GovernorConfig
	Cache     CacheConfig
	Retention RetentionConfig
	Postgres  PostgresConfig
	Providers ProvidersConfig
	Pricebook PricebookConfig
	LogLevel  string
}

type ServerConfig struct {
	Address               string
	AuthToken             string
	ControlPlaneBackend   string
	ControlPlaneFile      string
	ControlPlaneKey       string
	ControlPlaneSecretKey string
	TasksBackend          string
	TaskApprovalPolicies  []string
	TaskQueueWorkers      int
	TaskQueueBuffer       int

	// TraceBodyCapture enables recording (redacted) request and response bodies
	// in the distributed trace.  Off by default; enable via GATEWAY_TRACE_BODIES=true.
	TraceBodyCapture bool
	// TraceBodyMaxBytes caps each captured body at this many bytes (default 4096).
	TraceBodyMaxBytes int

	// RateLimit controls per-API-key request throttling.
	RateLimit RateLimitConfig
}

// RateLimitConfig configures the token-bucket rate limiter applied per API key.
type RateLimitConfig struct {
	// Enabled turns on per-key rate limiting.  Off by default.
	Enabled bool
	// RequestsPerMinute is the steady-state refill rate and the X-RateLimit-Limit
	// value (default 60).
	RequestsPerMinute int64
	// BurstSize is the maximum number of tokens that can accumulate (default equals
	// RequestsPerMinute).
	BurstSize int64
}

type RouterConfig struct {
	DefaultModel string
}

type ProviderConfig struct {
	MaxAttempts     int
	RetryBackoff    time.Duration
	FailoverEnabled bool
	HealthThreshold int
	HealthCooldown  time.Duration
}

type ChatConfig struct {
	SessionsBackend string
	SessionsFile    string
	SessionsKey     string
	SessionLimit    int
}

type OTelSignalConfig struct {
	Enabled  bool
	Endpoint string
	Headers  map[string]string
	Timeout  time.Duration
}

type OTelConfig struct {
	ServiceName     string
	Traces          OTelSignalConfig
	Metrics         OTelSignalConfig
	MetricsInterval time.Duration
	Logs            OTelSignalConfig
}

type GovernorConfig struct {
	DenyAll                 bool
	MaxPromptTokens         int
	MaxTotalBudgetMicros    int64
	ModelRewriteTo          string
	PolicyRules             []PolicyRuleConfig `json:"policy_rules"`
	BudgetBackend           string
	BudgetKey               string
	BudgetScope             string
	BudgetTenantFallback    string
	RouteMode               string
	AllowedProviders        []string
	DeniedProviders         []string
	AllowedModels           []string
	DeniedModels            []string
	AllowedProviderKinds    []string
	BudgetWarningThresholds []int
	BudgetHistoryLimit      int
}

type PolicyRuleConfig struct {
	ID                     string   `json:"id"`
	Action                 string   `json:"action"`
	Reason                 string   `json:"reason"`
	Roles                  []string `json:"roles"`
	Tenants                []string `json:"tenants"`
	Providers              []string `json:"providers"`
	ProviderKinds          []string `json:"provider_kinds"`
	Models                 []string `json:"models"`
	RouteReasons           []string `json:"route_reasons"`
	MinPromptTokens        int      `json:"min_prompt_tokens"`
	MinEstimatedCostMicros int64    `json:"min_estimated_cost_micros_usd"`
	RewriteModelTo         string   `json:"rewrite_model_to"`
}

type CacheConfig struct {
	DefaultTTL time.Duration
	Backend    string
	Redis      RedisConfig
	Semantic   SemanticCacheConfig
}

type RetentionConfig struct {
	Enabled        bool
	Interval       time.Duration
	TraceSnapshots RetentionPolicy
	BudgetEvents   RetentionPolicy
	AuditEvents    RetentionPolicy
	ExactCache     RetentionPolicy
	SemanticCache  RetentionPolicy
}

type RetentionPolicy struct {
	MaxAge   time.Duration
	MaxCount int
}

type SemanticCacheConfig struct {
	Enabled                          bool
	Backend                          string
	DefaultTTL                       time.Duration
	MinSimilarity                    float64
	MaxEntries                       int
	MaxTextChars                     int
	Embedder                         string
	EmbedderProvider                 string
	EmbedderModel                    string
	EmbedderBaseURL                  string
	EmbedderAPIKey                   string
	EmbedderTimeout                  time.Duration
	PostgresVectorMode               string
	PostgresVectorCandidates         int
	PostgresVectorIndexMode          string
	PostgresVectorIndexType          string
	PostgresVectorHNSWM              int
	PostgresVectorHNSWEfConstruction int
	PostgresVectorIVFFlatLists       int
	PostgresVectorSearchEf           int
	PostgresVectorSearchProbes       int
}

type RedisConfig struct {
	Address  string
	Password string
	DB       int
	Prefix   string
	Timeout  time.Duration
}

type PostgresConfig struct {
	DSN          string
	Schema       string
	TablePrefix  string
	MaxOpenConns int
	MaxIdleConns int
}

type ProvidersConfig struct {
	OpenAICompatible []OpenAICompatibleProviderConfig
}

type PricebookConfig struct {
	UnknownModelPolicy string             `json:"unknown_model_policy"`
	Entries            []ModelPriceConfig `json:"entries"`
}

type ModelPriceConfig struct {
	Provider                             string `json:"provider"`
	Model                                string `json:"model"`
	InputMicrosUSDPerMillionTokens       int64  `json:"input_micros_usd_per_million_tokens"`
	OutputMicrosUSDPerMillionTokens      int64  `json:"output_micros_usd_per_million_tokens"`
	CachedInputMicrosUSDPerMillionTokens int64  `json:"cached_input_micros_usd_per_million_tokens"`
}

type OpenAICompatibleProviderConfig struct {
	Name         string        `json:"name"`
	Kind         string        `json:"kind"`
	Protocol     string        `json:"protocol"`
	BaseURL      string        `json:"base_url"`
	APIKey       string        `json:"api_key"`
	APIVersion   string        `json:"api_version"`
	Timeout      time.Duration `json:"timeout"`
	StubMode     bool          `json:"stub_mode"`
	StubResponse string        `json:"stub_response"`
	DefaultModel string        `json:"default_model"`
}

func LoadFromEnv() Config {
	providersCfg := loadProvidersFromEnv()
	return Config{
		Server: ServerConfig{
			Address:               getEnv("GATEWAY_ADDRESS", ":8080"),
			AuthToken:             getEnv("GATEWAY_AUTH_TOKEN", ""),
			ControlPlaneBackend:   getEnv("GATEWAY_CONTROL_PLANE_BACKEND", "none"),
			ControlPlaneFile:      getEnv("GATEWAY_CONTROL_PLANE_FILE", ""),
			ControlPlaneKey:       getEnv("GATEWAY_CONTROL_PLANE_KEY", "control-plane"),
			ControlPlaneSecretKey: getEnv("GATEWAY_CONTROL_PLANE_SECRET_KEY", ""),
			TasksBackend:          getEnv("GATEWAY_TASKS_BACKEND", "memory"),
			TaskApprovalPolicies:  splitCSV(getEnv("GATEWAY_TASK_APPROVAL_POLICIES", "shell_exec")),
			TaskQueueWorkers:      getEnvInt("GATEWAY_TASK_QUEUE_WORKERS", 1),
			TaskQueueBuffer:       getEnvInt("GATEWAY_TASK_QUEUE_BUFFER", 128),
			TraceBodyCapture:      getEnvBool("GATEWAY_TRACE_BODIES", false),
			TraceBodyMaxBytes:     getEnvInt("GATEWAY_TRACE_BODY_MAX_BYTES", 4096),
			RateLimit: RateLimitConfig{
				Enabled:           getEnvBool("GATEWAY_RATE_LIMIT_ENABLED", false),
				RequestsPerMinute: getEnvInt64("GATEWAY_RATE_LIMIT_RPM", 60),
				BurstSize:         getEnvInt64("GATEWAY_RATE_LIMIT_BURST", 0), // 0 = same as RPM
			},
		},
		Router: RouterConfig{
			DefaultModel: getEnv("GATEWAY_DEFAULT_MODEL", "gpt-5.4-mini"),
		},
		Provider: ProviderConfig{
			MaxAttempts:     getEnvInt("GATEWAY_PROVIDER_MAX_ATTEMPTS", 2),
			RetryBackoff:    getEnvDuration("GATEWAY_PROVIDER_RETRY_BACKOFF", 200*time.Millisecond),
			FailoverEnabled: getEnvBool("GATEWAY_PROVIDER_FAILOVER_ENABLED", true),
			HealthThreshold: getEnvInt("GATEWAY_PROVIDER_HEALTH_FAILURE_THRESHOLD", 3),
			HealthCooldown:  getEnvDuration("GATEWAY_PROVIDER_HEALTH_COOLDOWN", 30*time.Second),
		},
		Chat: ChatConfig{
			SessionsBackend: getEnv("GATEWAY_CHAT_SESSIONS_BACKEND", "memory"),
			SessionsFile:    getEnv("GATEWAY_CHAT_SESSIONS_FILE", ""),
			SessionsKey:     getEnv("GATEWAY_CHAT_SESSIONS_KEY", "chat-sessions"),
			SessionLimit:    getEnvInt("GATEWAY_CHAT_SESSIONS_LIMIT", 50),
		},
		OTel: OTelConfig{
			ServiceName:     getEnv("GATEWAY_OTEL_SERVICE_NAME", "hecate-gateway"),
			MetricsInterval: getEnvDuration("GATEWAY_OTEL_METRICS_INTERVAL", 30*time.Second),
			Traces: OTelSignalConfig{
				Enabled:  getEnvBool("GATEWAY_OTEL_TRACES_ENABLED", false),
				Endpoint: getEnv("GATEWAY_OTEL_TRACES_ENDPOINT", ""),
				Headers:  parseEnvMap(getEnv("GATEWAY_OTEL_TRACES_HEADERS", "")),
				Timeout:  getEnvDuration("GATEWAY_OTEL_TRACES_TIMEOUT", 5*time.Second),
			},
			Metrics: OTelSignalConfig{
				Enabled:  getEnvBool("GATEWAY_OTEL_METRICS_ENABLED", false),
				Endpoint: getEnv("GATEWAY_OTEL_METRICS_ENDPOINT", ""),
				Headers:  parseEnvMap(getEnv("GATEWAY_OTEL_METRICS_HEADERS", "")),
				Timeout:  getEnvDuration("GATEWAY_OTEL_METRICS_TIMEOUT", 5*time.Second),
			},
			Logs: OTelSignalConfig{
				Enabled:  getEnvBool("GATEWAY_OTEL_LOGS_ENABLED", false),
				Endpoint: getEnv("GATEWAY_OTEL_LOGS_ENDPOINT", ""),
				Headers:  parseEnvMap(getEnv("GATEWAY_OTEL_LOGS_HEADERS", "")),
				Timeout:  getEnvDuration("GATEWAY_OTEL_LOGS_TIMEOUT", 5*time.Second),
			},
		},
		Governor: GovernorConfig{
			DenyAll:                 getEnvBool("GATEWAY_DENY_ALL", false),
			MaxPromptTokens:         getEnvInt("GATEWAY_MAX_PROMPT_TOKENS", 64_000),
			MaxTotalBudgetMicros:    getEnvInt64("GATEWAY_MAX_BUDGET_MICROS_USD", 5_000_000),
			ModelRewriteTo:          getEnv("GATEWAY_MODEL_REWRITE_TO", ""),
			PolicyRules:             loadPolicyRulesFromEnv(),
			BudgetBackend:           getEnv("GATEWAY_BUDGET_BACKEND", "memory"),
			BudgetKey:               getEnv("GATEWAY_BUDGET_KEY", "global"),
			BudgetScope:             getEnv("GATEWAY_BUDGET_SCOPE", "global"),
			BudgetTenantFallback:    getEnv("GATEWAY_BUDGET_TENANT_FALLBACK", "anonymous"),
			RouteMode:               getEnv("GATEWAY_ROUTE_MODE", "any"),
			AllowedProviders:        splitCSV(getEnv("GATEWAY_ALLOWED_PROVIDERS", "")),
			DeniedProviders:         splitCSV(getEnv("GATEWAY_DENIED_PROVIDERS", "")),
			AllowedModels:           splitCSV(getEnv("GATEWAY_ALLOWED_MODELS", "")),
			DeniedModels:            splitCSV(getEnv("GATEWAY_DENIED_MODELS", "")),
			AllowedProviderKinds:    splitCSV(getEnv("GATEWAY_ALLOWED_PROVIDER_KINDS", "")),
			BudgetWarningThresholds: parseEnvCSVInts(getEnv("GATEWAY_BUDGET_WARNING_THRESHOLDS", "50,80,95")),
			BudgetHistoryLimit:      getEnvInt("GATEWAY_BUDGET_HISTORY_LIMIT", 20),
		},
		Cache: CacheConfig{
			DefaultTTL: getEnvDuration("GATEWAY_CACHE_TTL", 5*time.Minute),
			Backend:    getEnv("GATEWAY_CACHE_BACKEND", "memory"),
			Redis: RedisConfig{
				Address:  getEnv("REDIS_ADDRESS", "127.0.0.1:6379"),
				Password: getEnv("REDIS_PASSWORD", ""),
				DB:       getEnvInt("REDIS_DB", 0),
				Prefix:   getEnv("REDIS_PREFIX", "agent-runtime"),
				Timeout:  getEnvDuration("REDIS_TIMEOUT", 3*time.Second),
			},
			Semantic: SemanticCacheConfig{
				Enabled:                          getEnvBool("GATEWAY_SEMANTIC_CACHE_ENABLED", false),
				Backend:                          getEnv("GATEWAY_SEMANTIC_CACHE_BACKEND", "memory"),
				DefaultTTL:                       getEnvDuration("GATEWAY_SEMANTIC_CACHE_TTL", 24*time.Hour),
				MinSimilarity:                    getEnvFloat64("GATEWAY_SEMANTIC_CACHE_MIN_SIMILARITY", 0.92),
				MaxEntries:                       getEnvInt("GATEWAY_SEMANTIC_CACHE_MAX_ENTRIES", 10_000),
				MaxTextChars:                     getEnvInt("GATEWAY_SEMANTIC_CACHE_MAX_TEXT_CHARS", 8_000),
				Embedder:                         getEnv("GATEWAY_SEMANTIC_CACHE_EMBEDDER", "local_simple"),
				EmbedderProvider:                 getEnv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_PROVIDER", ""),
				EmbedderModel:                    getEnv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_MODEL", ""),
				EmbedderBaseURL:                  getEnv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_BASE_URL", ""),
				EmbedderAPIKey:                   getEnv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_API_KEY", ""),
				EmbedderTimeout:                  getEnvDuration("GATEWAY_SEMANTIC_CACHE_EMBEDDER_TIMEOUT", 30*time.Second),
				PostgresVectorMode:               getEnv("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_MODE", "auto"),
				PostgresVectorCandidates:         getEnvInt("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_CANDIDATES", 200),
				PostgresVectorIndexMode:          getEnv("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_MODE", "auto"),
				PostgresVectorIndexType:          getEnv("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_TYPE", "hnsw"),
				PostgresVectorHNSWM:              getEnvInt("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_HNSW_M", 16),
				PostgresVectorHNSWEfConstruction: getEnvInt("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_HNSW_EF_CONSTRUCTION", 64),
				PostgresVectorIVFFlatLists:       getEnvInt("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_IVFFLAT_LISTS", 100),
				PostgresVectorSearchEf:           getEnvInt("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_SEARCH_EF", 80),
				PostgresVectorSearchProbes:       getEnvInt("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_SEARCH_PROBES", 10),
			},
		},
		Retention: RetentionConfig{
			Enabled:        getEnvBool("GATEWAY_RETENTION_ENABLED", false),
			Interval:       getEnvDuration("GATEWAY_RETENTION_INTERVAL", 15*time.Minute),
			TraceSnapshots: loadRetentionPolicyFromEnv("GATEWAY_RETENTION_TRACES_", 24*time.Hour, 2000),
			BudgetEvents:   loadRetentionPolicyFromEnv("GATEWAY_RETENTION_BUDGET_EVENTS_", 30*24*time.Hour, 200),
			AuditEvents:    loadRetentionPolicyFromEnv("GATEWAY_RETENTION_AUDIT_EVENTS_", 30*24*time.Hour, 500),
			ExactCache:     loadRetentionPolicyFromEnv("GATEWAY_RETENTION_EXACT_CACHE_", 24*time.Hour, 10_000),
			SemanticCache:  loadRetentionPolicyFromEnv("GATEWAY_RETENTION_SEMANTIC_CACHE_", 7*24*time.Hour, 10_000),
		},
		Postgres: PostgresConfig{
			DSN:          getEnv("POSTGRES_DSN", ""),
			Schema:       getEnv("POSTGRES_SCHEMA", "public"),
			TablePrefix:  getEnv("POSTGRES_TABLE_PREFIX", "hecate"),
			MaxOpenConns: getEnvInt("POSTGRES_MAX_OPEN_CONNS", 10),
			MaxIdleConns: getEnvInt("POSTGRES_MAX_IDLE_CONNS", 5),
		},
		Providers: providersCfg,
		Pricebook: loadPricebookFromEnv(),
		LogLevel:  getEnv("LOG_LEVEL", "INFO"),
	}
}

func loadRetentionPolicyFromEnv(prefix string, defaultAge time.Duration, defaultCount int) RetentionPolicy {
	return RetentionPolicy{
		MaxAge:   getEnvDuration(prefix+"MAX_AGE", defaultAge),
		MaxCount: getEnvInt(prefix+"MAX_COUNT", defaultCount),
	}
}

func loadPricebookFromEnv() PricebookConfig {
	cfg := defaultPricebookConfig()

	if raw := strings.TrimSpace(getEnv("GATEWAY_PRICEBOOK_JSON", "")); raw != "" {
		var loaded PricebookConfig
		if err := json.Unmarshal([]byte(raw), &loaded); err == nil {
			if strings.TrimSpace(loaded.UnknownModelPolicy) == "" {
				loaded.UnknownModelPolicy = cfg.UnknownModelPolicy
			}
			if len(loaded.Entries) == 0 {
				loaded.Entries = cfg.Entries
			}
			return loaded
		}
	}

	if policy := strings.TrimSpace(getEnv("GATEWAY_PRICEBOOK_UNKNOWN_MODEL_POLICY", "")); policy != "" {
		cfg.UnknownModelPolicy = policy
	}
	return cfg
}

func loadPolicyRulesFromEnv() []PolicyRuleConfig {
	raw := strings.TrimSpace(getEnv("GATEWAY_POLICY_RULES_JSON", ""))
	if raw == "" {
		return nil
	}

	var rules []PolicyRuleConfig
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil
	}

	out := make([]PolicyRuleConfig, 0, len(rules))
	for _, rule := range rules {
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Action = strings.TrimSpace(rule.Action)
		rule.Reason = strings.TrimSpace(rule.Reason)
		rule.Roles = normalizeValues(rule.Roles)
		rule.Tenants = normalizeValues(rule.Tenants)
		rule.Providers = normalizeValues(rule.Providers)
		rule.ProviderKinds = normalizeValues(rule.ProviderKinds)
		rule.Models = normalizeValues(rule.Models)
		rule.RouteReasons = normalizeValues(rule.RouteReasons)
		rule.RewriteModelTo = strings.TrimSpace(rule.RewriteModelTo)
		if rule.Action == "" {
			continue
		}
		out = append(out, rule)
	}
	return out
}

func defaultPricebookConfig() PricebookConfig {
	return PricebookConfig{
		UnknownModelPolicy: "error",
		Entries: []ModelPriceConfig{
			// Seeded from OpenAI's published API pricing/model docs as of 2026-04-23.
			// Keep this list small and explicit for sane defaults, but this is not a long-term
			// source of truth. Hecate still needs a proper pricebook ingestion/update path.
			// Source: https://developers.openai.com/api/docs/models
			{
				Provider:                             "openai",
				Model:                                "gpt-5.4",
				InputMicrosUSDPerMillionTokens:       2_500_000,
				OutputMicrosUSDPerMillionTokens:      15_000_000,
				CachedInputMicrosUSDPerMillionTokens: 250_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-5.4-mini",
				InputMicrosUSDPerMillionTokens:       750_000,
				OutputMicrosUSDPerMillionTokens:      4_500_000,
				CachedInputMicrosUSDPerMillionTokens: 75_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-5.4-nano",
				InputMicrosUSDPerMillionTokens:       200_000,
				OutputMicrosUSDPerMillionTokens:      1_250_000,
				CachedInputMicrosUSDPerMillionTokens: 20_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-4.1",
				InputMicrosUSDPerMillionTokens:       2_000_000,
				OutputMicrosUSDPerMillionTokens:      8_000_000,
				CachedInputMicrosUSDPerMillionTokens: 500_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-4.1-mini",
				InputMicrosUSDPerMillionTokens:       400_000,
				OutputMicrosUSDPerMillionTokens:      1_600_000,
				CachedInputMicrosUSDPerMillionTokens: 100_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-4.1-nano",
				InputMicrosUSDPerMillionTokens:       100_000,
				OutputMicrosUSDPerMillionTokens:      400_000,
				CachedInputMicrosUSDPerMillionTokens: 25_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-4o",
				InputMicrosUSDPerMillionTokens:       2_500_000,
				OutputMicrosUSDPerMillionTokens:      10_000_000,
				CachedInputMicrosUSDPerMillionTokens: 1_250_000,
			},
			{
				Provider:                             "openai",
				Model:                                "gpt-4o-mini",
				InputMicrosUSDPerMillionTokens:       150_000,
				OutputMicrosUSDPerMillionTokens:      600_000,
				CachedInputMicrosUSDPerMillionTokens: 75_000,
			},
			{
				Provider: "openai",
				Model:    "omni-moderation",
			},
			{
				Provider: "openai",
				Model:    "omni-moderation-latest",
			},
			{
				Provider: "openai",
				Model:    "text-moderation-latest",
			},
			// Seeded from Anthropic's published pricing/docs as of 2026-04-23.
			// Source: https://www.anthropic.com/claude/sonnet
			// Claude Sonnet 4.6: $3 / MTok input, $15 / MTok output, $0.30 / MTok cache reads.
			{
				Provider:                             "anthropic",
				Model:                                "claude-sonnet-4-6",
				InputMicrosUSDPerMillionTokens:       3_000_000,
				OutputMicrosUSDPerMillionTokens:      15_000_000,
				CachedInputMicrosUSDPerMillionTokens: 300_000,
			},
			// Claude Sonnet 4: $3 / MTok input, $15 / MTok output, $0.30 / MTok cache reads.
			{
				Provider:                             "anthropic",
				Model:                                "claude-sonnet-4-20250514",
				InputMicrosUSDPerMillionTokens:       3_000_000,
				OutputMicrosUSDPerMillionTokens:      15_000_000,
				CachedInputMicrosUSDPerMillionTokens: 300_000,
			},
			// Claude Haiku 3.5: $0.80 / MTok input, $4 / MTok output, $0.08 / MTok cache reads.
			{
				Provider:                             "anthropic",
				Model:                                "claude-haiku-3-5-20241022",
				InputMicrosUSDPerMillionTokens:       800_000,
				OutputMicrosUSDPerMillionTokens:      4_000_000,
				CachedInputMicrosUSDPerMillionTokens: 80_000,
			},
			// Seeded from Groq's published model docs as of 2026-04-23.
			// Source: https://console.groq.com/docs/models
			{
				Provider:                             "groq",
				Model:                                "llama-3.3-70b-versatile",
				InputMicrosUSDPerMillionTokens:       590_000,
				OutputMicrosUSDPerMillionTokens:      790_000,
				CachedInputMicrosUSDPerMillionTokens: 0,
			},
			{
				Provider:                             "groq",
				Model:                                "llama-3.1-8b-instant",
				InputMicrosUSDPerMillionTokens:       50_000,
				OutputMicrosUSDPerMillionTokens:      80_000,
				CachedInputMicrosUSDPerMillionTokens: 0,
			},
			{
				Provider:                             "groq",
				Model:                                "openai/gpt-oss-120b",
				InputMicrosUSDPerMillionTokens:       150_000,
				OutputMicrosUSDPerMillionTokens:      600_000,
				CachedInputMicrosUSDPerMillionTokens: 0,
			},
			{
				Provider:                             "groq",
				Model:                                "openai/gpt-oss-20b",
				InputMicrosUSDPerMillionTokens:       75_000,
				OutputMicrosUSDPerMillionTokens:      300_000,
				CachedInputMicrosUSDPerMillionTokens: 0,
			},
			// Seeded from Google Gemini API pricing docs as of 2026-04-23.
			// Source: https://ai.google.dev/gemini-api/docs/pricing
			{
				Provider:                             "gemini",
				Model:                                "gemini-2.5-flash",
				InputMicrosUSDPerMillionTokens:       300_000,
				OutputMicrosUSDPerMillionTokens:      2_500_000,
				CachedInputMicrosUSDPerMillionTokens: 30_000,
			},
			{
				Provider:                             "gemini",
				Model:                                "gemini-2.5-flash-lite",
				InputMicrosUSDPerMillionTokens:       100_000,
				OutputMicrosUSDPerMillionTokens:      400_000,
				CachedInputMicrosUSDPerMillionTokens: 10_000,
			},
		},
	}
}

func loadProvidersFromEnv() ProvidersConfig {
	names := splitCSV(os.Getenv("GATEWAY_PROVIDERS"))
	if len(names) == 0 {
		names = deriveProviderNamesFromEnv()
	}
	if len(names) == 0 {
		names = []string{"openai"}
	}
	items := make([]OpenAICompatibleProviderConfig, 0, len(names))
	for _, name := range names {
		cfg, ok := providerConfigFromEnv(name)
		if !ok {
			continue
		}
		items = append(items, cfg)
	}
	normalizeProviders(items)
	return ProvidersConfig{OpenAICompatible: items}
}

func deriveProviderNamesFromEnv() []string {
	const prefix = "PROVIDER_"

	order := make([]string, 0, 4)
	seen := make(map[string]struct{})
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !strings.HasPrefix(key, prefix) {
			continue
		}
		name, ok := providerNameFromEnvKey(key)
		if !ok {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		order = append(order, name)
	}
	return order
}

func providerNameFromEnvKey(key string) (string, bool) {
	const prefix = "PROVIDER_"

	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	nameAndField := strings.TrimPrefix(key, prefix)
	for _, suffix := range []string{"_API_KEY", "_BASE_URL", "_PROTOCOL"} {
		if strings.HasSuffix(nameAndField, suffix) {
			name := strings.TrimSuffix(nameAndField, suffix)
			name = strings.ToLower(name)
			name = strings.TrimSpace(name)
			if name == "" {
				return "", false
			}
			return name, true
		}
	}
	return "", false
}

func providerConfigFromEnv(name string) (OpenAICompatibleProviderConfig, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return OpenAICompatibleProviderConfig{}, false
	}

	cfg := providerDefaults(name, getEnv("GATEWAY_DEFAULT_MODEL", "gpt-5.4-mini"))
	prefix := providerEnvPrefix(name)

	cfg.Name = getEnv(prefix+"NAME", cfg.Name)
	cfg.Kind = getEnv(prefix+"KIND", cfg.Kind)
	cfg.Protocol = getEnv(prefix+"PROTOCOL", cfg.Protocol)
	cfg.BaseURL = getEnv(prefix+"BASE_URL", cfg.BaseURL)
	cfg.APIKey = getEnv(prefix+"API_KEY", cfg.APIKey)
	cfg.APIVersion = getEnv(prefix+"API_VERSION", cfg.APIVersion)
	cfg.Timeout = getEnvDuration(prefix+"TIMEOUT", cfg.Timeout)
	cfg.StubMode = getEnvBool(prefix+"STUB_MODE", cfg.StubMode)
	cfg.StubResponse = getEnv(prefix+"STUB_RESPONSE", cfg.StubResponse)
	cfg.DefaultModel = getEnv(prefix+"DEFAULT_MODEL", cfg.DefaultModel)

	if strings.TrimSpace(cfg.BaseURL) == "" {
		return OpenAICompatibleProviderConfig{}, false
	}
	return cfg, true
}

func providerDefaults(name, globalDefaultModel string) OpenAICompatibleProviderConfig {
	if builtIn, ok := BuiltInProviderByID(name); ok {
		return builtIn.RuntimeConfig(globalDefaultModel)
	}
	return OpenAICompatibleProviderConfig{
		Name:         strings.ToLower(strings.TrimSpace(name)),
		Kind:         "cloud",
		Protocol:     "openai",
		Timeout:      30 * time.Second,
		StubMode:     false,
		StubResponse: "Stubbed response from the AI Agent Runtime MVP.",
	}
}

func providerEnvPrefix(name string) string {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, ".", "_")
	return "PROVIDER_" + normalized + "_"
}

func normalizeProviders(items []OpenAICompatibleProviderConfig) {
	for i := range items {
		if items[i].Name == "" {
			items[i].Name = "provider"
		}
		if items[i].Kind == "" {
			items[i].Kind = "cloud"
		}
		if items[i].Protocol == "" {
			items[i].Protocol = "openai"
		}
		if items[i].Timeout == 0 {
			items[i].Timeout = 30 * time.Second
		}
		if items[i].StubResponse == "" {
			items[i].StubResponse = "Stubbed response from the AI Agent Runtime MVP."
		}
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvInt64(key string, fallback int64) int64 {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnvFloat64(key string, fallback float64) float64 {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	return normalizeValues(strings.Split(value, ","))
}

func normalizeValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseEnvCSVInts(value string) []int {
	parts := splitCSV(value)
	if len(parts) == 0 {
		return nil
	}

	out := make([]int, 0, len(parts))
	for _, part := range parts {
		item, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		out = append(out, item)
	}
	return out
}

func parseEnvMap(raw string) map[string]string {
	items := splitCSV(raw)
	if len(items) == 0 {
		return nil
	}

	out := make(map[string]string, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		out[key] = value
	}
	return out
}
