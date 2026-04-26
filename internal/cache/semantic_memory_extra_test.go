package cache

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestMemorySemanticStorePruneByMaxAge(t *testing.T) {
	store := NewMemorySemanticStore(0, 100, LocalSimpleEmbedder{Dimensions: 32})
	ctx := context.Background()

	for _, text := range []string{"hello world alpha", "another quick fox", "the third entry"} {
		if err := store.Set(ctx, SemanticEntry{
			Namespace: "tenant-a",
			Text:      text,
			Response:  &types.ChatResponse{ID: "resp"},
		}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	// Backdate the first two records so a Prune(maxAge=1h, _) drops them.
	store.mu.Lock()
	store.entries[0].storedAt = time.Now().Add(-2 * time.Hour)
	store.entries[1].storedAt = time.Now().Add(-2 * time.Hour)
	store.mu.Unlock()

	deleted, err := store.Prune(ctx, time.Hour, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.entries) != 1 {
		t.Errorf("remaining entries = %d, want 1", len(store.entries))
	}
}

func TestMemorySemanticStorePruneByMaxCount(t *testing.T) {
	store := NewMemorySemanticStore(0, 100, LocalSimpleEmbedder{Dimensions: 32})
	ctx := context.Background()

	for _, text := range []string{"first entry alpha", "second entry beta", "third entry gamma", "fourth entry delta"} {
		if err := store.Set(ctx, SemanticEntry{Namespace: "ns", Text: text, Response: &types.ChatResponse{}}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	deleted, err := store.Prune(ctx, 0, 2)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2 (4 entries trimmed to 2)", deleted)
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.entries) != 2 {
		t.Errorf("remaining entries = %d, want 2", len(store.entries))
	}
}

func TestMemorySemanticStorePruneRemovesExpired(t *testing.T) {
	store := NewMemorySemanticStore(0, 100, LocalSimpleEmbedder{Dimensions: 32})
	ctx := context.Background()

	if err := store.Set(ctx, SemanticEntry{Namespace: "ns", Text: "expiring text alpha", Response: &types.ChatResponse{}, ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set(ctx, SemanticEntry{Namespace: "ns", Text: "still valid beta", Response: &types.ChatResponse{}, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	deleted, err := store.Prune(ctx, 0, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (expired entry only)", deleted)
	}
}

func TestMemorySemanticStoreSetEnforcesMaxEntries(t *testing.T) {
	store := NewMemorySemanticStore(0, 2, LocalSimpleEmbedder{Dimensions: 32})
	ctx := context.Background()

	for _, text := range []string{"one alpha bravo", "two charlie delta", "three echo foxtrot", "four golf hotel"} {
		if err := store.Set(ctx, SemanticEntry{Namespace: "ns", Text: text, Response: &types.ChatResponse{}}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.entries) != 2 {
		t.Fatalf("entries length = %d, want 2 (oldest must be evicted)", len(store.entries))
	}
	// The two surviving entries should be the most-recently inserted, so
	// "three" and "four" should appear and "one"/"two" should not.
	keptTexts := []string{store.entries[0].entry.Text, store.entries[1].entry.Text}
	for _, want := range []string{"three echo foxtrot", "four golf hotel"} {
		found := false
		for _, kt := range keptTexts {
			if kt == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %q to remain, kept = %v", want, keptTexts)
		}
	}
}

func TestEligibleForSemanticCacheRejectsTimeSensitiveQueries(t *testing.T) {
	cases := []struct {
		name string
		req  types.ChatRequest
		want bool
	}{
		{
			name: "stable factual question is eligible",
			req:  types.ChatRequest{Messages: []types.Message{{Role: "user", Content: "What is the speed of light?"}}},
			want: true,
		},
		{
			name: "today is excluded",
			req:  types.ChatRequest{Messages: []types.Message{{Role: "user", Content: "Tell me about today"}}},
			want: false,
		},
		{
			name: "stock price is excluded",
			req:  types.ChatRequest{Messages: []types.Message{{Role: "user", Content: "What's the AAPL stock price?"}}},
			want: false,
		},
		{
			name: "weather is excluded",
			req:  types.ChatRequest{Messages: []types.Message{{Role: "user", Content: "Will it rain? weather forecast please"}}},
			want: false,
		},
		{
			name: "empty messages is not eligible",
			req:  types.ChatRequest{},
			want: false,
		},
		{
			name: "messages with empty content are not eligible",
			req:  types.ChatRequest{Messages: []types.Message{{Role: "user", Content: "   "}}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EligibleForSemanticCache(tc.req, 4096); got != tc.want {
				t.Errorf("EligibleForSemanticCache = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildSemanticTextSkipsEmptyContent(t *testing.T) {
	req := types.ChatRequest{Messages: []types.Message{
		{Role: "user", Content: "  "},
		{Role: "", Content: "anonymous body"},
		{Role: "assistant", Content: "the answer"},
	}}
	got := BuildSemanticText(req, 4096)
	// The empty-content row is skipped; the no-role row falls back to "message".
	if got == "" {
		t.Fatal("BuildSemanticText returned empty")
	}
	wantSubstrings := []string{"message: anonymous body", "assistant: the answer"}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("BuildSemanticText output %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "user:") {
		t.Errorf("user line with empty content should be skipped, got %q", got)
	}
}
