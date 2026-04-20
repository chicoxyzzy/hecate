package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/providers"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Handler struct {
	config        config.Config
	logger        *slog.Logger
	service       *gateway.Service
	authenticator *auth.Authenticator
	controlPlane  controlplane.Store
}

func NewHandler(cfg config.Config, logger *slog.Logger, service *gateway.Service, cpStore controlplane.Store) *Handler {
	return &Handler{
		config:        cfg,
		logger:        logger,
		service:       service,
		authenticator: auth.NewAuthenticator(cfg.Server, cpStore),
		controlPlane:  cpStore,
	}
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *Handler) HandleSession(w http.ResponseWriter, r *http.Request) {
	introspection := h.authenticator.Introspect(r)
	WriteJSON(w, http.StatusOK, SessionResponse{
		Object: "session",
		Data: SessionResponseItem{
			Authenticated:    introspection.Authenticated,
			InvalidToken:     introspection.InvalidToken,
			Role:             introspection.Principal.Role,
			Name:             introspection.Principal.Name,
			Tenant:           introspection.Principal.Tenant,
			Source:           introspection.Principal.Source,
			KeyID:            introspection.Principal.KeyID,
			AllowedProviders: introspection.Principal.AllowedProviders,
			AllowedModels:    introspection.Principal.AllowedModels,
		},
	})
}

func (h *Handler) HandleModels(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAny(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	result, err := h.service.ListModels(r.Context())
	if err != nil {
		h.logger.Error("list models failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	data := make([]OpenAIModelData, 0, len(result.Models))
	for _, model := range result.Models {
		if !principal.IsAdmin() && !modelAllowedForPrincipal(principal, model.Provider, model.ID) {
			continue
		}
		data = append(data, OpenAIModelData{
			ID:      model.ID,
			Object:  "model",
			OwnedBy: model.OwnedBy,
			Metadata: map[string]any{
				"provider":         model.Provider,
				"provider_kind":    model.Kind,
				"default":          model.Default,
				"discovery_source": model.DiscoverySource,
			},
		})
	}

	WriteJSON(w, http.StatusOK, OpenAIModelsResponse{
		Object: "list",
		Data:   data,
	})
}

func (h *Handler) HandleProviderStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	result, err := h.service.ProviderStatus(r.Context())
	if err != nil {
		h.logger.Error("provider status failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	data := make([]ProviderStatusResponseItem, 0, len(result.Providers))
	for _, provider := range result.Providers {
		item := ProviderStatusResponseItem{
			Name:            provider.Name,
			Kind:            provider.Kind,
			Healthy:         provider.Healthy,
			Status:          provider.Status,
			DefaultModel:    provider.DefaultModel,
			Models:          provider.Models,
			DiscoverySource: provider.DiscoverySource,
			Error:           provider.Error,
		}
		if !provider.RefreshedAt.IsZero() {
			item.RefreshedAt = provider.RefreshedAt.UTC().Format(time.RFC3339)
		}
		data = append(data, item)
	}

	WriteJSON(w, http.StatusOK, ProviderStatusResponse{
		Object: "provider_status",
		Data:   data,
	})
}

func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	snapshot, health, err := h.service.MetricsSnapshot(r.Context())
	if err != nil {
		h.logger.Error("metrics failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(telemetry.RenderPrometheus(snapshot, health)))
}

func (h *Handler) HandleBudgetStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	result, err := h.service.BudgetStatusWithFilter(r.Context(), budgetFilterFromRequest(r))
	if err != nil {
		h.logger.Error("budget status failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleBudgetReset(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	var resetReq BudgetResetRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&resetReq); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
			return
		}
	}

	filter := budgetFilterFromRequest(r)
	if resetReq.Key != "" {
		filter.Key = resetReq.Key
	}
	if resetReq.Scope != "" {
		filter.Scope = resetReq.Scope
	}
	if resetReq.Provider != "" {
		filter.Provider = resetReq.Provider
	}
	if resetReq.Tenant != "" {
		filter.Tenant = resetReq.Tenant
	}

	result, err := h.service.ResetBudgetWithFilter(r.Context(), filter)
	if err != nil {
		h.logger.Error("budget reset failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleBudgetTopUp(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	var topUpReq BudgetTopUpRequest
	if err := json.NewDecoder(r.Body).Decode(&topUpReq); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	if topUpReq.AmountMicrosUSD <= 0 {
		WriteError(w, http.StatusBadRequest, "invalid_request", "amount_micros_usd must be greater than zero")
		return
	}

	filter := budgetFilterFromMutation(topUpReq.Key, topUpReq.Scope, topUpReq.Provider, topUpReq.Tenant)
	result, err := h.service.TopUpBudgetWithFilter(r.Context(), filter, topUpReq.AmountMicrosUSD)
	if err != nil {
		h.logger.Error("budget top up failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleBudgetSetLimit(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	var limitReq BudgetLimitRequest
	if err := json.NewDecoder(r.Body).Decode(&limitReq); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	if limitReq.LimitMicrosUSD < 0 {
		WriteError(w, http.StatusBadRequest, "invalid_request", "limit_micros_usd must be zero or greater")
		return
	}

	filter := budgetFilterFromMutation(limitReq.Key, limitReq.Scope, limitReq.Provider, limitReq.Tenant)
	result, err := h.service.SetBudgetLimitWithFilter(r.Context(), filter, limitReq.LimitMicrosUSD)
	if err != nil {
		h.logger.Error("budget set limit failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleControlPlaneStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizeAdmin(r); !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	payload := ControlPlaneResponse{
		Object: "control_plane",
		Data: ControlPlaneResponseItem{
			Backend: "env",
			Tenants: []ControlPlaneTenantItem{},
			APIKeys: []ControlPlaneAPIKeyRecord{},
			Events:  []ControlPlaneAuditEventRecord{},
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
		WriteError(w, http.StatusInternalServerError, "gateway_error", err.Error())
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
	for _, event := range state.Events {
		payload.Data.Events = append(payload.Data.Events, renderControlPlaneAuditEvent(event))
	}

	WriteJSON(w, http.StatusOK, payload)
}

func (h *Handler) HandleControlPlaneUpsertTenant(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneTenantUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
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
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneAPIKeyUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
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
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key",
		"data":   renderControlPlaneAPIKey(key),
	})
}

func (h *Handler) HandleControlPlaneSetTenantEnabled(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneTenantLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	tenant, err := h.controlPlane.SetTenantEnabled(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Enabled)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneTenantLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	if err := h.controlPlane.DeleteTenant(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
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
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneAPIKeyLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	key, err := h.controlPlane.SetAPIKeyEnabled(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Enabled)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key",
		"data":   renderControlPlaneAPIKey(key),
	})
}

func (h *Handler) HandleControlPlaneRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneAPIKeyLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	key, err := h.controlPlane.RotateAPIKey(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID, req.Key)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key",
		"data":   renderControlPlaneAPIKey(key),
	})
}

func (h *Handler) HandleControlPlaneDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "control plane backend is not configured")
		return
	}

	var req ControlPlaneAPIKeyLifecycleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	if err := h.controlPlane.DeleteAPIKey(controlplane.WithActor(r.Context(), controlPlaneActor(principal, r)), req.ID); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"object": "control_plane_api_key_deleted",
		"data": map[string]string{
			"id": req.ID,
		},
	})
}

