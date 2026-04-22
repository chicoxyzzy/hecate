package api

import (
	"net/http"
	"slices"
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
			Backend:   "env",
			Tenants:   []ControlPlaneTenantItem{},
			APIKeys:   []ControlPlaneAPIKeyRecord{},
			Providers: []ControlPlaneProviderRecord{},
			Events:    []ControlPlaneAuditEventRecord{},
		},
	}

	for _, key := range h.config.Server.APIKeys {
		payload.Data.APIKeys = append(payload.Data.APIKeys, ControlPlaneAPIKeyRecord{
			ID:               key.Name,
			Name:             key.Name,
			Tenant:           key.Tenant,
			Role:             key.Role,
			AllowedProviders: key.AllowedProviders,
			AllowedModels:    key.AllowedModels,
			Enabled:          true,
			KeyPreview:       previewSecret(key.Key),
		})
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
	if fileStore, ok := h.controlPlane.(*controlplane.FileStore); ok {
		payload.Data.Path = fileStore.Path()
	}
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
	for _, provider := range state.Providers {
		payload.Data.Providers = append(payload.Data.Providers, renderControlPlaneProvider(provider, state.ProviderSecrets))
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

func (h *Handler) HandleControlPlaneUpsertProvider(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}
	if h.providerRuntime == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "dynamic provider runtime is not configured")
		return
	}

	var req ControlPlaneProviderUpsertRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	providerInput := controlplane.Provider{
		ID:       req.ID,
		Name:     req.Name,
		PresetID: req.PresetID,
		Enabled:  req.Enabled,
	}
	if req.Kind != nil {
		providerInput.Kind = *req.Kind
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "kind")
	}
	if req.Protocol != nil {
		providerInput.Protocol = *req.Protocol
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "protocol")
	}
	if req.BaseURL != nil {
		providerInput.BaseURL = *req.BaseURL
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "base_url")
	}
	if req.APIVersion != nil {
		providerInput.APIVersion = *req.APIVersion
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "api_version")
	}
	if req.DefaultModel != nil {
		providerInput.DefaultModel = *req.DefaultModel
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "default_model")
	}
	if req.Models != nil {
		providerInput.Models = req.Models
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "models")
	}
	if req.AllowAnyModel != nil {
		providerInput.AllowAnyModel = *req.AllowAnyModel
		providerInput.ExplicitFields = append(providerInput.ExplicitFields, "allow_any_model")
	}

	provider, err := h.providerRuntime.Upsert(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), providerInput, req.Key)
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

func (h *Handler) HandleControlPlaneSetProviderEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}
	if h.providerRuntime == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "dynamic provider runtime is not configured")
		return
	}

	var req ControlPlaneProviderLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	provider, err := h.providerRuntime.SetEnabled(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Enabled)
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

func (h *Handler) HandleControlPlaneRotateProviderSecret(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}
	if h.providerRuntime == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "dynamic provider runtime is not configured")
		return
	}

	var req ControlPlaneProviderLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	provider, err := h.providerRuntime.RotateSecret(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Key)
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

func (h *Handler) HandleControlPlaneDeleteProvider(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireControlPlane(w, r)
	if !ok {
		return
	}
	if h.providerRuntime == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "dynamic provider runtime is not configured")
		return
	}

	var req ControlPlaneProviderLifecycleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if err := h.providerRuntime.Delete(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID); err != nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_provider_deleted",
		"data": map[string]string{
			"id": req.ID,
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
		Models:          provider.Models,
		AllowAnyModel:   provider.AllowAnyModel,
		ExplicitFields:  append([]string(nil), provider.ExplicitFields...),
		InheritedFields: inheritedFields,
		Enabled:         provider.Enabled,
	}
	for _, secret := range secrets {
		if secret.ProviderID == provider.ID {
			record.CredentialConfigured = secret.APIKeyEncrypted != ""
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
	maybeAppend("allow_any_model", provider.AllowAnyModel == builtIn.AllowAnyModel)
	maybeAppend("models", slices.Equal(provider.Models, builtIn.ExampleModels))
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
