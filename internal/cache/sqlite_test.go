package cache

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

func newSQLiteTestStore(t *testing.T, ttl time.Duration) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	client, err := storage.NewSQLiteClient(context.Background(), storage.SQLiteConfig{
		Path:        filepath.Join(dir, "cache.db"),
		TablePrefix: "test",
	})
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store, err := NewSQLiteStore(context.Background(), client, "", ttl)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store
}

func TestSQLiteStore_RejectsNilClient(t *testing.T) {
	t.Parallel()
	if _, err := NewSQLiteStore(context.Background(), nil, "", time.Hour); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestSQLiteStore_GetMissesWhenEmpty(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, time.Hour)
	if _, ok := store.Get(context.Background(), "nope"); ok {
		t.Fatal("Get() ok = true, want false on empty store")
	}
}

func TestSQLiteStore_SetAndGetRoundTrip(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, time.Hour)
	resp := &types.ChatResponse{
		Model: "gpt-4o-mini",
		Choices: []types.ChatChoice{
			{Index: 0, Message: types.Message{Role: "assistant", Content: "hello"}},
		},
	}
	if err := store.Set(context.Background(), "k1", resp); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := store.Get(context.Background(), "k1")
	if !ok {
		t.Fatal("Get ok = false")
	}
	if got.Model != "gpt-4o-mini" {
		t.Fatalf("Model = %q, want gpt-4o-mini", got.Model)
	}
	if len(got.Choices) != 1 || got.Choices[0].Message.Content != "hello" {
		t.Fatalf("Choices round-trip lost data: %+v", got.Choices)
	}
}

func TestSQLiteStore_OverwriteEntry(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, time.Hour)
	ctx := context.Background()

	if err := store.Set(ctx, "k", &types.ChatResponse{Model: "first"}); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := store.Set(ctx, "k", &types.ChatResponse{Model: "second"}); err != nil {
		t.Fatalf("Set second: %v", err)
	}
	got, ok := store.Get(ctx, "k")
	if !ok {
		t.Fatal("Get ok = false after overwrite")
	}
	if got.Model != "second" {
		t.Fatalf("Model = %q, want second", got.Model)
	}
}

func TestSQLiteStore_TTLExpiry(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, 10*time.Millisecond)
	if err := store.Set(context.Background(), "k", &types.ChatResponse{Model: "x"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, ok := store.Get(context.Background(), "k"); ok {
		t.Fatal("expected entry to be expired")
	}
}

func TestSQLiteStore_Prune_RemovesExpired(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, 10*time.Millisecond)
	for _, key := range []string{"a", "b", "c", "d", "e"} {
		if err := store.Set(context.Background(), key, &types.ChatResponse{Model: "m"}); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
	}
	time.Sleep(30 * time.Millisecond)

	deleted, err := store.Prune(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 5 {
		t.Fatalf("deleted = %d, want 5", deleted)
	}
}

func TestSQLiteStore_Prune_RespectsMaxAge(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, time.Hour)
	ctx := context.Background()

	if err := store.Set(ctx, "old", &types.ChatResponse{Model: "o"}); err != nil {
		t.Fatalf("Set old: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if err := store.Set(ctx, "new", &types.ChatResponse{Model: "n"}); err != nil {
		t.Fatalf("Set new: %v", err)
	}

	deleted, err := store.Prune(ctx, 20*time.Millisecond, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, ok := store.Get(ctx, "new"); !ok {
		t.Fatal("'new' should still be present")
	}
	if _, ok := store.Get(ctx, "old"); ok {
		t.Fatal("'old' should be pruned")
	}
}

func TestSQLiteStore_Prune_RespectsMaxCount(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t, time.Hour)
	ctx := context.Background()

	// Insert with brief gaps so updated_at strictly orders.
	keys := []string{"k1", "k2", "k3", "k4", "k5"}
	for _, key := range keys {
		if err := store.Set(ctx, key, &types.ChatResponse{Model: key}); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	deleted, err := store.Prune(ctx, 0, 2)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 3 {
		t.Fatalf("deleted = %d, want 3", deleted)
	}

	// k4 and k5 are the two newest; the older three should be gone.
	for _, key := range []string{"k4", "k5"} {
		if _, ok := store.Get(ctx, key); !ok {
			t.Fatalf("%q should have been retained", key)
		}
	}
	for _, key := range []string{"k1", "k2", "k3"} {
		if _, ok := store.Get(ctx, key); ok {
			t.Fatalf("%q should have been pruned", key)
		}
	}
}

func TestSQLiteStore_PrefixYieldsDistinctTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	client, err := storage.NewSQLiteClient(context.Background(), storage.SQLiteConfig{
		Path:        filepath.Join(dir, "cache.db"),
		TablePrefix: "test",
	})
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	a, err := NewSQLiteStore(context.Background(), client, "ns_a", time.Hour)
	if err != nil {
		t.Fatalf("NewSQLiteStore(ns_a): %v", err)
	}
	b, err := NewSQLiteStore(context.Background(), client, "ns_b", time.Hour)
	if err != nil {
		t.Fatalf("NewSQLiteStore(ns_b): %v", err)
	}

	if err := a.Set(context.Background(), "k", &types.ChatResponse{Model: "from-a"}); err != nil {
		t.Fatalf("Set on a: %v", err)
	}
	if _, ok := b.Get(context.Background(), "k"); ok {
		t.Fatal("prefix-isolated stores should not share rows")
	}
}
