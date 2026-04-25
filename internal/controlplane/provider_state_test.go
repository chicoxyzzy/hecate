package controlplane

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	store, err := NewFileStore(filepath.Join(t.TempDir(), "cp.json"))
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	return store
}

func TestSetProviderEnabled_CreatesPlaceholderForBuiltIn(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	p, err := store.SetProviderEnabled(ctx, "openai", false)
	if err != nil {
		t.Fatalf("SetProviderEnabled() error = %v", err)
	}
	if p.Kind != "cloud" {
		t.Fatalf("placeholder Kind = %q, want cloud (hydrated from preset)", p.Kind)
	}
	if p.BaseURL == "" {
		t.Fatal("placeholder BaseURL empty — must inherit from built-in preset")
	}
	if p.Enabled {
		t.Fatal("placeholder should be disabled (we just disabled it)")
	}
}

func TestSetProviderEnabled_DoesNotOverwriteExistingFields(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// First disable creates placeholder.
	if _, err := store.SetProviderEnabled(ctx, "openai", false); err != nil {
		t.Fatalf("SetProviderEnabled() error = %v", err)
	}
	// Then re-enable — must keep the placeholder fields, just flip Enabled.
	p, err := store.SetProviderEnabled(ctx, "openai", true)
	if err != nil {
		t.Fatalf("SetProviderEnabled() error = %v", err)
	}
	if !p.Enabled {
		t.Fatal("Enabled = false after re-enable")
	}
	if p.BaseURL == "" {
		t.Fatal("BaseURL lost after re-enable")
	}
}

func TestSetProviderEnabled_ConflictResolutionIsTransitive(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// llamacpp + localai both at 127.0.0.1:8080.
	if _, err := store.SetProviderEnabled(ctx, "llamacpp", true); err != nil {
		t.Fatalf("enable llamacpp: %v", err)
	}
	if _, err := store.SetProviderEnabled(ctx, "localai", true); err != nil {
		t.Fatalf("enable localai: %v", err)
	}
	// llamacpp now disabled. Re-enable it.
	if _, err := store.SetProviderEnabled(ctx, "llamacpp", true); err != nil {
		t.Fatalf("re-enable llamacpp: %v", err)
	}

	state, _ := store.Snapshot(ctx)
	enabled := map[string]bool{}
	for _, p := range state.Providers {
		enabled[p.ID] = p.Enabled
	}
	if !enabled["llamacpp"] {
		t.Fatal("llamacpp should be enabled (we just re-enabled it)")
	}
	if enabled["localai"] {
		t.Fatal("localai should be disabled (auto-resolution)")
	}
}

func TestSetProviderEnabled_NoConflictWhenBaseURLDiffers(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// llamacpp + ollama have different ports — no conflict.
	if _, err := store.SetProviderEnabled(ctx, "llamacpp", true); err != nil {
		t.Fatalf("enable llamacpp: %v", err)
	}
	if _, err := store.SetProviderEnabled(ctx, "ollama", true); err != nil {
		t.Fatalf("enable ollama: %v", err)
	}

	state, _ := store.Snapshot(ctx)
	enabled := map[string]bool{}
	for _, p := range state.Providers {
		enabled[p.ID] = p.Enabled
	}
	if !enabled["llamacpp"] || !enabled["ollama"] {
		t.Fatalf("both should be enabled, got: %v", enabled)
	}
}

func TestRotateProviderSecret_AutoCreatesPlaceholder(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	p, err := store.RotateProviderSecret(ctx, "openai", ProviderSecret{
		ProviderID:      "openai",
		APIKeyEncrypted: "ciphertext",
		APIKeyPreview:   "sk...xyz",
	})
	if err != nil {
		t.Fatalf("RotateProviderSecret() error = %v", err)
	}
	if p.Kind != "cloud" {
		t.Fatalf("Kind = %q, want cloud (placeholder hydrated)", p.Kind)
	}
	if p.CredentialID == "" {
		t.Fatal("CredentialID not set after rotate")
	}
}

func TestRotateProviderSecret_RejectsUnknownProvider(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	_, err := store.RotateProviderSecret(context.Background(), "totally-not-real", ProviderSecret{
		ProviderID:      "totally-not-real",
		APIKeyEncrypted: "x",
	})
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestRotateProviderSecret_RejectsEmptyCiphertext(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)

	_, err := store.RotateProviderSecret(context.Background(), "openai", ProviderSecret{
		ProviderID:      "openai",
		APIKeyEncrypted: "",
	})
	if err == nil {
		t.Fatal("expected error for empty ciphertext")
	}
}

func TestDeleteProviderCredential_KeepsRecordRemovesSecret(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, _ = store.RotateProviderSecret(ctx, "openai", ProviderSecret{
		ProviderID: "openai", APIKeyEncrypted: "ciphertext",
	})

	p, err := store.DeleteProviderCredential(ctx, "openai")
	if err != nil {
		t.Fatalf("DeleteProviderCredential() error = %v", err)
	}
	if p.CredentialID != "" {
		t.Fatal("CredentialID should be cleared")
	}
	state, _ := store.Snapshot(ctx)
	for _, s := range state.ProviderSecrets {
		if s.ProviderID == "openai" {
			t.Fatal("secret should be removed from store")
		}
	}
	// The provider record itself stays so other state (e.g., enabled flag) survives.
	found := false
	for _, p := range state.Providers {
		if p.ID == "openai" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("provider record should remain after credential delete")
	}
}

func TestProviderConflictResolution_AlphabeticalDefault(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	// Reverse-alphabetical: localai first, then llamacpp.
	if _, err := store.SetProviderEnabled(ctx, "localai", true); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := store.SetProviderEnabled(ctx, "llamacpp", true); err != nil {
		t.Fatalf("%v", err)
	}

	// Most-recent-wins: enabling llamacpp should disable localai.
	state, _ := store.Snapshot(ctx)
	for _, p := range state.Providers {
		if p.ID == "localai" && p.Enabled {
			t.Fatal("localai should be auto-disabled after enabling llamacpp")
		}
	}
}

func TestProviderAuditEvents_RecordedForEnableDisable(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.SetProviderEnabled(ctx, "openai", false); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := store.SetProviderEnabled(ctx, "openai", true); err != nil {
		t.Fatalf("%v", err)
	}

	state, _ := store.Snapshot(ctx)
	enableEvents := 0
	for _, e := range state.Events {
		if e.Action == "provider.enabled_changed" {
			enableEvents++
		}
	}
	if enableEvents < 2 {
		t.Fatalf("expected ≥2 provider.enabled_changed events, got %d", enableEvents)
	}
}

func TestResolveProviderBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		want     string
	}{
		{"explicit base url", Provider{ID: "x", BaseURL: "https://example.com/v1"}, "https://example.com/v1"},
		{"falls back to built-in", Provider{ID: "openai"}, "https://api.openai.com"},
		{"unknown id no fallback", Provider{ID: "totally-unknown"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveProviderBaseURL(tt.provider); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
