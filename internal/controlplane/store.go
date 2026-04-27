package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/storage"
)

type Tenant struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
	Enabled          bool     `json:"enabled"`
}

type APIKey struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Key              string    `json:"key"`
	Tenant           string    `json:"tenant,omitempty"`
	Role             string    `json:"role"`
	AllowedProviders []string  `json:"allowed_providers,omitempty"`
	AllowedModels    []string  `json:"allowed_models,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at,omitempty"`
}

type Provider struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	PresetID       string    `json:"preset_id,omitempty"`
	Kind           string    `json:"kind"`
	Protocol       string    `json:"protocol"`
	BaseURL        string    `json:"base_url"`
	APIVersion     string    `json:"api_version,omitempty"`
	DefaultModel   string    `json:"default_model,omitempty"`
	ExplicitFields []string  `json:"explicit_fields,omitempty"`
	Enabled        bool      `json:"enabled"`
	CredentialID   string    `json:"credential_id,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
}

type ProviderSecret struct {
	ID              string    `json:"id"`
	ProviderID      string    `json:"provider_id"`
	APIKeyEncrypted string    `json:"api_key_encrypted"`
	APIKeyPreview   string    `json:"api_key_preview,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
	RotatedAt       time.Time `json:"rotated_at,omitempty"`
}

