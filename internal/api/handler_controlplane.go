package api

import (
	"net/http"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
)

func (h *Handler) HandleControlPlaneStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, errCodeUnauthorized, "missing or invalid bearer token")
		return
	}

	payload := ControlPlaneResponse{
		Object: "control_plane",
		Data: ControlPlaneResponseItem{
			Backend:     "env",
			Tenants:     []ControlPlaneTenantItem{},
			APIKeys:     []ControlPlaneAPIKeyRecord{},
			Providers:   []ControlPlaneProviderRecord{},
			PolicyRules: []ControlPlanePolicyRuleRecord{},
			Pricebook:   []ControlPlanePricebookRecord{},
			Events:      []ControlPlaneAuditEventRecord{},
		},
	}

	if h.controlPlane == nil {
		WriteJSON(w, http.StatusOK, payload)
		return
	}

	state, err := h.controlPlane.Snapshot(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	payload.Data.Backend = h.controlPlane.Backend()
	for _, tenant := range state.Tenants {
		payload.Data.Tenants = append(payload.Data.Tenants, ControlPlaneTenantItem{
			ID:               tenant.ID,
			Name:             tenant.Name,
			Description:      tenant.Description,
			AllowedProviders: tenant.AllowedProviders,
			AllowedModels:    tenant.AllowedModels,
			Enabled:          tenant.Enabled,
		})
	}
	for _, key := range state.APIKeys {
		payload.Data.APIKeys = append(payload.Data.APIKeys, renderControlPlaneAPIKey(key))
	}
	for _, record := range buildControlPlaneProviderList(h.config, state) {
		payload.Data.Providers = append(payload.Data.Providers, record)
	}
	for _, rule := range state.PolicyRules {
		payload.Data.PolicyRules = append(payload.Data.PolicyRules, renderControlPlanePolicyRule(rule))
	}
	for _, entry := range state.Pricebook {
		payload.Data.Pricebook = append(payload.Data.Pricebook, renderControlPlanePricebookEntry(entry))
	}
	for _, event := range state.Events {
		payload.Data.Events = append(payload.Data.Events, renderControlPlaneAuditEvent(event))
	}

	WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) HandleControlPlaneUpsertTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneTenantUpsertRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	tenant, err := h.controlPlane.UpsertTenant(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), controlplane.Tenant{
		ID:               req.ID,
		Name:             req.Name,
		Description:      req.Description,
		AllowedProviders: req.AllowedProviders,
		AllowedModels:    req.AllowedModels,
		Enabled:          req.Enabled,
	})
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_tenant",
		"data": ControlPlaneTenantItem{
			ID:               tenant.ID,
			Name:             tenant.Name,
			Description:      tenant.Description,
			AllowedProviders: tenant.AllowedProviders,
			AllowedModels:    tenant.AllowedModels,
			Enabled:          tenant.Enabled,
		},
	})
}

func (h *Handler) HandleControlPlaneUpsertAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneAPIKeyUpsertRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	key, err := h.controlPlane.UpsertAPIKey(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), controlplane.APIKey{
		ID:               req.ID,
		Name:             req.Name,
		Key:              req.Key,
		Tenant:           req.Tenant,
		Role:             req.Role,
		AllowedProviders: req.AllowedProviders,
		AllowedModels:    req.AllowedModels,
		Enabled:          req.Enabled,
	})
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key",
		"data":   renderControlPlaneAPIKey(key),
	})
}

func (h *Handler) HandleControlPlaneSetTenantEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneTenantLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	tenant, err := h.controlPlane.SetTenantEnabled(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Enabled)
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_tenant",
		"data": ControlPlaneTenantItem{
			ID:               tenant.ID,
			Name:             tenant.Name,
			Description:      tenant.Description,
			AllowedProviders: tenant.AllowedProviders,
			AllowedModels:    tenant.AllowedModels,
			Enabled:          tenant.Enabled,
		},
	})
}

func (h *Handler) HandleControlPlaneDeleteTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneTenantLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.controlPlane.DeleteTenant(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID); err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_tenant_deleted",
		"data": map[string]string{
			"id": req.ID,
		},
	})
}

func (h *Handler) HandleControlPlaneSetAPIKeyEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneAPIKeyLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	key, err := h.controlPlane.SetAPIKeyEnabled(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Enabled)
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key",
		"data":   renderControlPlaneAPIKey(key),
	})
}

