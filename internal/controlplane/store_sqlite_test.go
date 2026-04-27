package controlplane

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hecate/agent-runtime/internal/storage"
)

func newSQLiteTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	client, err := storage.NewSQLiteClient(context.Background(), storage.SQLiteConfig{
		Path:        filepath.Join(dir, "controlplane.db"),
		TablePrefix: "test",
	})
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store, err := NewSQLiteStore(context.Background(), client, "")
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store
}

func TestSQLiteStore_RejectsNilClient(t *testing.T) {
	_, err := NewSQLiteStore(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestSQLiteStore_BackendName(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	if got := store.Backend(); got != "sqlite" {
		t.Fatalf("Backend() = %q, want sqlite", got)
	}
}

func TestSQLiteStore_TenantRoundTrip(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	tenant, err := store.UpsertTenant(ctx, Tenant{Name: "Acme", Description: "test"})
	if err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}
	if tenant.ID == "" {
		t.Fatal("tenant id not generated")
	}
	if !tenant.Enabled {
		t.Fatal("new tenant should default to enabled")
	}

	// Snapshot reads back the same row out of SQLite — proves the JSON
	// payload survives marshal → TEXT → unmarshal.
	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Tenants) != 1 || state.Tenants[0].ID != tenant.ID {
		t.Fatalf("snapshot tenants = %+v", state.Tenants)
	}
	if state.Tenants[0].Description != "test" {
		t.Fatalf("description not preserved: %+v", state.Tenants[0])
	}

	// Update path — UpsertTenant on an existing ID emits "tenant.updated".
	updated, err := store.UpsertTenant(ctx, Tenant{ID: tenant.ID, Name: "Acme Inc", Enabled: true})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Name != "Acme Inc" {
		t.Fatalf("name = %q, want Acme Inc", updated.Name)
	}

	// Disable round-trips through the SQLite store.
	disabled, err := store.SetTenantEnabled(ctx, tenant.ID, false)
	if err != nil {
		t.Fatalf("SetTenantEnabled: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("tenant should be disabled")
	}

	state, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Tenants) != 1 || state.Tenants[0].Enabled {
		t.Fatalf("disabled state not persisted: %+v", state.Tenants)
	}

	// Delete and confirm Snapshot reflects an empty tenants slice — a
	// regression where SQLite returned the cached pre-delete row would
	// fail here.
	if err := store.DeleteTenant(ctx, tenant.ID); err != nil {
		t.Fatalf("DeleteTenant: %v", err)
	}
	state, _ = store.Snapshot(ctx)
	if len(state.Tenants) != 0 {
		t.Fatalf("expected 0 tenants after delete, got %d", len(state.Tenants))
	}
}

func TestSQLiteStore_APIKeyRoundTrip(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	key, err := store.UpsertAPIKey(ctx, APIKey{
		Name: "ci-key",
		Key:  "hct_sk_initial_secret",
		Role: "tenant",
	})
	if err != nil {
		t.Fatalf("UpsertAPIKey: %v", err)
	}
	if key.ID == "" {
		t.Fatal("key id not generated")
	}
	if key.CreatedAt.IsZero() || key.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", key)
	}

	// Snapshot proves the row survived round-trip through TEXT JSON.
	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.APIKeys) != 1 || state.APIKeys[0].ID != key.ID {
		t.Fatalf("snapshot keys = %+v", state.APIKeys)
	}
	if state.APIKeys[0].Key != "hct_sk_initial_secret" {
		t.Fatalf("key secret not preserved: %+v", state.APIKeys[0])
	}

	// Rotate flips the secret and updates the timestamp.
	rotated, err := store.RotateAPIKey(ctx, key.ID, "hct_sk_new_secret")
	if err != nil {
		t.Fatalf("RotateAPIKey: %v", err)
	}
	if rotated.Key != "hct_sk_new_secret" {
		t.Fatalf("rotated key = %q, want hct_sk_new_secret", rotated.Key)
	}

	// Disable.
	disabled, err := store.SetAPIKeyEnabled(ctx, key.ID, false)
	if err != nil {
		t.Fatalf("SetAPIKeyEnabled: %v", err)
	}
	if disabled.Enabled {
		t.Fatal("api key should be disabled")
	}

	// Snapshot the post-rotation, post-disable state.
	state, err = store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.APIKeys) != 1 {
		t.Fatalf("keys = %d, want 1", len(state.APIKeys))
	}
	if state.APIKeys[0].Key != "hct_sk_new_secret" || state.APIKeys[0].Enabled {
		t.Fatalf("post-rotate/disable state lost: %+v", state.APIKeys[0])
	}

	// Delete the key and confirm it's gone.
	if err := store.DeleteAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("DeleteAPIKey: %v", err)
	}
	state, _ = store.Snapshot(ctx)
	for _, k := range state.APIKeys {
		if k.ID == key.ID {
			t.Fatal("key should be removed")
		}
	}
}

func TestSQLiteStore_SnapshotEmptyOnFreshDatabase(t *testing.T) {
	t.Parallel()
	// A freshly-migrated SQLite store with no rows yet must return a
	// zero-value State, not an error — the gateway boots with an empty
	// control plane on day one.
	store := newSQLiteTestStore(t)
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Tenants) != 0 || len(state.APIKeys) != 0 {
		t.Fatalf("expected empty state, got %+v", state)
	}
}

func TestSQLiteStore_SnapshotReturnsCombinedState(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	tenant, err := store.UpsertTenant(ctx, Tenant{Name: "Acme"})
	if err != nil {
		t.Fatalf("UpsertTenant: %v", err)
	}
	if _, err := store.UpsertAPIKey(ctx, APIKey{
		Name:   "ci",
		Key:    "hct_sk_combined",
		Tenant: tenant.ID,
		Role:   "tenant",
	}); err != nil {
		t.Fatalf("UpsertAPIKey: %v", err)
	}

	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Tenants) != 1 || len(state.APIKeys) != 1 {
		t.Fatalf("expected 1 tenant and 1 api key, got %d / %d", len(state.Tenants), len(state.APIKeys))
	}
	if state.APIKeys[0].Tenant != tenant.ID {
		t.Fatalf("api key tenant linkage lost: %+v", state.APIKeys[0])
	}
	// Each mutation appended an audit event; a regression where audit
	// events failed to round-trip through the JSON column would land
	// here.
	if len(state.Events) < 2 {
		t.Fatalf("expected >=2 audit events, got %d", len(state.Events))
	}
}
