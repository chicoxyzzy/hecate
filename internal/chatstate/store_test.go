package chatstate

import (
	"context"
	"testing"

	"github.com/hecate/agent-runtime/pkg/types"
)

func TestMemoryStoreUpdateSessionSystemPrompt(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	ctx := context.Background()

	created, err := store.CreateSession(ctx, types.ChatSession{ID: "s1", Title: "first"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.SystemPrompt != "" {
		t.Fatalf("default SystemPrompt = %q, want empty", created.SystemPrompt)
	}

	updated, err := store.UpdateSessionSystemPrompt(ctx, "s1", "be terse and helpful")
	if err != nil {
		t.Fatalf("UpdateSessionSystemPrompt: %v", err)
	}
	if updated.SystemPrompt != "be terse and helpful" {
		t.Fatalf("SystemPrompt = %q, want %q", updated.SystemPrompt, "be terse and helpful")
	}
	// Title should not be touched by a system-prompt update.
	if updated.Title != "first" {
		t.Fatalf("Title clobbered by SystemPrompt update: got %q, want first", updated.Title)
	}

	// Round-trip via GetSession to confirm persistence (and not just an
	// echo of the in-memory return value).
	got, ok, err := store.GetSession(ctx, "s1")
	if err != nil || !ok {
		t.Fatalf("GetSession: ok=%v err=%v", ok, err)
	}
	if got.SystemPrompt != "be terse and helpful" {
		t.Fatalf("persisted SystemPrompt = %q, want %q", got.SystemPrompt, "be terse and helpful")
	}

	// Empty value clears the prompt.
	cleared, err := store.UpdateSessionSystemPrompt(ctx, "s1", "")
	if err != nil {
		t.Fatalf("UpdateSessionSystemPrompt(empty): %v", err)
	}
	if cleared.SystemPrompt != "" {
		t.Fatalf("expected empty SystemPrompt after clear, got %q", cleared.SystemPrompt)
	}
}

func TestMemoryStoreUpdateSessionSystemPromptUnknownID(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()
	if _, err := store.UpdateSessionSystemPrompt(context.Background(), "missing", "x"); err == nil {
		t.Fatal("UpdateSessionSystemPrompt on unknown id: err = nil, want error")
	}
}
