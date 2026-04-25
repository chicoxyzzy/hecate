package cache

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

func mustResp(model string) *types.ChatResponse {
	return &types.ChatResponse{Model: model}
}

func TestMemoryStore_GetMissesWhenEmpty(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(time.Hour)
	if _, ok := store.Get(context.Background(), "nope"); ok {
		t.Fatal("Get() ok = true, want false on empty store")
	}
}

func TestMemoryStore_SetAndGet(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(time.Hour)
	resp := mustResp("gpt-4o")
	if err := store.Set(context.Background(), "k1", resp); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, ok := store.Get(context.Background(), "k1")
	if !ok {
		t.Fatal("Get ok = false")
	}
	if got.Model != "gpt-4o" {
		t.Fatalf("Model = %q", got.Model)
	}
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(10 * time.Millisecond)
	_ = store.Set(context.Background(), "k", mustResp("x"))
	time.Sleep(20 * time.Millisecond)
	if _, ok := store.Get(context.Background(), "k"); ok {
		t.Fatal("expected entry to be expired")
	}
}

func TestMemoryStore_Prune_RemovesExpired(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(10 * time.Millisecond)
	for i := 0; i < 5; i++ {
		_ = store.Set(context.Background(), string(rune('a'+i)), mustResp("m"))
	}
	time.Sleep(20 * time.Millisecond)
	deleted, err := store.Prune(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("deleted = %d, want 5", deleted)
	}
}

func TestMemoryStore_Prune_RespectsMaxAge(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(time.Hour)
	_ = store.Set(context.Background(), "old", mustResp("o"))
	time.Sleep(15 * time.Millisecond)
	_ = store.Set(context.Background(), "new", mustResp("n"))

	deleted, err := store.Prune(context.Background(), 10*time.Millisecond, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, ok := store.Get(context.Background(), "new"); !ok {
		t.Fatal("'new' should still be present")
	}
	if _, ok := store.Get(context.Background(), "old"); ok {
		t.Fatal("'old' should be pruned")
	}
}

func TestMemoryStore_OverwriteEntry(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(time.Hour)
	_ = store.Set(context.Background(), "k", mustResp("first"))
	_ = store.Set(context.Background(), "k", mustResp("second"))
	got, _ := store.Get(context.Background(), "k")
	if got.Model != "second" {
		t.Fatalf("Model = %q, want second", got.Model)
	}
}

func TestMemoryStore_Concurrent(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore(time.Hour)
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			_ = store.Set(context.Background(), string(rune('a'+id)), mustResp("m"))
			_, _ = store.Get(context.Background(), string(rune('a'+id)))
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
