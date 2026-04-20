package cache

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestLocalSimpleEmbedderProducesStableVectors(t *testing.T) {
	t.Parallel()

	embedder := LocalSimpleEmbedder{MaxTextChars: 1024}
	left, err := embedder.Embed(context.Background(), "Explain channels and goroutines in Go")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	right, err := embedder.Embed(context.Background(), "Explain goroutines and channels in Go")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if len(left) == 0 || len(right) == 0 {
		t.Fatal("Embed() returned empty vector")
	}
	if got := cosineSimilarity(left, right); got < 0.6 {
		t.Fatalf("cosineSimilarity() = %f, want >= 0.6", got)
	}
}

func TestOpenAICompatibleEmbedderCallsEmbeddingsEndpoint(t *testing.T) {
	t.Parallel()

	var authHeader string
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != "http://example.test/v1/embeddings" {
				t.Fatalf("url = %q, want http://example.test/v1/embeddings", r.URL.String())
			}
			authHeader = r.Header.Get("Authorization")

			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if got := string(body); got != `{"model":"nomic-embed-text","input":["hello world"]}` {
				t.Fatalf("request body = %q, want embeddings payload", got)
			}

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"data":[{"embedding":[3,4]}]}`)),
			}, nil
		}),
	}

	embedder := NewOpenAICompatibleEmbedder(OpenAICompatibleEmbedderConfig{
		Name:       "test",
		BaseURL:    "http://example.test",
		APIKey:     "secret",
		Model:      "nomic-embed-text",
		HTTPClient: client,
	})
	vector, err := embedder.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if authHeader != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", authHeader)
	}
	if len(vector) != 2 {
		t.Fatalf("len(vector) = %d, want 2", len(vector))
	}
	if vector[0] != 0.6 || vector[1] != 0.8 {
		t.Fatalf("normalized vector = %#v, want [0.6 0.8]", vector)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