func (h *Handler) HandleControlPlaneRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneAPIKeyLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	key, err := h.controlPlane.RotateAPIKey(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Key)
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key",
		"data":   renderControlPlaneAPIKey(key),
	})
}

func (h *Handler) HandleControlPlaneDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlaneAPIKeyLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.controlPlane.DeleteAPIKey(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID); err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key_deleted",
		"data": map[string]string{
			"id": req.ID,
		},
	})
}

func (h *Handler) HandleControlPlaneSetProviderEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}
	if h.providerRuntime == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "dynamic provider runtime is not configured")
		return
	}

	id := r.PathValue("id")
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	provider, err := h.providerRuntime.SetEnabled(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), id, req.Enabled)
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	state, _ := h.controlPlane.Snapshot(r.Context())
	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_provider",
		"data":   renderControlPlaneProvider(provider, state.ProviderSecrets),
	})
}

// HandleControlPlaneSetProviderAPIKey is the single endpoint for managing a provider's
// API key. PUT with a non-empty `key` sets/updates it; PUT with an empty `key` clears it.
func (h *Handler) HandleControlPlaneSetProviderAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}
	if h.providerRuntime == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "dynamic provider runtime is not configured")
		return
	}

	id := r.PathValue("id")
	var req struct {
		Key string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	ctx := controlplane.WithActor(r.Context(), controlPlaneActor(principal, r))
	if req.Key == "" {
		if err := h.providerRuntime.DeleteCredential(ctx, id); err != nil {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"object": "control_plane_provider_api_key",
			"data":   map[string]string{"id": id, "status": "cleared"},
		})
		return
	}

	provider, err := h.providerRuntime.RotateSecret(ctx, id, req.Key)
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}
	state, _ := h.controlPlane.Snapshot(r.Context())
	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_provider_api_key",
		"data":   renderControlPlaneProvider(provider, state.ProviderSecrets),
	})
}

func (h *Handler) HandleControlPlaneUpsertPolicyRule(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlanePolicyRuleUpsertRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	rule, err := h.controlPlane.UpsertPolicyRule(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), config.PolicyRuleConfig{
		ID:                     req.ID,
		Action:                 req.Action,
		Reason:                 req.Reason,
		Roles:                  req.Roles,
		Tenants:                req.Tenants,
		Providers:              req.Providers,
		ProviderKinds:          req.ProviderKinds,
		Models:                 req.Models,
		RouteReasons:           req.RouteReasons,
		MinPromptTokens:        req.MinPromptTokens,
		MinEstimatedCostMicros: req.MinEstimatedCostMicros,
		RewriteModelTo:         req.RewriteModelTo,
	})
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_policy_rule",
		"data":   renderControlPlanePolicyRule(rule),
	})
}

func (h *Handler) HandleControlPlaneDeletePolicyRule(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlanePolicyRuleLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.controlPlane.DeletePolicyRule(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID); err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_policy_rule_deleted",
		"data": map[string]string{
			"id": req.ID,
		},
	})
}

func (h *Handler) HandleControlPlaneUpsertPricebookEntry(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlanePricebookUpsertRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	entry, err := h.controlPlane.UpsertPricebookEntry(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), config.ModelPriceConfig{
		Provider:                             req.Provider,
		Model:                                req.Model,
		InputMicrosUSDPerMillionTokens:       req.InputMicrosUSDPerMillionTokens,
		OutputMicrosUSDPerMillionTokens:      req.OutputMicrosUSDPerMillionTokens,
		CachedInputMicrosUSDPerMillionTokens: req.CachedInputMicrosUSDPerMillionTokens,
		Source:                               req.Source,
	})
	if err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_pricebook_entry",
		"data":   renderControlPlanePricebookEntry(entry),
	})
}

func (h *Handler) HandleControlPlaneDeletePricebookEntry(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}

	var req ControlPlanePricebookLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.controlPlane.DeletePricebookEntry(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.Provider, req.Model); err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_pricebook_entry_deleted",
		"data": map[string]string{
			"provider": req.Provider,
			"model":    req.Model,
		},
	})
}

