package controlplane

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
)

type Tenant struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	AllowedProviders []string `json:"allowed_providers,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
	Enabled          bool     `json:"enabled"`
	// SystemPrompt is the tenant-level layer for agent_loop tasks. It
	// stacks between the global default and per-task / workspace
	// layers in the composed system prompt. Tenant admins set this
	// via the admin UI to shape their agents' behavior (e.g. "You
	// operate inside a financial-services context — never run code
	// that touches production data without --dry-run.").
	//
	// Empty = no tenant-level addition; the global + task + workspace
	// layers still apply.
	SystemPrompt string `json:"system_prompt,omitempty"`
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

type actorContextKey struct{}

const maxAuditEvents = 100

func WithActor(ctx context.Context, actor string) context.Context {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return ctx
	}
	return context.WithValue(ctx, actorContextKey{}, actor)
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
			SystemPrompt:     tenant.SystemPrompt,
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
