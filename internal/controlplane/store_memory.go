package controlplane

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
)

// MemoryStore is an in-memory control plane store. State is lost on restart.
// It is used as the default backend when no persistent store is configured,
// allowing provider toggling and other control-plane operations without
// requiring external storage.
type MemoryStore struct {
	mu   sync.RWMutex
	data State
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{}
}

func (s *MemoryStore) Backend() string { return "memory" }

func (s *MemoryStore) Snapshot(_ context.Context) (State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneState(s.data), nil
}

func (s *MemoryStore) UpsertTenant(ctx context.Context, tenant Tenant) (Tenant, error) {
	tenant.ID = canonicalID(tenant.ID, tenant.Name)
	if tenant.ID == "" {
		return Tenant{}, fmt.Errorf("tenant id or name is required")
	}
	if tenant.Name == "" {
		tenant.Name = tenant.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	action := "tenant.created"
	for i := range s.data.Tenants {
		if s.data.Tenants[i].ID == tenant.ID {
			index = i
			action = "tenant.updated"
			break
		}
	}
	if index >= 0 {
		s.data.Tenants[index] = tenant
	} else {
		if !tenant.Enabled {
			tenant.Enabled = true
		}
		s.data.Tenants = append(s.data.Tenants, tenant)
	}
	appendAuditEvent(&s.data, newAuditEvent(ctx, action, "tenant", tenant.ID, tenant.Name))
	return tenant, nil
}

func (s *MemoryStore) SetTenantEnabled(ctx context.Context, id string, enabled bool) (Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := tenantIndex(s.data.Tenants, id)
	if index < 0 {
		return Tenant{}, fmt.Errorf("tenant %q not found", id)
	}
	s.data.Tenants[index].Enabled = enabled
	appendAuditEvent(&s.data, newAuditEvent(ctx, "tenant.enabled_changed", "tenant", s.data.Tenants[index].ID, fmt.Sprintf("enabled=%t", enabled)))
	return s.data.Tenants[index], nil
}

func (s *MemoryStore) DeleteTenant(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := tenantIndex(s.data.Tenants, id)
	if index < 0 {
		return fmt.Errorf("tenant %q not found", id)
	}
	if tenantReferencedByAPIKeys(s.data.APIKeys, id) {
		return fmt.Errorf("tenant %q still has api keys; delete or reassign keys first", id)
	}
	appendAuditEvent(&s.data, newAuditEvent(ctx, "tenant.deleted", "tenant", s.data.Tenants[index].ID, s.data.Tenants[index].Name))
	s.data.Tenants = append(s.data.Tenants[:index], s.data.Tenants[index+1:]...)
	return nil
}

func (s *MemoryStore) UpsertAPIKey(ctx context.Context, key APIKey) (APIKey, error) {
	key.ID = canonicalID(key.ID, key.Name)
	if key.ID == "" {
		return APIKey{}, fmt.Errorf("api key id or name is required")
	}
	if key.Name == "" {
		key.Name = key.ID
	}
	if key.Role == "" {
		key.Role = "tenant"
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	action := "api_key.created"
	for i := range s.data.APIKeys {
		if s.data.APIKeys[i].ID == key.ID {
			index = i
			action = "api_key.updated"
			break
		}
	}
	if key.Tenant != "" && !tenantExists(s.data.Tenants, key.Tenant) {
		return APIKey{}, fmt.Errorf("tenant %q does not exist", key.Tenant)
	}
	if index >= 0 {
		existing := s.data.APIKeys[index]
		if key.Key == "" {
			key.Key = existing.Key
		}
		if key.CreatedAt.IsZero() {
			key.CreatedAt = existing.CreatedAt
		}
		key.UpdatedAt = now
	} else {
		if key.Key == "" {
			return APIKey{}, fmt.Errorf("api key secret is required when creating a key")
		}
		if !key.Enabled {
			key.Enabled = true
		}
		key.CreatedAt = now
		key.UpdatedAt = now
	}
	if index >= 0 {
		s.data.APIKeys[index] = key
	} else {
		s.data.APIKeys = append(s.data.APIKeys, key)
	}
	appendAuditEvent(&s.data, newAuditEvent(ctx, action, "api_key", key.ID, key.Name))
	return key, nil
}

func (s *MemoryStore) SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) (APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := apiKeyIndex(s.data.APIKeys, id)
	if index < 0 {
		return APIKey{}, fmt.Errorf("api key %q not found", id)
	}
	s.data.APIKeys[index].Enabled = enabled
	s.data.APIKeys[index].UpdatedAt = time.Now().UTC()
	appendAuditEvent(&s.data, newAuditEvent(ctx, "api_key.enabled_changed", "api_key", s.data.APIKeys[index].ID, fmt.Sprintf("enabled=%t", enabled)))
	return s.data.APIKeys[index], nil
}

func (s *MemoryStore) RotateAPIKey(ctx context.Context, id, secret string) (APIKey, error) {
	if strings.TrimSpace(secret) == "" {
		return APIKey{}, fmt.Errorf("api key secret is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := apiKeyIndex(s.data.APIKeys, id)
	if index < 0 {
		return APIKey{}, fmt.Errorf("api key %q not found", id)
	}
	s.data.APIKeys[index].Key = secret
	s.data.APIKeys[index].UpdatedAt = time.Now().UTC()
	appendAuditEvent(&s.data, newAuditEvent(ctx, "api_key.rotated", "api_key", s.data.APIKeys[index].ID, s.data.APIKeys[index].Name))
	return s.data.APIKeys[index], nil
}

func (s *MemoryStore) DeleteAPIKey(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := apiKeyIndex(s.data.APIKeys, id)
	if index < 0 {
		return fmt.Errorf("api key %q not found", id)
	}
	appendAuditEvent(&s.data, newAuditEvent(ctx, "api_key.deleted", "api_key", s.data.APIKeys[index].ID, s.data.APIKeys[index].Name))
	s.data.APIKeys = append(s.data.APIKeys[:index], s.data.APIKeys[index+1:]...)
	return nil
}

func (s *MemoryStore) UpsertProvider(ctx context.Context, provider Provider, secret *ProviderSecret) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := applyProviderUpsert(ctx, &s.data, provider, secret)
	return p, err
}

func (s *MemoryStore) RotateProviderSecret(ctx context.Context, id string, secret ProviderSecret) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return applyRotateProviderSecret(ctx, &s.data, id, secret)
}

func (s *MemoryStore) DeleteProviderCredential(ctx context.Context, id string) (Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return applyDeleteProviderCredential(ctx, &s.data, id)
}

func (s *MemoryStore) DeleteProvider(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return applyDeleteProvider(ctx, &s.data, id)
}

func (s *MemoryStore) UpsertPolicyRule(ctx context.Context, rule config.PolicyRuleConfig) (config.PolicyRuleConfig, error) {
	rule, err := normalizePolicyRule(rule)
	if err != nil {
		return config.PolicyRuleConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	action := upsertPolicyRule(&s.data, rule)
	appendAuditEvent(&s.data, newAuditEvent(ctx, action, "policy_rule", rule.ID, rule.Action))
	return rule, nil
}

func (s *MemoryStore) DeletePolicyRule(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("policy rule id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := policyRuleIndex(s.data.PolicyRules, id)
	if index < 0 {
		return fmt.Errorf("policy rule %q not found", id)
	}
	appendAuditEvent(&s.data, newAuditEvent(ctx, "policy_rule.deleted", "policy_rule", s.data.PolicyRules[index].ID, s.data.PolicyRules[index].Action))
	s.data.PolicyRules = append(s.data.PolicyRules[:index], s.data.PolicyRules[index+1:]...)
	return nil
}

func (s *MemoryStore) UpsertPricebookEntry(ctx context.Context, entry config.ModelPriceConfig) (config.ModelPriceConfig, error) {
	entry, err := normalizePricebookEntry(entry)
	if err != nil {
		return config.ModelPriceConfig{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	action := upsertPricebookEntry(&s.data, entry)
	appendAuditEvent(&s.data, newAuditEvent(ctx, action, "pricebook_entry", pricebookEntryID(entry.Provider, entry.Model), ""))
	return entry, nil
}

func (s *MemoryStore) DeletePricebookEntry(ctx context.Context, provider, model string) error {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return fmt.Errorf("pricebook provider and model are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := pricebookEntryIndex(s.data.Pricebook, provider, model)
	if index < 0 {
		return fmt.Errorf("pricebook entry %q not found", pricebookEntryID(provider, model))
	}
	appendAuditEvent(&s.data, newAuditEvent(ctx, "pricebook_entry.deleted", "pricebook_entry", pricebookEntryID(provider, model), ""))
	s.data.Pricebook = append(s.data.Pricebook[:index], s.data.Pricebook[index+1:]...)
	return nil
}

func (s *MemoryStore) PruneAuditEvents(_ context.Context, maxAge time.Duration, maxCount int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return pruneAuditEvents(&s.data, maxAge, maxCount), nil
}
