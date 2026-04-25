package controlplane

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/storage"
)

func TestFileStoreUpsertTenantAndAPIKeyPersists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "control-plane.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	tenant, err := store.UpsertTenant(context.Background(), Tenant{
		Name:             "Team A",
		Description:      "Primary tenant",
		AllowedProviders: []string{"openai", "ollama"},
		AllowedModels:    []string{"gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	if tenant.ID != "team-a" {
		t.Fatalf("tenant.ID = %q, want team-a", tenant.ID)
	}

	key, err := store.UpsertAPIKey(context.Background(), APIKey{
		Name:             "Team A Dev",
		Key:              "hecate-team-a-dev",
		Tenant:           tenant.ID,
		Role:             "tenant",
		AllowedProviders: []string{"ollama"},
	})
	if err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}
	if key.ID != "team-a-dev" {
		t.Fatalf("key.ID = %q, want team-a-dev", key.ID)
	}
	if key.CreatedAt.IsZero() || key.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be populated")
	}

	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(reload) error = %v", err)
	}

	state, err := reloaded.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(state.Tenants) != 1 {
		t.Fatalf("tenant count = %d, want 1", len(state.Tenants))
	}
	if len(state.APIKeys) != 1 {
		t.Fatalf("api key count = %d, want 1", len(state.APIKeys))
	}
	if state.APIKeys[0].Tenant != "team-a" {
		t.Fatalf("api key tenant = %q, want team-a", state.APIKeys[0].Tenant)
	}
}

func TestFileStoreRejectsAPIKeyForUnknownTenant(t *testing.T) {
	t.Parallel()

	store, err := NewFileStore(filepath.Join(t.TempDir(), "control-plane.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	if _, err := store.UpsertAPIKey(context.Background(), APIKey{
		Name:   "Unknown Tenant Key",
		Key:    "secret",
		Tenant: "missing-tenant",
		Role:   "tenant",
	}); err == nil {
		t.Fatal("UpsertAPIKey() error = nil, want unknown tenant error")
	}
}

func TestFileStorePersistsPolicyRulesAndPricebookEntries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "control-plane.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	if _, err := store.UpsertPolicyRule(context.Background(), config.PolicyRuleConfig{
		ID:              "deny-expensive-cloud",
		Action:          "deny",
		Reason:          "cloud route blocked",
		ProviderKinds:   []string{" cloud ", "cloud"},
		RouteReasons:    []string{"fallback"},
		MinPromptTokens: 1000,
	}); err != nil {
		t.Fatalf("UpsertPolicyRule() error = %v", err)
	}
	if _, err := store.UpsertPricebookEntry(context.Background(), config.ModelPriceConfig{
		Provider:                             "openai",
		Model:                                "custom-model",
		InputMicrosUSDPerMillionTokens:       100_000,
		OutputMicrosUSDPerMillionTokens:      200_000,
		CachedInputMicrosUSDPerMillionTokens: 50_000,
	}); err != nil {
		t.Fatalf("UpsertPricebookEntry() error = %v", err)
	}

	reloaded, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore(reload) error = %v", err)
	}
	state, err := reloaded.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(state.PolicyRules) != 1 || state.PolicyRules[0].ProviderKinds[0] != "cloud" {
		t.Fatalf("policy rules = %#v, want normalized persisted rule", state.PolicyRules)
	}
	if len(state.Pricebook) != 1 || state.Pricebook[0].Model != "custom-model" {
		t.Fatalf("pricebook = %#v, want persisted entry", state.Pricebook)
	}

	if err := reloaded.DeletePolicyRule(context.Background(), "deny-expensive-cloud"); err != nil {
		t.Fatalf("DeletePolicyRule() error = %v", err)
	}
	if err := reloaded.DeletePricebookEntry(context.Background(), "openai", "custom-model"); err != nil {
		t.Fatalf("DeletePricebookEntry() error = %v", err)
	}
	state, err = reloaded.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot(after delete) error = %v", err)
	}
	if len(state.PolicyRules) != 0 || len(state.Pricebook) != 0 {
		t.Fatalf("state after delete = %#v, want no policy or pricebook records", state)
	}
}

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

func TestFileStoreLifecycleOperations(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "control-plane.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

	tenant, err := store.UpsertTenant(context.Background(), Tenant{Name: "Team A"})
	if err != nil {
		t.Fatalf("UpsertTenant() error = %v", err)
	}
	key, err := store.UpsertAPIKey(context.Background(), APIKey{Name: "Team A Dev", Key: "secret", Tenant: tenant.ID})
	if err != nil {
		t.Fatalf("UpsertAPIKey() error = %v", err)
	}

	disabledTenant, err := store.SetTenantEnabled(context.Background(), tenant.ID, false)
	if err != nil {
		t.Fatalf("SetTenantEnabled() error = %v", err)
	}
	if disabledTenant.Enabled {
		t.Fatal("expected tenant to be disabled")
	}

	rotatedKey, err := store.RotateAPIKey(context.Background(), key.ID, "new-secret")
	if err != nil {
		t.Fatalf("RotateAPIKey() error = %v", err)
	}
	if rotatedKey.Key != "new-secret" {
		t.Fatalf("rotated key secret = %q, want new-secret", rotatedKey.Key)
	}

	disabledKey, err := store.SetAPIKeyEnabled(context.Background(), key.ID, false)
	if err != nil {
		t.Fatalf("SetAPIKeyEnabled() error = %v", err)
	}
	if disabledKey.Enabled {
		t.Fatal("expected api key to be disabled")
	}

	if err := store.DeleteTenant(context.Background(), tenant.ID); err == nil {
		t.Fatal("DeleteTenant() error = nil, want tenant referenced error")
	}

	if err := store.DeleteAPIKey(context.Background(), key.ID); err != nil {
		t.Fatalf("DeleteAPIKey() error = %v", err)
	}
	if err := store.DeleteTenant(context.Background(), tenant.ID); err != nil {
		t.Fatalf("DeleteTenant() error = %v", err)
	}
}

func TestFileStoreAuditEventsCaptureActorAndMutationTrail(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "control-plane.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

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

	path := filepath.Join(t.TempDir(), "control-plane.json")
	store, err := NewFileStore(path)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}

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
