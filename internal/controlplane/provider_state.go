package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func applyProviderUpsert(ctx context.Context, state *State, provider Provider, secret *ProviderSecret) (Provider, error) {
	if state == nil {
		return Provider{}, fmt.Errorf("control plane state is required")
	}

	provider.ID = canonicalID(provider.ID, provider.Name)
	if provider.ID == "" {
		return Provider{}, fmt.Errorf("provider id or name is required")
	}
	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = provider.ID
	}
	if strings.TrimSpace(provider.Kind) == "" {
		provider.Kind = "cloud"
	}
	if strings.TrimSpace(provider.Protocol) == "" {
		provider.Protocol = "openai"
	}
	if strings.TrimSpace(provider.BaseURL) == "" {
		return Provider{}, fmt.Errorf("provider base_url is required")
	}

	now := time.Now().UTC()
	index := providerIndex(state.Providers, provider.ID)
	action := "provider.created"
	if index >= 0 {
		existing := state.Providers[index]
		action = "provider.updated"
		if provider.CreatedAt.IsZero() {
			provider.CreatedAt = existing.CreatedAt
		}
		if provider.CredentialID == "" {
			provider.CredentialID = existing.CredentialID
		}
	} else {
		provider.CreatedAt = now
	}
	provider.UpdatedAt = now

	if secret != nil {
		secret.ID = canonicalID(secret.ID, provider.ID+"-credential")
		secret.ProviderID = provider.ID
		if strings.TrimSpace(secret.APIKeyEncrypted) == "" {
			return Provider{}, fmt.Errorf("provider secret ciphertext is required")
		}
		if secret.CreatedAt.IsZero() {
			secret.CreatedAt = now
		}
		secret.RotatedAt = now
		upsertProviderSecret(state, *secret)
		provider.CredentialID = secret.ID
	}

	if index >= 0 {
		state.Providers[index] = provider
	} else {
		state.Providers = append(state.Providers, provider)
	}

	appendAuditEvent(state, newAuditEvent(ctx, action, "provider", provider.ID, provider.Name))
	return provider, nil
}

func applySetProviderEnabled(ctx context.Context, state *State, id string, enabled bool) (Provider, error) {
	index := providerIndex(state.Providers, id)
	if index < 0 {
		// Provider is not in the control-plane store (e.g. env-configured).
		// Create a minimal override entry so the enabled flag is persisted.
		p := Provider{
			ID:        id,
			Name:      id,
			Enabled:   enabled,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		state.Providers = append(state.Providers, p)
		appendAuditEvent(state, newAuditEvent(ctx, "provider.enabled_changed", "provider", p.ID, fmt.Sprintf("enabled=%t", enabled)))
		return p, nil
	}
	state.Providers[index].Enabled = enabled
	state.Providers[index].UpdatedAt = time.Now().UTC()
	appendAuditEvent(state, newAuditEvent(ctx, "provider.enabled_changed", "provider", state.Providers[index].ID, fmt.Sprintf("enabled=%t", enabled)))
	return state.Providers[index], nil
}

func applyRotateProviderSecret(ctx context.Context, state *State, id string, secret ProviderSecret) (Provider, error) {
	index := providerIndex(state.Providers, id)
	if index < 0 {
		return Provider{}, fmt.Errorf("provider %q not found", id)
	}
	if strings.TrimSpace(secret.APIKeyEncrypted) == "" {
		return Provider{}, fmt.Errorf("provider secret ciphertext is required")
	}

	existingSecret := providerSecretByProviderID(state.ProviderSecrets, id)
	now := time.Now().UTC()
	secret.ProviderID = id
	if existingSecret != nil {
		secret.ID = existingSecret.ID
		if secret.CreatedAt.IsZero() {
			secret.CreatedAt = existingSecret.CreatedAt
		}
	} else {
		secret.ID = canonicalID(secret.ID, id+"-credential")
		secret.CreatedAt = now
	}
	secret.RotatedAt = now
	upsertProviderSecret(state, secret)

	state.Providers[index].CredentialID = secret.ID
	state.Providers[index].UpdatedAt = now
	appendAuditEvent(state, newAuditEvent(ctx, "provider.secret_rotated", "provider", state.Providers[index].ID, state.Providers[index].Name))
	return state.Providers[index], nil
}

func applyDeleteProviderCredential(ctx context.Context, state *State, id string) (Provider, error) {
	index := providerIndex(state.Providers, id)
	if index < 0 {
		return Provider{}, fmt.Errorf("provider %q not found", id)
	}
	deleteProviderSecret(state, id)
	state.Providers[index].CredentialID = ""
	state.Providers[index].UpdatedAt = time.Now().UTC()
	appendAuditEvent(state, newAuditEvent(ctx, "provider.credential_deleted", "provider", state.Providers[index].ID, state.Providers[index].Name))
	return state.Providers[index], nil
}

func applyDeleteProvider(ctx context.Context, state *State, id string) error {
	index := providerIndex(state.Providers, id)
	if index < 0 {
		return fmt.Errorf("provider %q not found", id)
	}
	appendAuditEvent(state, newAuditEvent(ctx, "provider.deleted", "provider", state.Providers[index].ID, state.Providers[index].Name))
	state.Providers = append(state.Providers[:index], state.Providers[index+1:]...)
	deleteProviderSecret(state, id)
	return nil
}

func providerIndex(items []Provider, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

func providerSecretByProviderID(items []ProviderSecret, providerID string) *ProviderSecret {
	for i := range items {
		if items[i].ProviderID == providerID {
			return &items[i]
		}
	}
	return nil
}

func upsertProviderSecret(state *State, secret ProviderSecret) {
	for i := range state.ProviderSecrets {
		if state.ProviderSecrets[i].ProviderID == secret.ProviderID {
			state.ProviderSecrets[i] = secret
			return
		}
	}
	state.ProviderSecrets = append(state.ProviderSecrets, secret)
}

func deleteProviderSecret(state *State, providerID string) {
	filtered := state.ProviderSecrets[:0]
	for _, secret := range state.ProviderSecrets {
		if secret.ProviderID == providerID {
			continue
		}
		filtered = append(filtered, secret)
	}
	state.ProviderSecrets = append([]ProviderSecret(nil), filtered...)
}