func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.authorizeAny(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}

	var wireReq OpenAIChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&wireReq); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	internalReq, err := normalizeChatRequest(wireReq, RequestIDFromContext(r.Context()), principal)
	if err != nil {
		WriteError(w, http.StatusForbidden, "forbidden", err.Error())
		return
	}
	result, err := h.service.HandleChat(r.Context(), internalReq)
	if err != nil {
		h.logger.Error("chat completion failed",
			slog.String("request_id", RequestIDFromContext(r.Context())),
			slog.Any("error", err),
		)

		statusCode := http.StatusInternalServerError
		if gateway.IsClientError(err) {
			statusCode = http.StatusBadRequest
		}
		if gateway.IsDeniedError(err) {
			statusCode = http.StatusForbidden
		}
		var upstreamErr *providers.UpstreamError
		if errors.As(err, &upstreamErr) {
			statusCode = mapUpstreamStatus(upstreamErr.StatusCode)
			WriteError(w, statusCode, "upstream_error", upstreamErr.Message)
			return
		}

		WriteError(w, statusCode, "gateway_error", err.Error())
		return
	}

	wireResp := renderChatCompletionResponse(result.Response)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Runtime-Provider", result.Metadata.Provider)
	w.Header().Set("X-Runtime-Provider-Kind", result.Metadata.ProviderKind)
	w.Header().Set("X-Runtime-Route-Reason", result.Metadata.RouteReason)
	w.Header().Set("X-Runtime-Requested-Model", result.Metadata.RequestedModel)
	w.Header().Set("X-Runtime-Requested-Model-Canonical", result.Metadata.CanonicalRequestedModel)
	w.Header().Set("X-Runtime-Model", result.Metadata.Model)
	w.Header().Set("X-Runtime-Model-Canonical", result.Metadata.CanonicalResolvedModel)
	w.Header().Set("X-Runtime-Cache", strconv.FormatBool(result.Metadata.CacheHit))
	w.Header().Set("X-Runtime-Cost-USD", formatUSD(result.Metadata.CostMicrosUSD))
	WriteJSON(w, http.StatusOK, wireResp)
}

func (h *Handler) authorizeAny(r *http.Request) (auth.Principal, bool) {
	return h.authenticator.Authenticate(r)
}

func (h *Handler) authorizeAdmin(r *http.Request) (auth.Principal, bool) {
	if h.authenticator == nil || !h.authenticator.Enabled() {
		return auth.Principal{Role: "admin"}, true
	}
	principal, ok := h.authorizeAny(r)
	if !ok || !principal.IsAdmin() {
		return auth.Principal{}, false
	}
	return principal, true
}

