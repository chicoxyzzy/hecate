package cache

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestBuildSemanticNamespaceFallsBackToAnonymousAndCanonicalModel(t *testing.T) {
	t.Parallel()

	req := types.ChatRequest{
		Metadata: map[string]string{},
	}
	decision := types.RouteDecision{
		Provider: "openai",
		Model:    "gpt-4o-mini-2024-07-18",
	}

	got := BuildSemanticNamespace(req, decision)
	want := "model:gpt-4o-mini|provider:openai|tenant:anonymous"
	if got != want {
		t.Fatalf("BuildSemanticNamespace() = %q, want %q", got, want)
	}
}

func TestBuildSemanticTextNormalizesRolesAndTruncates(t *testing.T) {
	t.Parallel()

	req := types.ChatRequest{
		Messages: []types.Message{
			{Role: "USER", Content: "Hello"},
			{Role: "", Content: "World"},
			{Role: "assistant", Content: ""},
		},
	}

	got := BuildSemanticText(req, 20)
	want := "user: Hello\nmessage:"
	if got != want {
		t.Fatalf("BuildSemanticText() = %q, want %q", got, want)
	}
}

func TestEligibleForSemanticCacheRejectsTimeSensitiveRequests(t *testing.T) {
	t.Parallel()

	req := types.ChatRequest{
		Messages: []types.Message{{Role: "user", Content: "What is the latest stock price today?"}},
	}
	if EligibleForSemanticCache(req, 1024) {
		t.Fatal("EligibleForSemanticCache() = true, want false")
	}
}

func TestCloneChatResponseClonesChoicesSlice(t *testing.T) {
	t.Parallel()

	resp := &types.ChatResponse{
		Choices: []types.ChatChoice{{Index: 0, Message: types.Message{Role: "assistant", Content: "hello"}}},
	}
	cloned := cloneChatResponse(resp)
	cloned.Choices[0].Message.Content = "changed"

	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("original choice content mutated = %q, want hello", resp.Choices[0].Message.Content)
	}
}

func TestMemorySemanticStorePrunesExpiredEntriesOnSearch(t *testing.T) {
	t.Parallel()

	store := NewMemorySemanticStore(time.Hour, 10, LocalSimpleEmbedder{})
	err := store.Set(context.Background(), SemanticEntry{
		Namespace: "n1",
		Text:      "user: expired prompt",
		Response:  &types.ChatResponse{Model: "m1"},
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if _, ok := store.Search(context.Background(), SemanticQuery{
		Namespace:     "n1",
		Text:          "user: expired prompt",
		MinSimilarity: 0.5,
		MaxTextChars:  1024,
	}); ok {
		t.Fatal("Search() ok = true, want false for expired entry")
	}
	if len(store.entries) != 0 {
		t.Fatalf("len(entries) = %d, want 0 after pruning", len(store.entries))
	}
}

func TestMemorySemanticStoreHonorsMaxEntries(t *testing.T) {
	t.Parallel()

	store := NewMemorySemanticStore(time.Hour, 2, LocalSimpleEmbedder{})
	for _, text := range []string{"user: one", "user: two", "user: three"} {
		err := store.Set(context.Background(), SemanticEntry{
			Namespace: "n1",
			Text:      text,
			Response:  &types.ChatResponse{Model: "m1"},
		})
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}
	}

	if len(store.entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(store.entries))
	}
	if store.entries[0].entry.Text != "user: two" || store.entries[1].entry.Text != "user: three" {
		t.Fatalf("kept entries = %#v, want last two entries", []string{store.entries[0].entry.Text, store.entries[1].entry.Text})
	}
}
