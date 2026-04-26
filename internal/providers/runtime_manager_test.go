package providers

import (
	"context"
	"encoding/base64"
	"log/slog"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/secrets"
)

func TestControlPlaneRuntimeManagerUpsertReloadsRegistryAndEncryptsSecrets(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	store := controlplane.NewMemoryStore()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := secrets.NewAESGCMCipher(key)
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}

	manager := NewControlPlaneRuntimeManager(logger, []config.OpenAICompatibleProviderConfig{
		{Name: "openai", Kind: "cloud", Protocol: "openai", BaseURL: "https://api.openai.com", APIKey: "env-secret", DefaultModel: "gpt-4o-mini"},
	}, store, cipher)

	if err := manager.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	if _, err := manager.Upsert(context.Background(), controlplane.Provider{
		Name:         "groq",
		Kind:         "cloud",
		Protocol:     "openai",
		BaseURL:      "https://api.groq.com/openai/v1",
		DefaultModel: "llama-3.3-70b-versatile",
		Enabled:      true,
	}, "groq-secret"); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	registry := manager.Registry()
	groq, ok := registry.Get("groq")
	if !ok {
		t.Fatal("expected groq provider in registry after reload")
	}
	if groq.Kind() != KindCloud {
		t.Fatalf("groq.Kind() = %q, want cloud", groq.Kind())
	}

	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(state.ProviderSecrets) != 1 {
		t.Fatalf("provider secret count = %d, want 1", len(state.ProviderSecrets))
	}
	if state.ProviderSecrets[0].APIKeyEncrypted == "groq-secret" {
		t.Fatal("expected provider secret to be encrypted at rest")
	}
	if state.ProviderSecrets[0].APIKeyPreview == "" {
		t.Fatal("expected provider secret preview to be stored")
	}
}

func TestControlPlaneRuntimeManagerHydratesBuiltInProviderDefaults(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	store := controlplane.NewMemoryStore()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := secrets.NewAESGCMCipher(key)
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}

	manager := NewControlPlaneRuntimeManager(logger, nil, store, cipher)
	if _, err := manager.Upsert(context.Background(), controlplane.Provider{
		Name:    "groq",
		Enabled: true,
	}, "groq-secret"); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(state.Providers) != 1 {
		t.Fatalf("provider count = %d, want 1", len(state.Providers))
	}
	got := state.Providers[0]
	if got.BaseURL != "https://api.groq.com/openai/v1" {
		t.Fatalf("base url = %q, want groq default", got.BaseURL)
	}
	if got.Protocol != "openai" {
		t.Fatalf("protocol = %q, want openai", got.Protocol)
	}
	if got.Kind != "cloud" {
		t.Fatalf("kind = %q, want cloud", got.Kind)
	}
	if got.PresetID != "groq" {
		t.Fatalf("preset id = %q, want groq", got.PresetID)
	}
}

func TestControlPlaneRuntimeManagerPreservesExistingOverridesOnMinimalUpdate(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(testWriter{t}, nil))
	store := controlplane.NewMemoryStore()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	cipher, err := secrets.NewAESGCMCipher(key)
	if err != nil {
		t.Fatalf("NewAESGCMCipher() error = %v", err)
	}

	manager := NewControlPlaneRuntimeManager(logger, nil, store, cipher)
	if _, err := manager.Upsert(context.Background(), controlplane.Provider{
		Name:           "groq",
		DefaultModel:   "openai/gpt-oss-20b",
		ExplicitFields: []string{"default_model"},
		Enabled:        true,
	}, "groq-secret"); err != nil {
		t.Fatalf("initial Upsert() error = %v", err)
	}

	if _, err := manager.Upsert(context.Background(), controlplane.Provider{
		ID:      "groq",
		Name:    "groq",
		Enabled: true,
	}, ""); err != nil {
		t.Fatalf("minimal Upsert() error = %v", err)
	}

	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	got := state.Providers[0]
	if got.DefaultModel != "openai/gpt-oss-20b" {
		t.Fatalf("default model = %q, want preserved explicit override", got.DefaultModel)
	}
	if len(got.ExplicitFields) != 1 || got.ExplicitFields[0] != "default_model" {
		t.Fatalf("explicit fields = %#v, want [default_model]", got.ExplicitFields)
	}
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