type AuditEvent struct {
	Timestamp  time.Time `json:"timestamp"`
	Actor      string    `json:"actor"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Detail     string    `json:"detail,omitempty"`
}

type State struct {
	Tenants         []Tenant                  `json:"tenants"`
	APIKeys         []APIKey                  `json:"api_keys"`
	Providers       []Provider                `json:"providers,omitempty"`
	ProviderSecrets []ProviderSecret          `json:"provider_secrets,omitempty"`
	PolicyRules     []config.PolicyRuleConfig `json:"policy_rules,omitempty"`
	Pricebook       []config.ModelPriceConfig `json:"pricebook,omitempty"`
	Events          []AuditEvent              `json:"events,omitempty"`
}

type Store interface {
	Backend() string
	Snapshot(ctx context.Context) (State, error)
	UpsertTenant(ctx context.Context, tenant Tenant) (Tenant, error)
	UpsertAPIKey(ctx context.Context, key APIKey) (APIKey, error)
	SetTenantEnabled(ctx context.Context, id string, enabled bool) (Tenant, error)
	DeleteTenant(ctx context.Context, id string) error
	SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) (APIKey, error)
	RotateAPIKey(ctx context.Context, id, secret string) (APIKey, error)
	DeleteAPIKey(ctx context.Context, id string) error
	UpsertProvider(ctx context.Context, provider Provider, secret *ProviderSecret) (Provider, error)
	SetProviderEnabled(ctx context.Context, id string, enabled bool) (Provider, error)
	RotateProviderSecret(ctx context.Context, id string, secret ProviderSecret) (Provider, error)
	DeleteProviderCredential(ctx context.Context, id string) (Provider, error)
	DeleteProvider(ctx context.Context, id string) error
	UpsertPolicyRule(ctx context.Context, rule config.PolicyRuleConfig) (config.PolicyRuleConfig, error)
	DeletePolicyRule(ctx context.Context, id string) error
	UpsertPricebookEntry(ctx context.Context, entry config.ModelPriceConfig) (config.ModelPriceConfig, error)
	DeletePricebookEntry(ctx context.Context, provider, model string) error
	PruneAuditEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
}

type redisClient interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
}

type RedisStore struct {
	client redisClient
	key    string
	mu     sync.Mutex
}

type actorContextKey struct{}

const maxAuditEvents = 100

func WithActor(ctx context.Context, actor string) context.Context {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorContextKey{}, actor)
}

func NewRedisStore(client *storage.RedisClient, prefix, key string) (*RedisStore, error) {
	return NewRedisStoreFromClient(client, prefix, key)
}

func NewRedisStoreFromClient(client redisClient, prefix, key string) (*RedisStore, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "control-plane"
	}
	if prefix != "" {
		key = prefix + ":" + key
	}

	store := &RedisStore{
		client: client,
		key:    key,
	}
	if _, err := store.readState(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *RedisStore) Backend() string {
	return "redis"
}

func (s *RedisStore) Key() string {
	return s.key
}

func (s *RedisStore) Snapshot(ctx context.Context) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readState(ctx)
}

func (s *RedisStore) UpsertTenant(ctx context.Context, tenant Tenant) (Tenant, error) {
	tenant.ID = canonicalID(tenant.ID, tenant.Name)
	if tenant.ID == "" {
		return Tenant{}, fmt.Errorf("tenant id or name is required")
	}
	if tenant.Name == "" {
		tenant.Name = tenant.ID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return Tenant{}, err
	}

	index := -1
	action := "tenant.created"
	for i := range state.Tenants {
		if state.Tenants[i].ID == tenant.ID {
			index = i
			action = "tenant.updated"
			break
		}
	}
	if index >= 0 {
		state.Tenants[index] = tenant
	} else {
		if !tenant.Enabled {
			tenant.Enabled = true
		}
		state.Tenants = append(state.Tenants, tenant)
	}
	appendAuditEvent(&state, newAuditEvent(ctx, action, "tenant", tenant.ID, tenant.Name))

	if err := s.writeState(ctx, state); err != nil {
		return Tenant{}, err
	}
	return tenant, nil
}

func (s *RedisStore) UpsertAPIKey(ctx context.Context, key APIKey) (APIKey, error) {
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

	state, err := s.readState(ctx)
	if err != nil {
		return APIKey{}, err
	}

	index := -1
	action := "api_key.created"
	for i := range state.APIKeys {
		if state.APIKeys[i].ID == key.ID {
			index = i
			action = "api_key.updated"
			break
		}
	}

	if key.Tenant != "" && !tenantExists(state.Tenants, key.Tenant) {
		return APIKey{}, fmt.Errorf("tenant %q does not exist", key.Tenant)
	}

	if index >= 0 {
		existing := state.APIKeys[index]
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
		state.APIKeys[index] = key
	} else {
		state.APIKeys = append(state.APIKeys, key)
	}
	appendAuditEvent(&state, newAuditEvent(ctx, action, "api_key", key.ID, key.Name))

	if err := s.writeState(ctx, state); err != nil {
		return APIKey{}, err
	}
	return key, nil
}

func (s *RedisStore) SetTenantEnabled(ctx context.Context, id string, enabled bool) (Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return Tenant{}, err
	}
	index := tenantIndex(state.Tenants, id)
	if index < 0 {
		return Tenant{}, fmt.Errorf("tenant %q not found", id)
	}
	state.Tenants[index].Enabled = enabled
	appendAuditEvent(&state, newAuditEvent(ctx, "tenant.enabled_changed", "tenant", state.Tenants[index].ID, fmt.Sprintf("enabled=%t", enabled)))
	if err := s.writeState(ctx, state); err != nil {
		return Tenant{}, err
	}
	return state.Tenants[index], nil
}

func (s *RedisStore) DeleteTenant(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return err
	}
	index := tenantIndex(state.Tenants, id)
	if index < 0 {
		return fmt.Errorf("tenant %q not found", id)
	}
	if tenantReferencedByAPIKeys(state.APIKeys, id) {
		return fmt.Errorf("tenant %q still has api keys; delete or reassign keys first", id)
	}

	appendAuditEvent(&state, newAuditEvent(ctx, "tenant.deleted", "tenant", state.Tenants[index].ID, state.Tenants[index].Name))
	state.Tenants = append(state.Tenants[:index], state.Tenants[index+1:]...)
	return s.writeState(ctx, state)
}

func (s *RedisStore) SetAPIKeyEnabled(ctx context.Context, id string, enabled bool) (APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return APIKey{}, err
	}
	index := apiKeyIndex(state.APIKeys, id)
	if index < 0 {
		return APIKey{}, fmt.Errorf("api key %q not found", id)
	}
	state.APIKeys[index].Enabled = enabled
	state.APIKeys[index].UpdatedAt = time.Now().UTC()
	appendAuditEvent(&state, newAuditEvent(ctx, "api_key.enabled_changed", "api_key", state.APIKeys[index].ID, fmt.Sprintf("enabled=%t", enabled)))
	if err := s.writeState(ctx, state); err != nil {
		return APIKey{}, err
	}
	return state.APIKeys[index], nil
}

func (s *RedisStore) RotateAPIKey(ctx context.Context, id, secret string) (APIKey, error) {
	if strings.TrimSpace(secret) == "" {
		return APIKey{}, fmt.Errorf("api key secret is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return APIKey{}, err
	}
	index := apiKeyIndex(state.APIKeys, id)
	if index < 0 {
		return APIKey{}, fmt.Errorf("api key %q not found", id)
	}
	state.APIKeys[index].Key = secret
	state.APIKeys[index].UpdatedAt = time.Now().UTC()
	appendAuditEvent(&state, newAuditEvent(ctx, "api_key.rotated", "api_key", state.APIKeys[index].ID, state.APIKeys[index].Name))
	if err := s.writeState(ctx, state); err != nil {
		return APIKey{}, err
	}
	return state.APIKeys[index], nil
}

func (s *RedisStore) DeleteAPIKey(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return err
	}
	index := apiKeyIndex(state.APIKeys, id)
	if index < 0 {
		return fmt.Errorf("api key %q not found", id)
	}
	appendAuditEvent(&state, newAuditEvent(ctx, "api_key.deleted", "api_key", state.APIKeys[index].ID, state.APIKeys[index].Name))
	state.APIKeys = append(state.APIKeys[:index], state.APIKeys[index+1:]...)
	return s.writeState(ctx, state)
}

func (s *RedisStore) UpsertPolicyRule(ctx context.Context, rule config.PolicyRuleConfig) (config.PolicyRuleConfig, error) {
	rule, err := normalizePolicyRule(rule)
	if err != nil {
		return config.PolicyRuleConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return config.PolicyRuleConfig{}, err
	}
	action := upsertPolicyRule(&state, rule)
	appendAuditEvent(&state, newAuditEvent(ctx, action, "policy_rule", rule.ID, rule.Action))
	if err := s.writeState(ctx, state); err != nil {
		return config.PolicyRuleConfig{}, err
	}
	return rule, nil
}

func (s *RedisStore) DeletePolicyRule(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("policy rule id is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return err
	}
	index := policyRuleIndex(state.PolicyRules, id)
	if index < 0 {
		return fmt.Errorf("policy rule %q not found", id)
	}
	appendAuditEvent(&state, newAuditEvent(ctx, "policy_rule.deleted", "policy_rule", state.PolicyRules[index].ID, state.PolicyRules[index].Action))
	state.PolicyRules = append(state.PolicyRules[:index], state.PolicyRules[index+1:]...)
	return s.writeState(ctx, state)
}

func (s *RedisStore) UpsertPricebookEntry(ctx context.Context, entry config.ModelPriceConfig) (config.ModelPriceConfig, error) {
	entry, err := normalizePricebookEntry(entry)
	if err != nil {
		return config.ModelPriceConfig{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return config.ModelPriceConfig{}, err
	}
	action := upsertPricebookEntry(&state, entry)
	appendAuditEvent(&state, newAuditEvent(ctx, action, "pricebook_entry", pricebookEntryID(entry.Provider, entry.Model), ""))
	if err := s.writeState(ctx, state); err != nil {
		return config.ModelPriceConfig{}, err
	}
	return entry, nil
}

func (s *RedisStore) DeletePricebookEntry(ctx context.Context, provider, model string) error {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if provider == "" || model == "" {
		return fmt.Errorf("pricebook provider and model are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return err
	}
	index := pricebookEntryIndex(state.Pricebook, provider, model)
	if index < 0 {
		return fmt.Errorf("pricebook entry %q not found", pricebookEntryID(provider, model))
	}
	appendAuditEvent(&state, newAuditEvent(ctx, "pricebook_entry.deleted", "pricebook_entry", pricebookEntryID(provider, model), ""))
	state.Pricebook = append(state.Pricebook[:index], state.Pricebook[index+1:]...)
	return s.writeState(ctx, state)
}

func (s *RedisStore) PruneAuditEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return 0, err
	}
	deleted := pruneAuditEvents(&state, maxAge, maxCount)
	if deleted == 0 {
		return 0, nil
	}
	if err := s.writeState(ctx, state); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (s *RedisStore) readState(ctx context.Context) (State, error) {
	raw, err := s.client.Get(ctx, s.key)
	if err != nil {
		if err == storage.ErrNil {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read control plane redis state: %w", err)
	}
	if len(raw) == 0 {
		return State{}, nil
	}

	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		return State{}, fmt.Errorf("decode control plane redis state: %w", err)
	}
	return cloneState(state), nil
}

func (s *RedisStore) writeState(ctx context.Context, state State) error {
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode control plane redis state: %w", err)
	}
	if err := s.client.Set(ctx, s.key, payload); err != nil {
		return fmt.Errorf("write control plane redis state: %w", err)
	}
	return nil
}

func cloneState(state State) State {
	out := State{
		Tenants:         make([]Tenant, 0, len(state.Tenants)),
		APIKeys:         make([]APIKey, 0, len(state.APIKeys)),
		Providers:       make([]Provider, 0, len(state.Providers)),
		ProviderSecrets: make([]ProviderSecret, 0, len(state.ProviderSecrets)),
		PolicyRules:     make([]config.PolicyRuleConfig, 0, len(state.PolicyRules)),
		Pricebook:       make([]config.ModelPriceConfig, 0, len(state.Pricebook)),
		Events:          make([]AuditEvent, 0, len(state.Events)),
	}
	for _, tenant := range state.Tenants {
		out.Tenants = append(out.Tenants, Tenant{
			ID:               tenant.ID,
			Name:             tenant.Name,
			Description:      tenant.Description,
			AllowedProviders: append([]string(nil), tenant.AllowedProviders...),
			AllowedModels:    append([]string(nil), tenant.AllowedModels...),
			Enabled:          tenant.Enabled,
		})
	}
	for _, key := range state.APIKeys {
		out.APIKeys = append(out.APIKeys, APIKey{
			ID:               key.ID,
			Name:             key.Name,
			Key:              key.Key,
			Tenant:           key.Tenant,
			Role:             key.Role,
			AllowedProviders: append([]string(nil), key.AllowedProviders...),
			AllowedModels:    append([]string(nil), key.AllowedModels...),
			Enabled:          key.Enabled,
			CreatedAt:        key.CreatedAt,
			UpdatedAt:        key.UpdatedAt,
		})
	}
	for _, provider := range state.Providers {
		out.Providers = append(out.Providers, Provider{
			ID:             provider.ID,
			Name:           provider.Name,
			PresetID:       provider.PresetID,
			Kind:           provider.Kind,
			Protocol:       provider.Protocol,
			BaseURL:        provider.BaseURL,
			APIVersion:     provider.APIVersion,
			DefaultModel:   provider.DefaultModel,
			ExplicitFields: append([]string(nil), provider.ExplicitFields...),
			Enabled:        provider.Enabled,
			CredentialID:   provider.CredentialID,
			CreatedAt:      provider.CreatedAt,
			UpdatedAt:      provider.UpdatedAt,
		})
	}
	for _, secret := range state.ProviderSecrets {
		out.ProviderSecrets = append(out.ProviderSecrets, ProviderSecret{
			ID:              secret.ID,
			ProviderID:      secret.ProviderID,
			APIKeyEncrypted: secret.APIKeyEncrypted,
			APIKeyPreview:   secret.APIKeyPreview,
			CreatedAt:       secret.CreatedAt,
			RotatedAt:       secret.RotatedAt,
		})
	}
	for _, rule := range state.PolicyRules {
		out.PolicyRules = append(out.PolicyRules, clonePolicyRule(rule))
	}
	for _, entry := range state.Pricebook {
		out.Pricebook = append(out.Pricebook, entry)
	}
	for _, event := range state.Events {
		out.Events = append(out.Events, AuditEvent{
			Timestamp:  event.Timestamp,
			Actor:      event.Actor,
			Action:     event.Action,
			TargetType: event.TargetType,
			TargetID:   event.TargetID,
			Detail:     event.Detail,
		})
	}
	return out
}

func actorFromContext(ctx context.Context) string {
	actor, _ := ctx.Value(actorContextKey{}).(string)
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "system"
	}
	return actor
}

func newAuditEvent(ctx context.Context, action, targetType, targetID, detail string) AuditEvent {
	return AuditEvent{
		Timestamp:  time.Now().UTC(),
		Actor:      actorFromContext(ctx),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Detail:     detail,
	}
}

func appendAuditEvent(state *State, event AuditEvent) {
	if state == nil {
		return
	}
	state.Events = append(state.Events, event)
	if len(state.Events) > maxAuditEvents {
		state.Events = append([]AuditEvent(nil), state.Events[len(state.Events)-maxAuditEvents:]...)
	}
}

func pruneAuditEvents(state *State, maxAge time.Duration, maxCount int) int {
	if state == nil {
		return 0
	}

	now := time.Now()
	deleted := 0
	kept := state.Events[:0]
	for _, event := range state.Events {
		if maxAge > 0 && !event.Timestamp.IsZero() && event.Timestamp.Before(now.Add(-maxAge)) {
			deleted++
			continue
		}
		kept = append(kept, event)
	}
	if maxCount > 0 && len(kept) > maxCount {
		deleted += len(kept) - maxCount
		kept = append([]AuditEvent(nil), kept[len(kept)-maxCount:]...)
	} else {
		kept = append([]AuditEvent(nil), kept...)
	}
	state.Events = kept
	return deleted
}

func canonicalID(id, name string) string {
	value := strings.TrimSpace(id)
	if value == "" {
		value = strings.TrimSpace(name)
	}
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func tenantExists(tenants []Tenant, id string) bool {
	for _, tenant := range tenants {
		if tenant.ID == id {
			return true
		}
	}
	return false
}

func tenantIndex(tenants []Tenant, id string) int {
	for i := range tenants {
		if tenants[i].ID == id {
			return i
		}
	}
	return -1
}

func apiKeyIndex(keys []APIKey, id string) int {
	for i := range keys {
		if keys[i].ID == id {
			return i
		}
	}
	return -1
}

func normalizePolicyRule(rule config.PolicyRuleConfig) (config.PolicyRuleConfig, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Action = strings.TrimSpace(rule.Action)
	rule.Reason = strings.TrimSpace(rule.Reason)
	rule.Roles = normalizeStringList(rule.Roles)
	rule.Tenants = normalizeStringList(rule.Tenants)
	rule.Providers = normalizeStringList(rule.Providers)
	rule.ProviderKinds = normalizeStringList(rule.ProviderKinds)
	rule.Models = normalizeStringList(rule.Models)
	rule.RouteReasons = normalizeStringList(rule.RouteReasons)
	rule.RewriteModelTo = strings.TrimSpace(rule.RewriteModelTo)
	if rule.ID == "" {
		return config.PolicyRuleConfig{}, fmt.Errorf("policy rule id is required")
	}
	if rule.Action == "" {
		return config.PolicyRuleConfig{}, fmt.Errorf("policy rule action is required")
	}
	return rule, nil
}

func normalizePricebookEntry(entry config.ModelPriceConfig) (config.ModelPriceConfig, error) {
	entry.Provider = strings.TrimSpace(entry.Provider)
	entry.Model = strings.TrimSpace(entry.Model)
	if entry.Provider == "" || entry.Model == "" {
		return config.ModelPriceConfig{}, fmt.Errorf("pricebook provider and model are required")
	}
	if entry.InputMicrosUSDPerMillionTokens < 0 || entry.OutputMicrosUSDPerMillionTokens < 0 || entry.CachedInputMicrosUSDPerMillionTokens < 0 {
		return config.ModelPriceConfig{}, fmt.Errorf("pricebook values must be zero or greater")
	}
	switch strings.TrimSpace(entry.Source) {
	case "":
		// Empty == manual for backward compatibility. Every pre-Source-field
		// row was put there by a human, so default to that.
		entry.Source = config.PricebookSourceManual
	case config.PricebookSourceManual, config.PricebookSourceImported:
		// ok
	default:
		return config.ModelPriceConfig{}, fmt.Errorf("pricebook source must be %q or %q (got %q)",
			config.PricebookSourceManual, config.PricebookSourceImported, entry.Source)
	}
	return entry, nil
}

func upsertPolicyRule(state *State, rule config.PolicyRuleConfig) string {
	index := policyRuleIndex(state.PolicyRules, rule.ID)
	if index >= 0 {
		state.PolicyRules[index] = clonePolicyRule(rule)
		return "policy_rule.updated"
	}
	state.PolicyRules = append(state.PolicyRules, clonePolicyRule(rule))
	return "policy_rule.created"
}

func upsertPricebookEntry(state *State, entry config.ModelPriceConfig) string {
	index := pricebookEntryIndex(state.Pricebook, entry.Provider, entry.Model)
	if index >= 0 {
		state.Pricebook[index] = entry
		return "pricebook_entry.updated"
	}
	state.Pricebook = append(state.Pricebook, entry)
	return "pricebook_entry.created"
}

func policyRuleIndex(items []config.PolicyRuleConfig, id string) int {
	for i := range items {
		if items[i].ID == id {
			return i
		}
	}
	return -1
}

func pricebookEntryIndex(items []config.ModelPriceConfig, provider, model string) int {
	for i := range items {
		if items[i].Provider == provider && items[i].Model == model {
			return i
		}
	}
	return -1
}

func pricebookEntryID(provider, model string) string {
	return strings.TrimSpace(provider) + "/" + strings.TrimSpace(model)
}

func normalizeStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func clonePolicyRule(rule config.PolicyRuleConfig) config.PolicyRuleConfig {
	rule.Roles = append([]string(nil), rule.Roles...)
	rule.Tenants = append([]string(nil), rule.Tenants...)
	rule.Providers = append([]string(nil), rule.Providers...)
	rule.ProviderKinds = append([]string(nil), rule.ProviderKinds...)
	rule.Models = append([]string(nil), rule.Models...)
	rule.RouteReasons = append([]string(nil), rule.RouteReasons...)
	return rule
}

func tenantReferencedByAPIKeys(keys []APIKey, tenantID string) bool {
	for _, key := range keys {
		if key.Tenant == tenantID {
			return true
		}
	}
	return false
}