func normalizeChatRequest(req OpenAIChatCompletionRequest, requestID string, principal auth.Principal) (types.ChatRequest, error) {
	messages := make([]types.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		messages = append(messages, types.Message{
			Role:    msg.Role,
			Content: msg.Content,
			Name:    msg.Name,
		})
	}

	tenant := req.User
	if principal.Tenant != "" {
		if req.User != "" && req.User != principal.Tenant {
			return types.ChatRequest{}, fmt.Errorf("api key is bound to tenant %q and cannot act as %q", principal.Tenant, req.User)
		}
		tenant = principal.Tenant
	}

	metadata := map[string]string{
		"user":     tenant,
		"provider": req.Provider,
	}
	if tenant != "" {
		metadata["tenant"] = tenant
	}
	if len(principal.AllowedProviders) > 0 {
		metadata["auth_allowed_providers"] = strings.Join(principal.AllowedProviders, ",")
	}
	if len(principal.AllowedModels) > 0 {
		metadata["auth_allowed_models"] = strings.Join(principal.AllowedModels, ",")
	}
	if principal.Role != "" {
		metadata["auth_role"] = principal.Role
	}

	return types.ChatRequest{
		RequestID:   requestID,
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Metadata:    metadata,
	}, nil
}

func modelAllowedForPrincipal(principal auth.Principal, provider, model string) bool {
	if principal.IsAdmin() {
		return true
	}
	if len(principal.AllowedProviders) > 0 {
		if !contains(principal.AllowedProviders, provider) {
			return false
		}
	}
	if len(principal.AllowedModels) > 0 {
		if !contains(principal.AllowedModels, model) {
			return false
		}
	}
	return true
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
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

func controlPlaneActor(principal auth.Principal, r *http.Request) string {
	actor := strings.TrimSpace(principal.Name)
	if actor == "" {
		actor = principal.Role
	}
	if actor == "" {
		actor = "admin"
	}
	requestID := strings.TrimSpace(RequestIDFromContext(r.Context()))
	if requestID == "" {
		return actor
	}
	return actor + ":" + requestID
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

func renderChatCompletionResponse(resp *types.ChatResponse) OpenAIChatCompletionResponse {
	choices := make([]OpenAIChatCompletionChoice, 0, len(resp.Choices))
	for _, choice := range resp.Choices {
		choices = append(choices, OpenAIChatCompletionChoice{
			Index: choice.Index,
			Message: OpenAIChatMessage{
				Role:    choice.Message.Role,
				Content: choice.Message.Content,
				Name:    choice.Message.Name,
			},
			FinishReason: choice.FinishReason,
		})
	}

	return OpenAIChatCompletionResponse{
		ID:      resp.ID,
		Object:  "chat.completion",
		Created: resp.CreatedAt.Unix(),
		Model:   resp.Model,
		Choices: choices,
		Usage: OpenAIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}
}

func renderBudgetStatusResponse(result *gateway.BudgetStatusResult) BudgetStatusResponse {
	status := result.Status
	return BudgetStatusResponse{
		Object: "budget_status",
		Data: BudgetStatusResponseItem{
			Key:                status.Key,
			Scope:              status.Scope,
			Provider:           status.Provider,
			Tenant:             status.Tenant,
			Backend:            status.Backend,
			LimitSource:        status.LimitSource,
			SpentMicrosUSD:     status.SpentMicrosUSD,
			SpentUSD:           formatUSD(status.SpentMicrosUSD),
			CurrentMicrosUSD:   status.CurrentMicrosUSD,
			CurrentUSD:         formatUSD(status.CurrentMicrosUSD),
			MaxMicrosUSD:       status.MaxMicrosUSD,
			MaxUSD:             formatUSD(status.MaxMicrosUSD),
			RemainingMicrosUSD: status.RemainingMicrosUSD,
			RemainingUSD:       formatUSD(status.RemainingMicrosUSD),
			Enforced:           status.Enforced,
		},
	}
}

func budgetFilterFromMutation(key, scope, provider, tenant string) governor.BudgetFilter {
	return governor.BudgetFilter{
		Key:      key,
		Scope:    scope,
		Provider: provider,
		Tenant:   tenant,
	}
}

func budgetFilterFromRequest(r *http.Request) governor.BudgetFilter {
	query := r.URL.Query()
	return governor.BudgetFilter{
		Key:      query.Get("key"),
		Scope:    query.Get("scope"),
		Provider: query.Get("provider"),
		Tenant:   query.Get("tenant"),
	}
}

func formatUSD(micros int64) string {
	return fmt.Sprintf("%.6f", float64(micros)/1_000_000)
}

func mapUpstreamStatus(statusCode int) int {
	switch statusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusTooManyRequests:
		return statusCode
	default:
		return http.StatusBadGateway
	}
}
