package requestscope

import (
	"testing"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestBuildAndMetadataRoundTrip(t *testing.T) {
	t.Parallel()

	scope := Build(auth.Principal{
		Role:             "tenant",
		Tenant:           "team-a",
		AllowedProviders: []string{"ollama", "openai"},
		AllowedModels:    []string{"llama3.1:8b", "gpt-4o-mini"},
	}, "team-a", "ollama")

	got := FromChatRequest(types.ChatRequest{
		Scope: scope,
	})

	if got.Tenant != "team-a" {
		t.Fatalf("tenant = %q, want team-a", got.Tenant)
	}
	if got.User != "team-a" {
		t.Fatalf("user = %q, want team-a", got.User)
	}
	if got.ProviderHint != "ollama" {
		t.Fatalf("provider_hint = %q, want ollama", got.ProviderHint)
	}
	if len(got.AllowedProviders) != 2 || got.AllowedProviders[0] != "ollama" || got.AllowedProviders[1] != "openai" {
		t.Fatalf("allowed providers = %#v, want sorted unique providers", got.AllowedProviders)
	}
	if len(got.AllowedModels) != 2 || got.AllowedModels[0] != "gpt-4o-mini" || got.AllowedModels[1] != "llama3.1:8b" {
		t.Fatalf("allowed models = %#v, want sorted unique models", got.AllowedModels)
	}
	if got.Principal.Role != "tenant" {
		t.Fatalf("principal role = %q, want tenant", got.Principal.Role)
	}
}

func TestFromChatRequestUsesTypedScope(t *testing.T) {
	t.Parallel()

	got := FromChatRequest(types.ChatRequest{
		Scope: types.RequestScope{
			Tenant:           "team-a",
			User:             "team-a",
			ProviderHint:     "openai",
			AllowedProviders: []string{" openai ", "ollama"},
			AllowedModels:    []string{" llama3.1:8b ", "gpt-4o-mini"},
			Principal: types.PrincipalContext{
				Role: "tenant",
			},
		},
	})

	if got.Tenant != "team-a" {
		t.Fatalf("tenant = %q, want team-a", got.Tenant)
	}
	if got.ProviderHint != "openai" {
		t.Fatalf("provider_hint = %q, want openai", got.ProviderHint)
	}
	if len(got.AllowedProviders) != 2 {
		t.Fatalf("allowed providers = %#v, want 2", got.AllowedProviders)
	}
	if got.Principal.Role != "tenant" {
		t.Fatalf("principal role = %q, want tenant", got.Principal.Role)
	}
}

func TestEffectiveTenantUsesFallbacks(t *testing.T) {
	t.Parallel()

	if got := EffectiveTenant(types.RequestScope{}, "fallback"); got != "fallback" {
		t.Fatalf("EffectiveTenant() = %q, want fallback", got)
	}
	if got := EffectiveTenant(types.RequestScope{}, ""); got != "anonymous" {
		t.Fatalf("EffectiveTenant() = %q, want anonymous", got)
	}
}
