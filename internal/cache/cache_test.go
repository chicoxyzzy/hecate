package cache

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestStableKeyBuilderKey(t *testing.T) {
	t.Parallel()

	builder := StableKeyBuilder{}
	reqA := types.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []types.Message{
			{Role: "system", Content: "Be concise."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   64,
		Temperature: 0.2,
	}
	reqB := reqA
	reqB.RequestID = "ignored-for-cache"

	keyA, err := builder.Key(reqA)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	keyB, err := builder.Key(reqB)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if keyA != keyB {
		t.Fatalf("Key() mismatch: %s != %s", keyA, keyB)
	}
}

func TestStableKeyBuilderCanonicalizesDatedModels(t *testing.T) {
	t.Parallel()

	builder := StableKeyBuilder{}
	base := types.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}
	dated := base
	dated.Model = "gpt-4o-mini-2024-07-18"

	baseKey, err := builder.Key(base)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	datedKey, err := builder.Key(dated)
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if baseKey != datedKey {
		t.Fatalf("canonicalized keys mismatch: %s != %s", baseKey, datedKey)
	}
}

func TestMemorySemanticStoreFindsSimilarPromptWithinNamespace(t *testing.T) {
	t.Parallel()

	store := NewMemorySemanticStore(time.Hour, 100, LocalSimpleEmbedder{})
	response := &types.ChatResponse{Model: "llama3.1:8b"}
	if err := store.Set(context.Background(), SemanticEntry{
		Namespace: "model:llama3.1:8b|provider:ollama|tenant:team-a",
		Text:      "user: explain go channels and goroutines",
		Response:  response,
	}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	match, ok := store.Search(context.Background(), SemanticQuery{
		Namespace:     "model:llama3.1:8b|provider:ollama|tenant:team-a",
		Text:          "user: explain goroutines and channels in go",
		MinSimilarity: 0.6,
		MaxTextChars:  1024,
	})
	if !ok {
		t.Fatal("Search() ok = false, want true")
	}
	if match.Response.Model != "llama3.1:8b" {
		t.Fatalf("match model = %q, want llama3.1:8b", match.Response.Model)
	}
}

func TestMemorySemanticStoreIsolatesNamespaces(t *testing.T) {
	t.Parallel()

	store := NewMemorySemanticStore(time.Hour, 100, LocalSimpleEmbedder{})
	if err := store.Set(context.Background(), SemanticEntry{
		Namespace: "model:gpt-4o-mini|provider:openai|tenant:team-a",
		Text:      "user: explain caching",
		Response:  &types.ChatResponse{Model: "gpt-4o-mini"},
	}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if _, ok := store.Search(context.Background(), SemanticQuery{
		Namespace:     "model:gpt-4o-mini|provider:openai|tenant:team-b",
		Text:          "user: explain caching",
		MinSimilarity: 0.6,
		MaxTextChars:  1024,
	}); ok {
		t.Fatal("Search() ok = true, want false for different namespace")
	}
}
