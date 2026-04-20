package cache

import (
	"testing"

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
