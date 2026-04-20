package config

import "testing"

func TestLoadAPIKeysFromEnvDefaultsRoleAndSkipsEmptyKey(t *testing.T) {
	t.Setenv("GATEWAY_API_KEYS_JSON", `[{"name":"tenant-a","key":"secret-a","tenant":"acme"},{"name":"missing","key":""}]`)

	keys := loadAPIKeysFromEnv()
	if len(keys) != 1 {
		t.Fatalf("len(keys) = %d, want 1", len(keys))
	}
	if keys[0].Role != "tenant" {
		t.Fatalf("role = %q, want tenant", keys[0].Role)
	}
	if keys[0].Tenant != "acme" {
		t.Fatalf("tenant = %q, want acme", keys[0].Tenant)
	}
}

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
