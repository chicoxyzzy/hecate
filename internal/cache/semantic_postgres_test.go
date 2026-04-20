package cache

import "testing"

func TestFormatPGVector(t *testing.T) {
	t.Parallel()

	got := formatPGVector([]float64{0.5, 1, -0.25})
	want := "[0.5,1,-0.25]"
	if got != want {
		t.Fatalf("formatPGVector() = %q, want %q", got, want)
	}
}

func TestNormalizeANNIndexType(t *testing.T) {
	t.Parallel()

	if got := normalizeANNIndexType("ivfflat", "hnsw"); got != "ivfflat" {
		t.Fatalf("normalizeANNIndexType(ivfflat) = %q, want ivfflat", got)
	}
	if got := normalizeANNIndexType("weird", "hnsw"); got != "hnsw" {
		t.Fatalf("normalizeANNIndexType(weird) = %q, want hnsw", got)
	}
}

func TestSemanticANNIndexName(t *testing.T) {
	t.Parallel()

	if got := semanticANNIndexName("hnsw"); got != "hecate_cache_semantic_hnsw_idx" {
		t.Fatalf("semanticANNIndexName(hnsw) = %q", got)
	}
	if got := semanticANNIndexName("ivfflat"); got != "hecate_cache_semantic_ivfflat_idx" {
		t.Fatalf("semanticANNIndexName(ivfflat) = %q", got)
	}
}
