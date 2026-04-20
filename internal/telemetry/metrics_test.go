package telemetry

import (
	"strings"
	"testing"
)

func TestMetricsRecordChatTracksCacheAndSemanticDetails(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	metrics.RecordChat("ollama", "local", true, "semantic", "postgres_pgvector", "hnsw", 123)

	snapshot := metrics.Snapshot()
	if snapshot.CacheTypeRequests["semantic"] != 1 {
		t.Fatalf("cache type semantic = %d, want 1", snapshot.CacheTypeRequests["semantic"])
	}
	if snapshot.SemanticStrategyHits["postgres_pgvector"] != 1 {
		t.Fatalf("semantic strategy hits = %d, want 1", snapshot.SemanticStrategyHits["postgres_pgvector"])
	}
	if snapshot.SemanticIndexHits["hnsw"] != 1 {
		t.Fatalf("semantic index hits = %d, want 1", snapshot.SemanticIndexHits["hnsw"])
	}
}

func TestRenderPrometheusIncludesSemanticMetrics(t *testing.T) {
	t.Parallel()

	output := RenderPrometheus(Snapshot{
		CacheTypeRequests: map[string]int64{
			"semantic": 2,
		},
		SemanticStrategyHits: map[string]int64{
			"postgres_pgvector": 2,
		},
		SemanticIndexHits: map[string]int64{
			"hnsw": 2,
		},
	}, ProviderHealthSnapshot{})

	for _, needle := range []string{
		`gateway_cache_type_requests_total{cache_type="semantic"} 2`,
		`gateway_semantic_strategy_hits_total{strategy="postgres_pgvector"} 2`,
		`gateway_semantic_index_hits_total{index_type="hnsw"} 2`,
	} {
		if !strings.Contains(output, needle) {
			t.Fatalf("RenderPrometheus() missing %q in output:\n%s", needle, output)
		}
	}
}
