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
	Governor  GovernorConfig
	Cache     CacheConfig
	Postgres  PostgresConfig
	Providers ProvidersConfig
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

type GovernorConfig struct {
	DenyAll              bool
	MaxPromptTokens      int
	MaxTotalBudgetMicros int64
	ModelRewriteTo       string
	BudgetBackend        string
	BudgetKey            string
	BudgetScope          string
	BudgetTenantFallback string
	RouteMode            string
	AllowedProviders     []string
	DeniedProviders      []string
	AllowedModels        []string
	DeniedModels         []string
	AllowedProviderKinds []string
}

type CacheConfig struct {
	DefaultTTL time.Duration
	Backend    string
	Redis      RedisConfig
	Semantic   SemanticCacheConfig
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

type OpenAICompatibleProviderConfig struct {
	Name                                 string        `json:"name"`
	Kind                                 string        `json:"kind"`
	BaseURL                              string        `json:"base_url"`
	APIKey                               string        `json:"api_key"`
	Timeout                              time.Duration `json:"timeout"`
	StubMode                             bool          `json:"stub_mode"`
	StubResponse                         string        `json:"stub_response"`
	DefaultModel                         string        `json:"default_model"`
	Models                               []string      `json:"models"`
	AllowAnyModel                        bool          `json:"allow_any_model"`
	InputMicrosUSDPerMillionTokens       int64         `json:"input_micros_usd_per_million_tokens"`
	OutputMicrosUSDPerMillionTokens      int64         `json:"output_micros_usd_per_million_tokens"`
	CachedInputMicrosUSDPerMillionTokens int64         `json:"cached_input_micros_usd_per_million_tokens"`
}

func LoadFromEnv() Config {
	providersCfg := loadProvidersFromEnv()
	return Config{
		Server: ServerConfig{
			Address:             getEnv("GATEWAY_ADDRESS", ":8080"),
			AuthToken:           os.Getenv("GATEWAY_AUTH_TOKEN"),
			APIKeys:             loadAPIKeysFromEnv(),
			ControlPlaneBackend: getEnv("GATEWAY_CONTROL_PLANE_BACKEND", "none"),
			ControlPlaneFile:    os.Getenv("GATEWAY_CONTROL_PLANE_FILE"),
			ControlPlaneKey:     getEnv("GATEWAY_CONTROL_PLANE_KEY", "control-plane"),
		},
		Router: RouterConfig{
			DefaultModel:     getEnv("GATEWAY_DEFAULT_MODEL", "gpt-4o-mini"),
			DefaultProvider:  getEnv("GATEWAY_DEFAULT_PROVIDER", firstProviderName(providersCfg, "openai")),
			Strategy:         getEnv("GATEWAY_ROUTER_STRATEGY", "explicit_or_default"),
			FallbackProvider: os.Getenv("GATEWAY_ROUTER_FALLBACK_PROVIDER"),
		},
		Governor: GovernorConfig{
			DenyAll:              getEnvBool("GATEWAY_DENY_ALL", false),
			MaxPromptTokens:      getEnvInt("GATEWAY_MAX_PROMPT_TOKENS", 64_000),
			MaxTotalBudgetMicros: getEnvInt64("GATEWAY_MAX_BUDGET_MICROS_USD", 5_000_000),
			ModelRewriteTo:       os.Getenv("GATEWAY_MODEL_REWRITE_TO"),
			BudgetBackend:        getEnv("GATEWAY_BUDGET_BACKEND", "memory"),
			BudgetKey:            getEnv("GATEWAY_BUDGET_KEY", "global"),
			BudgetScope:          getEnv("GATEWAY_BUDGET_SCOPE", "global"),
			BudgetTenantFallback: getEnv("GATEWAY_BUDGET_TENANT_FALLBACK", "anonymous"),
			RouteMode:            getEnv("GATEWAY_ROUTE_MODE", "any"),
			AllowedProviders:     splitCSV(os.Getenv("GATEWAY_ALLOWED_PROVIDERS")),
			DeniedProviders:      splitCSV(os.Getenv("GATEWAY_DENIED_PROVIDERS")),
			AllowedModels:        splitCSV(os.Getenv("GATEWAY_ALLOWED_MODELS")),
			DeniedModels:         splitCSV(os.Getenv("GATEWAY_DENIED_MODELS")),
			AllowedProviderKinds: splitCSV(os.Getenv("GATEWAY_ALLOWED_PROVIDER_KINDS")),
		},
		Cache: CacheConfig{
			DefaultTTL: getEnvDuration("GATEWAY_CACHE_TTL", 5*time.Minute),
			Backend:    getEnv("GATEWAY_CACHE_BACKEND", "memory"),
			Redis: RedisConfig{
				Address:  getEnv("REDIS_ADDRESS", "127.0.0.1:6379"),
				Password: os.Getenv("REDIS_PASSWORD"),
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
				EmbedderProvider:                 os.Getenv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_PROVIDER"),
				EmbedderModel:                    os.Getenv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_MODEL"),
				EmbedderBaseURL:                  os.Getenv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_BASE_URL"),
				EmbedderAPIKey:                   os.Getenv("GATEWAY_SEMANTIC_CACHE_EMBEDDER_API_KEY"),
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
		Postgres: PostgresConfig{
			DSN:          os.Getenv("POSTGRES_DSN"),
			Schema:       getEnv("POSTGRES_SCHEMA", "public"),
			TablePrefix:  getEnv("POSTGRES_TABLE_PREFIX", "hecate"),
			MaxOpenConns: getEnvInt("POSTGRES_MAX_OPEN_CONNS", 10),
			MaxIdleConns: getEnvInt("POSTGRES_MAX_IDLE_CONNS", 5),
		},
		Providers: providersCfg,
		LogLevel:  getEnv("LOG_LEVEL", "INFO"),
	}
}

func loadAPIKeysFromEnv() []APIKeyConfig {
	raw := strings.TrimSpace(os.Getenv("GATEWAY_API_KEYS_JSON"))
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
	if raw := os.Getenv("GATEWAY_PROVIDERS_JSON"); raw != "" {
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
		APIKey:        os.Getenv("OPENAI_API_KEY"),
		Timeout:       getEnvDuration("OPENAI_TIMEOUT", 30*time.Second),
		StubMode:      getEnvBool("OPENAI_STUB_MODE", true),
		StubResponse:  getEnv("OPENAI_STUB_RESPONSE", "Stubbed response from the AI Agent Runtime MVP."),
		DefaultModel:  getEnv("OPENAI_DEFAULT_MODEL", getEnv("GATEWAY_DEFAULT_MODEL", "gpt-4o-mini")),
		Models:        splitCSV(os.Getenv("OPENAI_MODELS")),
		AllowAnyModel: getEnvBool("OPENAI_ALLOW_ANY_MODEL", true),
	})

	if getEnvBool("LOCAL_PROVIDER_ENABLED", false) {
		items = append(items, OpenAICompatibleProviderConfig{
			Name:          getEnv("LOCAL_PROVIDER_NAME", "local"),
			Kind:          getEnv("LOCAL_PROVIDER_KIND", "local"),
			BaseURL:       getEnv("LOCAL_PROVIDER_BASE_URL", "http://127.0.0.1:11434"),
			APIKey:        os.Getenv("LOCAL_PROVIDER_API_KEY"),
			Timeout:       getEnvDuration("LOCAL_PROVIDER_TIMEOUT", 30*time.Second),
			StubMode:      getEnvBool("LOCAL_PROVIDER_STUB_MODE", false),
			StubResponse:  getEnv("LOCAL_PROVIDER_STUB_RESPONSE", "Stubbed local provider response."),
			DefaultModel:  os.Getenv("LOCAL_PROVIDER_DEFAULT_MODEL"),
			Models:        splitCSV(os.Getenv("LOCAL_PROVIDER_MODELS")),
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
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
