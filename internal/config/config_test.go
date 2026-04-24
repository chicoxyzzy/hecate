package config

import "testing"

func TestLoadFromEnvSemanticAndPostgresSettings(t *testing.T) {
	t.Setenv("GATEWAY_SEMANTIC_CACHE_ENABLED", "true")
	t.Setenv("GATEWAY_SEMANTIC_CACHE_BACKEND", "postgres")
	t.Setenv("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_MODE", "required")
	t.Setenv("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_TYPE", "ivfflat")
	t.Setenv("GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_SEARCH_PROBES", "42")
	t.Setenv("POSTGRES_DSN", "postgres://user:pass@localhost:5432/hecate?sslmode=disable")
	t.Setenv("POSTGRES_SCHEMA", "runtime")
	t.Setenv("POSTGRES_TABLE_PREFIX", "gateway")

	cfg := LoadFromEnv()
	if !cfg.Cache.Semantic.Enabled {
		t.Fatal("semantic cache enabled = false, want true")
	}
	if cfg.Cache.Semantic.Backend != "postgres" {
		t.Fatalf("semantic backend = %q, want postgres", cfg.Cache.Semantic.Backend)
	}
	if cfg.Cache.Semantic.PostgresVectorMode != "required" {
		t.Fatalf("vector mode = %q, want required", cfg.Cache.Semantic.PostgresVectorMode)
	}
	if cfg.Cache.Semantic.PostgresVectorIndexType != "ivfflat" {
		t.Fatalf("index type = %q, want ivfflat", cfg.Cache.Semantic.PostgresVectorIndexType)
	}
	if cfg.Cache.Semantic.PostgresVectorSearchProbes != 42 {
		t.Fatalf("search probes = %d, want 42", cfg.Cache.Semantic.PostgresVectorSearchProbes)
	}
	if cfg.Postgres.Schema != "runtime" {
		t.Fatalf("schema = %q, want runtime", cfg.Postgres.Schema)
	}
	if cfg.Postgres.TablePrefix != "gateway" {
		t.Fatalf("table prefix = %q, want gateway", cfg.Postgres.TablePrefix)
	}
}

func TestLoadFromEnvUsesCurrentOpenAIDefaultModel(t *testing.T) {
	t.Setenv("GATEWAY_DEFAULT_MODEL", "")

	cfg := LoadFromEnv()
	if cfg.Router.DefaultModel != "gpt-5.4-mini" {
		t.Fatalf("default model = %q, want gpt-5.4-mini", cfg.Router.DefaultModel)
	}
}

func TestLoadFromEnvPricebookSettings(t *testing.T) {
	t.Setenv("GATEWAY_PRICEBOOK_UNKNOWN_MODEL_POLICY", "zero")
	t.Setenv("GATEWAY_PRICEBOOK_JSON", `{
		"unknown_model_policy":"error",
		"entries":[
			{
				"provider":"openai",
				"model":"gpt-4o-mini",
				"input_micros_usd_per_million_tokens":150000,
				"output_micros_usd_per_million_tokens":600000,
				"cached_input_micros_usd_per_million_tokens":75000
			}
		]
	}`)

	cfg := LoadFromEnv()
	if cfg.Pricebook.UnknownModelPolicy != "error" {
		t.Fatalf("unknown model policy = %q, want error from json override", cfg.Pricebook.UnknownModelPolicy)
	}
	if len(cfg.Pricebook.Entries) != 1 {
		t.Fatalf("pricebook entries = %d, want 1", len(cfg.Pricebook.Entries))
	}
	if cfg.Pricebook.Entries[0].Model != "gpt-4o-mini" {
		t.Fatalf("pricebook entry model = %q, want gpt-4o-mini", cfg.Pricebook.Entries[0].Model)
	}
}

func TestDefaultPricebookIncludesCurrentProviderDefaults(t *testing.T) {
	t.Parallel()

	cfg := defaultPricebookConfig()

	for _, tt := range []struct {
		provider string
		model    string
	}{
		{provider: "openai", model: "gpt-5.4-mini"},
		{provider: "openai", model: "gpt-5.4"},
		{provider: "anthropic", model: "claude-sonnet-4-6"},
		{provider: "groq", model: "llama-3.3-70b-versatile"},
		{provider: "gemini", model: "gemini-2.5-flash"},
	} {
		tt := tt
		t.Run(tt.provider+"/"+tt.model, func(t *testing.T) {
			t.Parallel()

			for _, entry := range cfg.Entries {
				if entry.Provider == tt.provider && entry.Model == tt.model {
					if entry.InputMicrosUSDPerMillionTokens <= 0 || entry.OutputMicrosUSDPerMillionTokens <= 0 {
						t.Fatalf("pricebook entry for %s/%s has non-positive pricing: %#v", tt.provider, tt.model, entry)
					}
					return
				}
			}
			t.Fatalf("pricebook entry for %s/%s not found", tt.provider, tt.model)
		})
	}
}

