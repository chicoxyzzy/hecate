package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
)

type fakeStore struct {
	state controlplane.State
}

func (f fakeStore) Backend() string { return "fake" }
func (f fakeStore) Snapshot(context.Context) (controlplane.State, error) {
	return f.state, nil
}
func (f fakeStore) UpsertTenant(context.Context, controlplane.Tenant) (controlplane.Tenant, error) {
	return controlplane.Tenant{}, nil
}
func (f fakeStore) UpsertAPIKey(context.Context, controlplane.APIKey) (controlplane.APIKey, error) {
	return controlplane.APIKey{}, nil
}
func (f fakeStore) SetTenantEnabled(context.Context, string, bool) (controlplane.Tenant, error) {
	return controlplane.Tenant{}, nil
}
func (f fakeStore) DeleteTenant(context.Context, string) error { return nil }
func (f fakeStore) SetAPIKeyEnabled(context.Context, string, bool) (controlplane.APIKey, error) {
	return controlplane.APIKey{}, nil
}
func (f fakeStore) RotateAPIKey(context.Context, string, string) (controlplane.APIKey, error) {
	return controlplane.APIKey{}, nil
}
func (f fakeStore) DeleteAPIKey(context.Context, string) error { return nil }

func TestBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		header string
		want   string
	}{
		{header: "", want: ""},
		{header: "Token abc", want: ""},
		{header: "Bearer abc", want: "abc"},
		{header: "Bearer   abc  ", want: "abc"},
	}

	for _, tt := range tests {
		if got := bearerToken(tt.header); got != tt.want {
			t.Fatalf("bearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestAuthenticateAdminToken(t *testing.T) {
	t.Parallel()

	authn := NewAuthenticator(config.ServerConfig{AuthToken: "root-secret"}, nil)
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer root-secret")

	principal, ok := authn.Authenticate(req)
	if !ok {
		t.Fatal("Authenticate() ok = false, want true")
	}
	if !principal.IsAdmin() {
		t.Fatalf("principal role = %q, want admin", principal.Role)
	}
	if principal.Source != "admin_token" {
		t.Fatalf("source = %q, want admin_token", principal.Source)
	}
}

func TestAuthenticateControlPlaneAPIKeyMergesTenantRestrictions(t *testing.T) {
	t.Parallel()

	store := fakeStore{
		state: controlplane.State{
			Tenants: []controlplane.Tenant{{
				ID:               "acme",
				Enabled:          true,
				AllowedProviders: []string{"openai", "ollama"},
				AllowedModels:    []string{"gpt-4o-mini", "llama3.1:8b"},
			}},
			APIKeys: []controlplane.APIKey{{
				ID:               "key-1",
				Name:             "tenant-key",
				Key:              "tenant-secret",
				Tenant:           "acme",
				Role:             "tenant",
				Enabled:          true,
				AllowedProviders: []string{"ollama", "anthropic"},
				AllowedModels:    []string{"llama3.1:8b", "claude-sonnet"},
			}},
		},
	}
	authn := NewAuthenticator(config.ServerConfig{}, store)
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tenant-secret")

	principal, ok := authn.Authenticate(req)
	if !ok {
		t.Fatal("Authenticate() ok = false, want true")
	}
	if principal.Source != "control_plane_api_key" {
		t.Fatalf("source = %q, want control_plane_api_key", principal.Source)
	}
	if len(principal.AllowedProviders) != 1 || principal.AllowedProviders[0] != "ollama" {
		t.Fatalf("allowed providers = %#v, want [ollama]", principal.AllowedProviders)
	}
	if len(principal.AllowedModels) != 1 || principal.AllowedModels[0] != "llama3.1:8b" {
		t.Fatalf("allowed models = %#v, want [llama3.1:8b]", principal.AllowedModels)
	}
}

func TestAuthenticateRejectsDisabledTenantKey(t *testing.T) {
	t.Parallel()

	store := fakeStore{
		state: controlplane.State{
			Tenants: []controlplane.Tenant{{ID: "acme", Enabled: false}},
			APIKeys: []controlplane.APIKey{{
				ID:      "key-1",
				Name:    "tenant-key",
				Key:     "tenant-secret",
				Tenant:  "acme",
				Role:    "tenant",
				Enabled: true,
			}},
		},
	}
	authn := NewAuthenticator(config.ServerConfig{}, store)
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer tenant-secret")

	if _, ok := authn.Authenticate(req); ok {
		t.Fatal("Authenticate() ok = true, want false for disabled tenant")
	}
}
