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
	Address             string
	AuthToken           string
	APIKeys             []APIKeyConfig
	ControlPlaneBackend string
	ControlPlaneFile    string
	ControlPlaneKey     string
}

type APIKeyConfig struct {
	Name             string   `json:"name"`
	Key              string   `json:"key"`
	Tenant           string   `json:"tenant"`
	Role             string   `json:"role"`
	AllowedProviders []string `json:"allowed_providers"`
	AllowedModels    []string `json:"allowed_models"`
}

type RouterConfig struct {
	DefaultModel     string
	DefaultProvider  string
	Strategy         string
	FallbackProvider string
}

type ProviderConfig struct {
	MaxAttempts     int
	RetryBackoff    time.Duration
	FailoverEnabled bool
	HealthThreshold int
	HealthCooldown  time.Duration
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
	Name          string        `json:"name"`
	Kind          string        `json:"kind"`
	BaseURL       string        `json:"base_url"`
	APIKey        string        `json:"api_key"`
	Timeout       time.Duration `json:"timeout"`
	StubMode      bool          `json:"stub_mode"`
	StubResponse  string        `json:"stub_response"`
	DefaultModel  string        `json:"default_model"`
	Models        []string      `json:"models"`
	AllowAnyModel bool          `json:"allow_any_model"`
}

func LoadFromEnv() Config {
	providersCfg := loadProvidersFromEnv()
	return Config{
		Server: ServerConfig{
			Address:             getEnv("GATEWAY_ADDRESS", ":8080"),
			AuthToken:           getEnv("GATEWAY_AUTH_TOKEN", ""),
			APIKeys:             loadAPIKeysFromEnv(),
			ControlPlaneBackend: getEnv("GATEWAY_CONTROL_PLANE_BACKEND", "none"),
			ControlPlaneFile:    getEnv("GATEWAY_CONTROL_PLANE_FILE", ""),
			ControlPlaneKey:     getEnv("GATEWAY_CONTROL_PLANE_KEY", "control-plane"),
		},
		Router: RouterConfig{
			DefaultModel:     getEnv("GATEWAY_DEFAULT_MODEL", "gpt-4o-mini"),
			DefaultProvider:  getEnv("GATEWAY_DEFAULT_PROVIDER", firstProviderName(providersCfg, "openai")),
			Strategy:         getEnv("GATEWAY_ROUTER_STRATEGY", "explicit_or_default"),
			FallbackProvider: getEnv("GATEWAY_ROUTER_FALLBACK_PROVIDER", ""),
		},
		Provider: ProviderConfig{
			MaxAttempts:     getEnvInt("GATEWAY_PROVIDER_MAX_ATTEMPTS", 2),
			RetryBackoff:    getEnvDuration("GATEWAY_PROVIDER_RETRY_BACKOFF", 200*time.Millisecond),
			FailoverEnabled: getEnvBool("GATEWAY_PROVIDER_FAILOVER_ENABLED", true),
			HealthThreshold: getEnvInt("GATEWAY_PROVIDER_HEALTH_FAILURE_THRESHOLD", 3),
			HealthCooldown:  getEnvDuration("GATEWAY_PROVIDER_HEALTH_COOLDOWN", 30*time.Second),
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
			// Seeded from OpenAI's published API pricing/model docs as of 2026-04-22.
			// Keep this list small and explicit for sane defaults, but this is not a long-term
			// source of truth. Hecate still needs a proper pricebook ingestion/update path.
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
		},
	}
}

func loadAPIKeysFromEnv() []APIKeyConfig {
	raw := strings.TrimSpace(getEnv("GATEWAY_API_KEYS_JSON", ""))
	if raw == "" {
		return nil
	}

	var keys []APIKeyConfig
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil
	}

	out := make([]APIKeyConfig, 0, len(keys))
	for _, item := range keys {
		if item.Key == "" {
			continue
		}
		if item.Role == "" {
			item.Role = "tenant"
		}
		out = append(out, item)
	}
	return out
}

func loadProvidersFromEnv() ProvidersConfig {
	if raw := getEnv("GATEWAY_PROVIDERS_JSON", ""); raw != "" {
		var providersCfg []OpenAICompatibleProviderConfig
		if err := json.Unmarshal([]byte(raw), &providersCfg); err == nil && len(providersCfg) > 0 {
			normalizeProviders(providersCfg)
			return ProvidersConfig{OpenAICompatible: providersCfg}
		}
	}

	items := make([]OpenAICompatibleProviderConfig, 0, 2)
	items = append(items, OpenAICompatibleProviderConfig{
		Name:          getEnv("OPENAI_PROVIDER_NAME", "openai"),
		Kind:          getEnv("OPENAI_PROVIDER_KIND", "cloud"),
		BaseURL:       getEnv("OPENAI_BASE_URL", "https://api.openai.com"),
		APIKey:        getEnv("OPENAI_API_KEY", ""),
		Timeout:       getEnvDuration("OPENAI_TIMEOUT", 30*time.Second),
		StubMode:      getEnvBool("OPENAI_STUB_MODE", true),
		StubResponse:  getEnv("OPENAI_STUB_RESPONSE", "Stubbed response from the AI Agent Runtime MVP."),
		DefaultModel:  getEnv("OPENAI_DEFAULT_MODEL", getEnv("GATEWAY_DEFAULT_MODEL", "gpt-4o-mini")),
		Models:        splitCSV(getEnv("OPENAI_MODELS", "")),
		AllowAnyModel: getEnvBool("OPENAI_ALLOW_ANY_MODEL", true),
	})

	if getEnvBool("LOCAL_PROVIDER_ENABLED", false) {
		items = append(items, OpenAICompatibleProviderConfig{
			Name:          getEnv("LOCAL_PROVIDER_NAME", "local"),
			Kind:          getEnv("LOCAL_PROVIDER_KIND", "local"),
			BaseURL:       getEnv("LOCAL_PROVIDER_BASE_URL", "http://127.0.0.1:11434"),
			APIKey:        getEnv("LOCAL_PROVIDER_API_KEY", ""),
			Timeout:       getEnvDuration("LOCAL_PROVIDER_TIMEOUT", 30*time.Second),
			StubMode:      getEnvBool("LOCAL_PROVIDER_STUB_MODE", false),
			StubResponse:  getEnv("LOCAL_PROVIDER_STUB_RESPONSE", "Stubbed local provider response."),
			DefaultModel:  getEnv("LOCAL_PROVIDER_DEFAULT_MODEL", ""),
			Models:        splitCSV(getEnv("LOCAL_PROVIDER_MODELS", "")),
			AllowAnyModel: getEnvBool("LOCAL_PROVIDER_ALLOW_ANY_MODEL", false),
		})
	}

	normalizeProviders(items)
	return ProvidersConfig{OpenAICompatible: items}
}

func normalizeProviders(items []OpenAICompatibleProviderConfig) {
	for i := range items {
		if items[i].Name == "" {
			items[i].Name = "provider"
		}
		if items[i].Kind == "" {
			items[i].Kind = "cloud"
		}
		if items[i].Timeout == 0 {
			items[i].Timeout = 30 * time.Second
		}
		if items[i].StubResponse == "" {
			items[i].StubResponse = "Stubbed response from the AI Agent Runtime MVP."
		}
	}
}

func firstProviderName(cfg ProvidersConfig, fallback string) string {
	if len(cfg.OpenAICompatible) == 0 {
		return fallback
	}
	return cfg.OpenAICompatible[0].Name
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