func TestLoadFromEnvPolicyRules(t *testing.T) {
	t.Setenv("GATEWAY_POLICY_RULES_JSON", `[
		{
			"id":"tenant-local-rewrite",
			"action":"rewrite_model",
			"reason":"prefer cheaper model for tenant",
			"tenants":["team-a"],
			"models":["gpt-4o"],
			"rewrite_model_to":"gpt-4o-mini"
		},
		{
			"id":"block-expensive-cloud",
			"action":"deny",
			"provider_kinds":["cloud"],
			"min_estimated_cost_micros_usd":1000000
		}
	]`)

	cfg := LoadFromEnv()
	if len(cfg.Governor.PolicyRules) != 2 {
		t.Fatalf("policy rules = %d, want 2", len(cfg.Governor.PolicyRules))
	}
	if cfg.Governor.PolicyRules[0].ID != "tenant-local-rewrite" {
		t.Fatalf("first rule id = %q, want tenant-local-rewrite", cfg.Governor.PolicyRules[0].ID)
	}
	if cfg.Governor.PolicyRules[1].MinEstimatedCostMicros != 1_000_000 {
		t.Fatalf("second rule min estimated cost = %d, want 1000000", cfg.Governor.PolicyRules[1].MinEstimatedCostMicros)
	}
}

