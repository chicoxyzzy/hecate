package controlplane

import (
	"context"
	"testing"
)

func newTestStore(t *testing.T) *MemoryStore {
	t.Helper()
	return NewMemoryStore()
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
