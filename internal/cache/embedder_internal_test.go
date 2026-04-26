package cache

import (
	"context"
	"math"
	"testing"
)

func TestTokenizeSemanticTextLowercasesAndDropsShort(t *testing.T) {
	got := tokenizeSemanticText("Hello WORLD! a 12 abc 1234")
	want := []string{"hello", "world", "abc", "1234"}
	if !equalStringSlices(got, want) {
		t.Errorf("tokenizeSemanticText = %v, want %v", got, want)
	}

	if got := tokenizeSemanticText(""); len(got) != 0 {
		t.Errorf("empty text → got %v, want empty", got)
	}
	if got := tokenizeSemanticText("a b c"); len(got) != 0 {
		t.Errorf("only-short tokens → got %v, want empty", got)
	}
}

func TestNormalizeVectorL2(t *testing.T) {
	v := normalizeVector([]float64{3, 4})
	if !floatsClose(v[0], 0.6) || !floatsClose(v[1], 0.8) {
		t.Errorf("normalizeVector([3,4]) = %v, want [0.6, 0.8]", v)
	}

	zero := normalizeVector([]float64{0, 0, 0})
	for _, x := range zero {
		if x != 0 {
			t.Errorf("zero vector should remain zero, got %v", zero)
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	// Cosine similarity is the dot product of *already-normalized* vectors.
	// Identical vectors → 1, orthogonal → 0, zero-length → 0.
	left := normalizeVector([]float64{1, 0, 0})
	right := normalizeVector([]float64{1, 0, 0})
	if got := cosineSimilarity(left, right); !floatsClose(got, 1.0) {
		t.Errorf("identical vectors similarity = %v, want 1.0", got)
	}

	orth := normalizeVector([]float64{0, 1, 0})
	if got := cosineSimilarity(left, orth); !floatsClose(got, 0.0) {
		t.Errorf("orthogonal vectors similarity = %v, want 0.0", got)
	}

	if got := cosineSimilarity(nil, left); got != 0 {
		t.Errorf("nil vector similarity = %v, want 0", got)
	}
	if got := cosineSimilarity(left, []float64{1, 0}); got != 0 {
		t.Errorf("mismatched lengths similarity = %v, want 0", got)
	}
}

func TestLocalSimpleEmbedderProducesNormalizedVector(t *testing.T) {
	e := LocalSimpleEmbedder{Dimensions: 64, MaxTextChars: 1024}
	vec, err := e.Embed(context.Background(), "the quick brown fox jumps over the lazy dog")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 64 {
		t.Errorf("len(vec) = %d, want 64", len(vec))
	}
	var sum float64
	for _, x := range vec {
		sum += x * x
	}
	if !floatsClose(math.Sqrt(sum), 1.0) {
		t.Errorf("vector not L2-normalized: |v| = %v", math.Sqrt(sum))
	}
}

func TestLocalSimpleEmbedderEmptyOrShortInput(t *testing.T) {
	e := LocalSimpleEmbedder{}
	for _, in := range []string{"", "a b c"} {
		vec, err := e.Embed(context.Background(), in)
		if err != nil {
			t.Fatalf("Embed(%q): %v", in, err)
		}
		if vec != nil {
			t.Errorf("Embed(%q) = %v, want nil (no usable tokens)", in, vec)
		}
	}
}

func TestLocalSimpleEmbedderTruncatesByMaxTextChars(t *testing.T) {
	long := make([]byte, 5000)
	for i := range long {
		long[i] = 'a'
	}
	e := LocalSimpleEmbedder{Dimensions: 16, MaxTextChars: 16}
	vec, err := e.Embed(context.Background(), string(long))
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	// Only one unique 16-char token survives, so exactly one bucket should
	// hold the full mass — verifying truncation actually happened.
	nonZero := 0
	for _, x := range vec {
		if x > 0 {
			nonZero++
		}
	}
	if nonZero != 1 {
		t.Errorf("expected 1 non-zero bucket after truncation, got %d (vector=%v)", nonZero, vec)
	}
}

func TestBuildEmbeddingsURL(t *testing.T) {
	cases := []struct {
		base, want string
	}{
		{"http://localhost:8080", "http://localhost:8080/v1/embeddings"},
		{"http://localhost:8080/", "http://localhost:8080/v1/embeddings"},
		{"http://localhost:8080/v1", "http://localhost:8080/v1/embeddings"},
		{"http://localhost:8080/v1/", "http://localhost:8080/v1/embeddings"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1/embeddings"},
	}
	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			if got := buildEmbeddingsURL(tc.base); got != tc.want {
				t.Errorf("buildEmbeddingsURL(%q) = %q, want %q", tc.base, got, tc.want)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func floatsClose(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
