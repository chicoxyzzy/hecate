package controlplane

import (
	"context"
	"testing"

	"github.com/hecate/agent-runtime/internal/storage"
)

func TestRedisStoreUpsertTenantAndAPIKeyPersists(t *testing.T) {
	t.Parallel()

	client := &fakeRedisClient{}
	store, err := NewRedisStoreFromClient(client, "hecate", "control-plane")
	if err != nil {
		t.Fatalf("NewRedisStoreFromClient() error = %v", err)
	}

	tenant, err := store.UpsertTenant(context.Background(), Tenant{
		Name:             "Team A",
		AllowedProviders: []string{"openai"},
	})
	if err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}

	key, err := store.UpsertAPIKey(context.Background(), APIKey{
		Name:   "Team A Dev",
		Key:    "hecate-team-a-dev",
		Tenant: tenant.ID,
	})
	if err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}
	if key.ID != "team-a-dev" {
		t.Fatalf("key.ID = %q, want team-a-dev", key.ID)
	}

	snapshot, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Tenants) != 1 {
		t.Fatalf("tenant count = %d, want 1", len(snapshot.Tenants))
	}
	if len(snapshot.APIKeys) != 1 {
		t.Fatalf("api key count = %d, want 1", len(snapshot.APIKeys))
	}
}

func TestRedisStoreRejectsUnknownTenant(t *testing.T) {
	t.Parallel()

	store, err := NewRedisStoreFromClient(&fakeRedisClient{}, "hecate", "control-plane")
	if err != nil {
		t.Fatalf("NewRedisStoreFromClient() error = %v", err)
	}

	if _, err := store.UpsertAPIKey(context.Background(), APIKey{
		Name:   "Unknown Tenant Key",
		Key:    "secret",
		Tenant: "missing-tenant",
	}); err == nil {
		t.Fatal("UpsertAPIKey() error = nil, want unknown tenant error")
	}
}

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

// TestSetProviderEnabledDisablesConflictingProviders verifies that enabling a provider
// automatically disables any other provider sharing the same base URL. The default
// llamacpp/localai pair both target 127.0.0.1:8080.
func TestSetProviderEnabledDisablesConflictingProviders(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore()

	ctx := context.Background()

	// Enable llamacpp first.
	if _, err := store.SetProviderEnabled(ctx, "llamacpp", true); err != nil {
		t.Fatalf("SetProviderEnabled(llamacpp, true) error = %v", err)
	}
	// Now enable localai — should disable llamacpp because they share 127.0.0.1:8080.
	if _, err := store.SetProviderEnabled(ctx, "localai", true); err != nil {
		t.Fatalf("SetProviderEnabled(localai, true) error = %v", err)
	}

	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}

	enabled := map[string]bool{}
	for _, p := range state.Providers {
		enabled[p.ID] = p.Enabled
	}
	if !enabled["localai"] {
		t.Fatalf("localai should be enabled after explicit enable")
	}
	if enabled["llamacpp"] {
		t.Fatalf("llamacpp should be auto-disabled (shares endpoint with localai)")
	}

	// Auto-disabled placeholder records must carry hydrated built-in fields so the
	// frontend's group-by-kind rendering doesn't drop them.
	for _, p := range state.Providers {
		if p.ID != "llamacpp" && p.ID != "localai" {
			continue
		}
		if p.Kind == "" {
			t.Fatalf("provider %q has empty Kind — placeholder must inherit from built-in preset", p.ID)
		}
		if p.BaseURL == "" {
			t.Fatalf("provider %q has empty BaseURL — placeholder must inherit from built-in preset", p.ID)
		}
	}
}

type fakeRedisClient struct {
	data map[string][]byte
}

func (f *fakeRedisClient) Get(_ context.Context, key string) ([]byte, error) {
	if f.data == nil {
		return nil, storage.ErrNil
	}
	value, ok := f.data[key]
	if !ok {
		return nil, storage.ErrNil
	}
	return append([]byte(nil), value...), nil
}

func (f *fakeRedisClient) Set(_ context.Context, key string, value []byte) error {
	if f.data == nil {
		f.data = map[string][]byte{}
	}
	f.data[key] = append([]byte(nil), value...)
	return nil
}
