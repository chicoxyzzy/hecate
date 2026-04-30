package controlplane

import (
	"context"
	"testing"
)

func TestMemoryStoreAuditEventsCaptureActorAndMutationTrail(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	ctx := WithActor(context.Background(), "admin:req-123")
	tenant, err := store.UpsertTenant(ctx, Tenant{Name: "Team A"})
	if err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	key, err := store.UpsertAPIKey(ctx, APIKey{Name: "Team A Dev", Key: "secret", Tenant: tenant.ID})
	if err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}
	if _, err := store.RotateAPIKey(ctx, key.ID, "new-secret"); err != nil {
		t.Fatalf("RotateAPIKey() error = %v", err)
	}
	if err := store.DeleteAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("DeleteAPIKey() error = %v", err)
	}

	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(state.Events) != 4 {
		t.Fatalf("event count = %d, want 4", len(state.Events))
	}
	if state.Events[0].Actor != "admin:req-123" {
		t.Fatalf("event actor = %q, want admin:req-123", state.Events[0].Actor)
	}
	if state.Events[0].Action != "tenant.created" {
		t.Fatalf("first event action = %q, want tenant.created", state.Events[0].Action)
	}
	if state.Events[2].Action != "api_key.rotated" {
		t.Fatalf("third event action = %q, want api_key.rotated", state.Events[2].Action)
	}
	if state.Events[3].Action != "api_key.deleted" {
		t.Fatalf("fourth event action = %q, want api_key.deleted", state.Events[3].Action)
	}
	if state.Events[3].TargetID != key.ID {
		t.Fatalf("deleted key target id = %q, want %q", state.Events[3].TargetID, key.ID)
	}
}