func TestSplitCSVTrimsAndDropsEmptyValues(t *testing.T) {
	t.Parallel()

	got := splitCSV(" openai, , ollama ,")
	want := []string{"openai", "ollama"}
	if len(got) != len(want) {
		t.Fatalf("len(splitCSV) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitCSV[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadProvidersFromEnvUsesGenericProviderPrefixes(t *testing.T) {
	t.Setenv("PROVIDER_OPENAI_API_KEY", "openai-secret")
	t.Setenv("PROVIDER_OLLAMA_BASE_URL", "http://127.0.0.1:11434/v1")
	t.Setenv("PROVIDER_ANTHROPIC_API_KEY", "anthropic-secret")

	cfg := LoadFromEnv()
	if len(cfg.Providers.OpenAICompatible) != len(BuiltInProviders()) {
		t.Fatalf("provider count = %d, want %d built-ins", len(cfg.Providers.OpenAICompatible), len(BuiltInProviders()))
	}
	openai, ok := testProviderByName(cfg.Providers.OpenAICompatible, "openai")
	if !ok {
		t.Fatal("openai provider missing")
	}
	if openai.APIKey != "openai-secret" {
		t.Fatalf("openai api key = %q, want openai-secret", openai.APIKey)
	}
	ollama, ok := testProviderByName(cfg.Providers.OpenAICompatible, "ollama")
	if !ok {
		t.Fatal("ollama provider missing")
	}
	if ollama.Kind != "local" {
		t.Fatalf("ollama kind = %q, want local", ollama.Kind)
	}
	anthropic, ok := testProviderByName(cfg.Providers.OpenAICompatible, "anthropic")
	if !ok {
		t.Fatal("anthropic provider missing")
	}
	if anthropic.Protocol != "anthropic" {
		t.Fatalf("anthropic protocol = %q, want anthropic", anthropic.Protocol)
	}
}

func TestBuiltInProviderCatalogDefaults(t *testing.T) {
	t.Parallel()

	openai, ok := BuiltInProviderByID("openai")
	if !ok {
		t.Fatal("BuiltInProviderByID(openai) = not found")
	}
	if openai.DefaultModel != "gpt-5.4-mini" {
		t.Fatalf("openai built-in default model = %q, want gpt-5.4-mini", openai.DefaultModel)
	}
	if got := openai.RuntimeConfig("gpt-5.4").DefaultModel; got != "gpt-5.4" {
		t.Fatalf("openai runtime default model = %q, want overridden global default", got)
	}

	anthropic, ok := BuiltInProviderByID("anthropic")
	if !ok {
		t.Fatal("BuiltInProviderByID(anthropic) = not found")
	}
	if anthropic.Protocol != "anthropic" {
		t.Fatalf("anthropic protocol = %q, want anthropic", anthropic.Protocol)
	}
	if got := anthropic.RuntimeConfig("ignored").DefaultModel; got != "claude-sonnet-4-6" {
		t.Fatalf("anthropic default model = %q, want claude-sonnet-4-6", got)
	}

	deepseek, ok := BuiltInProviderByID("deepseek")
	if !ok {
		t.Fatal("BuiltInProviderByID(deepseek) = not found")
	}
	if deepseek.Protocol != "openai" {
		t.Fatalf("deepseek protocol = %q, want openai", deepseek.Protocol)
	}
	if deepseek.BaseURL != "https://api.deepseek.com/v1" {
		t.Fatalf("deepseek base url = %q, want https://api.deepseek.com/v1", deepseek.BaseURL)
	}
	if got := deepseek.RuntimeConfig("ignored").DefaultModel; got != "deepseek-chat" {
		t.Fatalf("deepseek default model = %q, want deepseek-chat", got)
	}

	xai, ok := BuiltInProviderByID("xai")
	if !ok {
		t.Fatal("BuiltInProviderByID(xai) = not found")
	}
	if xai.Protocol != "openai" {
		t.Fatalf("xai protocol = %q, want openai", xai.Protocol)
	}
	if xai.BaseURL != "https://api.x.ai/v1" {
		t.Fatalf("xai base url = %q, want https://api.x.ai/v1", xai.BaseURL)
	}
	if got := xai.RuntimeConfig("ignored").DefaultModel; got != "grok-3-mini" {
		t.Fatalf("xai default model = %q, want grok-3-mini", got)
	}
	if alias, ok := BuiltInProviderByID("grok"); !ok || alias.ID != "xai" {
		t.Fatalf("BuiltInProviderByID(grok) = %#v, %v; want alias id xai", alias, ok)
	}

	mistral, ok := BuiltInProviderByID("mistral")
	if !ok {
		t.Fatal("BuiltInProviderByID(mistral) = not found")
	}
	if mistral.Protocol != "openai" {
		t.Fatalf("mistral protocol = %q, want openai", mistral.Protocol)
	}
	if mistral.BaseURL != "https://api.mistral.ai/v1" {
		t.Fatalf("mistral base url = %q, want https://api.mistral.ai/v1", mistral.BaseURL)
	}
	if got := mistral.RuntimeConfig("ignored").DefaultModel; got != "mistral-small-latest" {
		t.Fatalf("mistral default model = %q, want mistral-small-latest", got)
	}

	for _, id := range []string{"ollama", "LM Studio", "localai", "llamacpp"} {
		local, ok := BuiltInProviderByID(id)
		if !ok {
			t.Fatalf("BuiltInProviderByID(%s) = not found", id)
		}
		if local.DefaultModel != "" {
			t.Fatalf("%s built-in default model = %q, want empty for discovery", local.ID, local.DefaultModel)
		}
		if got := local.RuntimeConfig("ignored").DefaultModel; got != "" {
			t.Fatalf("%s runtime default model = %q, want empty for discovery", local.ID, got)
		}
	}
}

func TestLoadProvidersFromEnvIncludesCustomProviderFromCoreEnvKeys(t *testing.T) {
	t.Setenv("PROVIDER_CUSTOM_BASE_URL", "https://example.com/v1")
	t.Setenv("PROVIDER_CUSTOM_API_KEY", "custom-secret")

	cfg := LoadFromEnv()
	if len(cfg.Providers.OpenAICompatible) != len(BuiltInProviders())+1 {
		t.Fatalf("provider count = %d, want %d built-ins + custom", len(cfg.Providers.OpenAICompatible), len(BuiltInProviders()))
	}
	custom, ok := testProviderByName(cfg.Providers.OpenAICompatible, "custom")
	if !ok {
		t.Fatal("custom provider missing")
	}
	if custom.BaseURL != "https://example.com/v1" {
		t.Fatalf("custom base_url = %q, want https://example.com/v1", custom.BaseURL)
	}
	if custom.APIKey != "custom-secret" {
		t.Fatalf("custom api key = %q, want custom-secret", custom.APIKey)
	}
}

func TestLoadProvidersFromEnvSupportsLegacyGrokEnvAlias(t *testing.T) {
	t.Setenv("PROVIDER_GROK_API_KEY", "legacy-grok-secret")

	cfg := LoadFromEnv()
	xai, ok := testProviderByName(cfg.Providers.OpenAICompatible, "xai")
	if !ok {
		t.Fatal("xai provider missing")
	}
	if xai.APIKey != "legacy-grok-secret" {
		t.Fatalf("xai api key = %q, want legacy-grok-secret", xai.APIKey)
	}
}

func testProviderByName(items []OpenAICompatibleProviderConfig, name string) (OpenAICompatibleProviderConfig, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return OpenAICompatibleProviderConfig{}, false
}