func renderControlPlaneAPIKey(key controlplane.APIKey) ControlPlaneAPIKeyRecord {
	record := ControlPlaneAPIKeyRecord{
		ID:               key.ID,
		Name:             key.Name,
		Tenant:           key.Tenant,
		Role:             key.Role,
		AllowedProviders: key.AllowedProviders,
		AllowedModels:    key.AllowedModels,
		Enabled:          key.Enabled,
		KeyPreview:       previewSecret(key.Key),
	}
	if !key.CreatedAt.IsZero() {
		record.CreatedAt = key.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !key.UpdatedAt.IsZero() {
		record.UpdatedAt = key.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return record
}

func renderControlPlanePolicyRule(rule config.PolicyRuleConfig) ControlPlanePolicyRuleRecord {
	return ControlPlanePolicyRuleRecord{
		ID:                     rule.ID,
		Action:                 rule.Action,
		Reason:                 rule.Reason,
		Roles:                  rule.Roles,
		Tenants:                rule.Tenants,
		Providers:              rule.Providers,
		ProviderKinds:          rule.ProviderKinds,
		Models:                 rule.Models,
		RouteReasons:           rule.RouteReasons,
		MinPromptTokens:        rule.MinPromptTokens,
		MinEstimatedCostMicros: rule.MinEstimatedCostMicros,
		RewriteModelTo:         rule.RewriteModelTo,
	}
}

func renderControlPlanePricebookEntry(entry config.ModelPriceConfig) ControlPlanePricebookRecord {
	return ControlPlanePricebookRecord{
		Provider:                             entry.Provider,
		Model:                                entry.Model,
		InputMicrosUSDPerMillionTokens:       entry.InputMicrosUSDPerMillionTokens,
		OutputMicrosUSDPerMillionTokens:      entry.OutputMicrosUSDPerMillionTokens,
		CachedInputMicrosUSDPerMillionTokens: entry.CachedInputMicrosUSDPerMillionTokens,
		Source:                               entry.Source,
	}
}

func renderControlPlaneAuditEvent(event controlplane.AuditEvent) ControlPlaneAuditEventRecord {
	record := ControlPlaneAuditEventRecord{
		Actor:      event.Actor,
		Action:     event.Action,
		TargetType: event.TargetType,
		TargetID:   event.TargetID,
		Detail:     event.Detail,
	}
	if !event.Timestamp.IsZero() {
		record.Timestamp = event.Timestamp.UTC().Format(time.RFC3339)
	}
	return record
}

// buildControlPlaneProviderList returns one record per built-in provider. The set is
// fixed — providers cannot be added or removed. The CP store contributes overrides for
// `Enabled` and credential information; everything else is sourced from the preset.
// When two built-ins share a base URL, only one is reported as enabled.
func buildControlPlaneProviderList(cfg config.Config, state controlplane.State) []ControlPlaneProviderRecord {
	envKeyByID := make(map[string]bool)
	for _, pc := range cfg.Providers.OpenAICompatible {
		if pc.APIKey != "" {
			envKeyByID[pc.Name] = true
		}
	}
	cpByID := make(map[string]controlplane.Provider, len(state.Providers))
	for _, p := range state.Providers {
		cpByID[p.ID] = p
	}

	builtIns := config.BuiltInProviders()

	// Pre-compute the set of base URLs that are shared by 2+ built-ins. A
	// provider in a conflict group with no CP record stays disabled by
	// default — the operator must opt one in. Outside conflict groups,
	// the legacy "default enabled" behavior holds (cloud presets light up
	// once the operator pastes a key).
	conflictURLs := make(map[string]int)
	for _, b := range builtIns {
		if b.BaseURL == "" {
			continue
		}
		conflictURLs[b.BaseURL]++
	}

	records := make([]ControlPlaneProviderRecord, 0, len(builtIns))
	for _, builtIn := range builtIns {
		// Default Enabled depends on conflict state: shared base URLs default
		// to off (operator opt-in), unique base URLs default to on (legacy).
		defaultEnabled := conflictURLs[builtIn.BaseURL] < 2
		record := ControlPlaneProviderRecord{
			ID:           builtIn.ID,
			Name:         builtIn.ID,
			PresetID:     builtIn.ID,
			Kind:         builtIn.Kind,
			Protocol:     builtIn.Protocol,
			BaseURL:      builtIn.BaseURL,
			APIVersion:   builtIn.APIVersion,
			DefaultModel: builtIn.DefaultModel,
			Enabled:      defaultEnabled,
		}
		if cp, ok := cpByID[builtIn.ID]; ok {
			record.Enabled = cp.Enabled
			if !cp.CreatedAt.IsZero() {
				record.CreatedAt = cp.CreatedAt.UTC().Format(time.RFC3339)
			}
			if !cp.UpdatedAt.IsZero() {
				record.UpdatedAt = cp.UpdatedAt.UTC().Format(time.RFC3339)
			}
			for _, secret := range state.ProviderSecrets {
				if secret.ProviderID == builtIn.ID {
					record.CredentialConfigured = secret.APIKeyEncrypted != ""
					record.CredentialSource = "vault"
					record.CredentialPreview = secret.APIKeyPreview
					break
				}
			}
		}
		if !record.CredentialConfigured && envKeyByID[builtIn.ID] {
			record.CredentialConfigured = true
			record.CredentialSource = "env"
		}
		records = append(records, record)
	}

	resolveDefaultProviderConflicts(records)
	return records
}

// resolveDefaultProviderConflicts mutates records in place: when a base URL is shared
// between providers, at most one is left enabled. If no record in the group is
// explicitly enabled, the entire group stays disabled — the operator must opt one in
// rather than having the gateway pick a winner on their behalf. The previous behavior
// (auto-enable the alphabetically-first provider in an unconfigured conflict group)
// produced a confusing UX: toggling the auto-picked winner off "promoted" the next
// peer to on, which read as the system enabling something the operator didn't ask for.
//
// Built-ins are already iterated in alphabetical order by `BuiltInProviders()`, so
// the input slice is sorted; that determines the winner when multiple records are
// explicitly enabled (a degraded state the backend also defends against).
func resolveDefaultProviderConflicts(records []ControlPlaneProviderRecord) {
	groupByURL := make(map[string][]int)
	for i, r := range records {
		if r.BaseURL == "" {
			continue
		}
		groupByURL[r.BaseURL] = append(groupByURL[r.BaseURL], i)
	}
	for _, group := range groupByURL {
		if len(group) < 2 {
			continue
		}
		// Alphabetically-first enabled record wins (records are already sorted by ID).
		// If nothing is enabled, the whole group stays off — operator must opt in.
		winner := -1
		for _, idx := range group {
			if records[idx].Enabled {
				winner = idx
				break
			}
		}
		if winner < 0 {
			continue
		}
		for _, idx := range group {
			if idx != winner {
				records[idx].Enabled = false
			}
		}
	}
}

func renderControlPlaneProvider(provider controlplane.Provider, secrets []controlplane.ProviderSecret) ControlPlaneProviderRecord {
	inheritedFields := controlPlaneInheritedFields(provider)
	record := ControlPlaneProviderRecord{
		ID:              provider.ID,
		Name:            provider.Name,
		PresetID:        provider.PresetID,
		Kind:            provider.Kind,
		Protocol:        provider.Protocol,
		BaseURL:         provider.BaseURL,
		APIVersion:      provider.APIVersion,
		DefaultModel:    provider.DefaultModel,
		ExplicitFields:  append([]string(nil), provider.ExplicitFields...),
		InheritedFields: inheritedFields,
		Enabled:         provider.Enabled,
	}
	for _, secret := range secrets {
		if secret.ProviderID == provider.ID {
			record.CredentialConfigured = secret.APIKeyEncrypted != ""
			record.CredentialSource = "vault"
			record.CredentialPreview = secret.APIKeyPreview
			break
		}
	}
	if !provider.CreatedAt.IsZero() {
		record.CreatedAt = provider.CreatedAt.UTC().Format(time.RFC3339)
	}
	if !provider.UpdatedAt.IsZero() {
		record.UpdatedAt = provider.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return record
}

func controlPlaneInheritedFields(provider controlplane.Provider) []string {
	builtIn, ok := config.BuiltInProviderByID(firstNonEmpty(provider.PresetID, provider.Name, provider.ID))
	if !ok {
		return nil
	}

	explicit := make(map[string]struct{}, len(provider.ExplicitFields))
	for _, field := range provider.ExplicitFields {
		explicit[field] = struct{}{}
	}

	var inherited []string
	maybeAppend := func(field string, condition bool) {
		if !condition {
			return
		}
		if _, ok := explicit[field]; ok {
			return
		}
		inherited = append(inherited, field)
	}

	maybeAppend("kind", provider.Kind == builtIn.Kind)
	maybeAppend("protocol", provider.Protocol == builtIn.Protocol)
	maybeAppend("base_url", provider.BaseURL == builtIn.BaseURL)
	maybeAppend("api_version", provider.APIVersion == builtIn.APIVersion)
	maybeAppend("default_model", provider.DefaultModel == builtIn.DefaultModel)
	return inherited
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func previewSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 2 {
		return secret
	}
	if len(secret) <= 8 {
		return secret[:2] + "..." + secret[len(secret)-2:]
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}
