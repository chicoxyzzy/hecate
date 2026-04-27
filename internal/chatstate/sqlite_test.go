package chatstate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

func newSQLiteTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	client, err := storage.NewSQLiteClient(context.Background(), storage.SQLiteConfig{
		Path:        filepath.Join(dir, "chatstate.db"),
		TablePrefix: "test",
	})
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store, err := NewSQLiteStore(context.Background(), client)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store
}

func TestSQLiteStoreCreateAndGetSession(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if got := store.Backend(); got != "sqlite" {
		t.Fatalf("Backend() = %q, want sqlite", got)
	}

	created, err := store.CreateSession(ctx, types.ChatSession{
		ID:           "s1",
		Title:        "first session",
		SystemPrompt: "be terse",
		Tenant:       "tenant-a",
		User:         "alice",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.ID != "s1" || created.Title != "first session" {
		t.Fatalf("created mismatch: %+v", created)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not populated: %+v", created)
	}

	got, ok, err := store.GetSession(ctx, "s1")
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.SystemPrompt != "be terse" || got.Tenant != "tenant-a" || got.User != "alice" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Turns) != 0 {
		t.Fatalf("new session has turns: %+v", got.Turns)
	}

	// Missing id -> ok=false, err=nil.
	_, ok, err = store.GetSession(ctx, "missing")
	if err != nil {
		t.Fatalf("GetSession(missing): err = %v", err)
	}
	if ok {
		t.Fatal("GetSession(missing): ok = true, want false")
	}
}

func TestSQLiteStoreAppendTurn(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, types.ChatSession{ID: "s1", Title: "t", Tenant: "tA"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	turn := types.ChatSessionTurn{
		ID:                "turn-1",
		RequestID:         "req-1",
		UserMessage:       types.Message{Role: "user", Content: "hi"},
		AssistantMessage:  types.Message{Role: "assistant", Content: "hello"},
		RequestedProvider: "openai",
		Provider:          "openai",
		ProviderKind:      "chat",
		RequestedModel:    "gpt-4o",
		Model:             "gpt-4o",
		CostMicrosUSD:     1234,
		PromptTokens:      10,
		CompletionTokens:  5,
		TotalTokens:       15,
	}
	updated, err := store.AppendTurn(ctx, "s1", turn)
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	if len(updated.Turns) != 1 {
		t.Fatalf("turn count = %d, want 1", len(updated.Turns))
	}
	got := updated.Turns[0]
	if got.ID != "turn-1" || got.RequestID != "req-1" {
		t.Fatalf("turn round-trip mismatch: %+v", got)
	}
	if got.UserMessage.Content != "hi" || got.AssistantMessage.Content != "hello" {
		t.Fatalf("messages round-trip mismatch: %+v", got)
	}
	if got.TotalTokens != 15 || got.CostMicrosUSD != 1234 {
		t.Fatalf("numeric round-trip mismatch: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("turn CreatedAt not populated")
	}

	// Appending updates the parent session's UpdatedAt.
	if !updated.UpdatedAt.Equal(got.CreatedAt) {
		t.Fatalf("session UpdatedAt = %v, want %v", updated.UpdatedAt, got.CreatedAt)
	}
}

func TestSQLiteStoreListSessionsPagination(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	base := time.Now().UTC().Truncate(time.Second)
	// Create three sessions in tenant-a with strictly increasing
	// CreatedAt/UpdatedAt so ORDER BY updated_at DESC, created_at DESC
	// is deterministic.
	for i, id := range []string{"s1", "s2", "s3"} {
		ts := base.Add(time.Duration(i) * time.Minute)
		if _, err := store.CreateSession(ctx, types.ChatSession{
			ID:        id,
			Title:     id,
			Tenant:    "tenant-a",
			CreatedAt: ts,
		}); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
	}
	// One session in a different tenant — should be filtered out.
	if _, err := store.CreateSession(ctx, types.ChatSession{
		ID:        "other",
		Title:     "other tenant",
		Tenant:    "tenant-b",
		CreatedAt: base.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateSession(other): %v", err)
	}

	all, err := store.ListSessions(ctx, Filter{Tenant: "tenant-a"})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListSessions tenant-a: got %d, want 3", len(all))
	}
	// Newest first.
	if all[0].ID != "s3" || all[1].ID != "s2" || all[2].ID != "s1" {
		t.Fatalf("ordering: got %s,%s,%s want s3,s2,s1", all[0].ID, all[1].ID, all[2].ID)
	}

	// Limit cap.
	page1, err := store.ListSessions(ctx, Filter{Tenant: "tenant-a", Limit: 2})
	if err != nil {
		t.Fatalf("ListSessions(limit=2): %v", err)
	}
	if len(page1) != 2 || page1[0].ID != "s3" || page1[1].ID != "s2" {
		t.Fatalf("limit=2 page: %+v", page1)
	}

	// Offset.
	page2, err := store.ListSessions(ctx, Filter{Tenant: "tenant-a", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListSessions(limit=2,offset=2): %v", err)
	}
	if len(page2) != 1 || page2[0].ID != "s1" {
		t.Fatalf("offset page: %+v", page2)
	}

	// Offset without explicit limit still works (LIMIT -1 fallback).
	page3, err := store.ListSessions(ctx, Filter{Tenant: "tenant-a", Offset: 1})
	if err != nil {
		t.Fatalf("ListSessions(offset=1,no limit): %v", err)
	}
	if len(page3) != 2 || page3[0].ID != "s2" || page3[1].ID != "s1" {
		t.Fatalf("offset-only page: %+v", page3)
	}

	// No tenant filter — sees both tenants.
	mixed, err := store.ListSessions(ctx, Filter{})
	if err != nil {
		t.Fatalf("ListSessions(unfiltered): %v", err)
	}
	if len(mixed) != 4 {
		t.Fatalf("ListSessions(unfiltered): got %d, want 4", len(mixed))
	}
}

func TestSQLiteStoreDeleteSessionCascadesTurns(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, types.ChatSession{ID: "s1", Title: "t", Tenant: "tA"}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for _, id := range []string{"turn-1", "turn-2"} {
		if _, err := store.AppendTurn(ctx, "s1", types.ChatSessionTurn{
			ID:               id,
			RequestID:        id,
			UserMessage:      types.Message{Role: "user", Content: "u"},
			AssistantMessage: types.Message{Role: "assistant", Content: "a"},
		}); err != nil {
			t.Fatalf("AppendTurn(%s): %v", id, err)
		}
	}

	if err := store.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	// Session is gone.
	_, ok, err := store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatalf("GetSession after delete: %v", err)
	}
	if ok {
		t.Fatal("session still present after delete")
	}

	// Turns are gone — query the turns table directly to confirm the
	// cascade fired (the session lookup above is not a sufficient
	// witness; the explicit DELETE-on-turns belt-and-braces in
	// DeleteSession would also satisfy it).
	var count int
	if err := store.client.DB().QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM "+store.turnsTable+" WHERE session_id = ?",
		"s1",
	).Scan(&count); err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if count != 0 {
		t.Fatalf("turns remaining after cascade delete: %d", count)
	}
}

func TestSQLiteStoreUpdateSessionTitle(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, types.ChatSession{
		ID:           "s1",
		Title:        "original",
		SystemPrompt: "keep me",
		Tenant:       "tA",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	updated, err := store.UpdateSession(ctx, "s1", "renamed")
	if err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
	if updated.Title != "renamed" {
		t.Fatalf("Title = %q, want renamed", updated.Title)
	}
	if updated.SystemPrompt != "keep me" {
		t.Fatalf("rename clobbered SystemPrompt: %q", updated.SystemPrompt)
	}

	got, ok, err := store.GetSession(ctx, "s1")
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.Title != "renamed" {
		t.Fatalf("persisted Title = %q, want renamed", got.Title)
	}
}

func TestSQLiteStoreUpdateSessionSystemPrompt(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateSession(ctx, types.ChatSession{
		ID:     "s1",
		Title:  "first",
		Tenant: "tA",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	updated, err := store.UpdateSessionSystemPrompt(ctx, "s1", "be terse and helpful")
	if err != nil {
		t.Fatalf("UpdateSessionSystemPrompt: %v", err)
	}
	if updated.SystemPrompt != "be terse and helpful" {
		t.Fatalf("SystemPrompt = %q", updated.SystemPrompt)
	}
	if updated.Title != "first" {
		t.Fatalf("Title clobbered by SystemPrompt update: %q", updated.Title)
	}
}

func TestNewSQLiteStoreRejectsNilClient(t *testing.T) {
	t.Parallel()
	if _, err := NewSQLiteStore(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil client")
	}
}
