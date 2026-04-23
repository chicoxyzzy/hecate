package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/orchestrator"
	"github.com/hecate/agent-runtime/internal/taskstate"
	"github.com/hecate/agent-runtime/internal/telemetry"
)

type Handler struct {
	config          config.Config
	logger          *slog.Logger
	service         *gateway.Service
	authenticator   *auth.Authenticator
	controlPlane    controlplane.Store
	providerRuntime ProviderRuntime
	taskStore       taskstate.Store
	taskRunner      *orchestrator.Runner
}

type ProviderRuntime interface {
	Reload(ctx context.Context) error
	SecretStorageEnabled() bool
	Upsert(ctx context.Context, provider controlplane.Provider, apiKey string) (controlplane.Provider, error)
	SetEnabled(ctx context.Context, id string, enabled bool) (controlplane.Provider, error)
	RotateSecret(ctx context.Context, id, apiKey string) (controlplane.Provider, error)
	Delete(ctx context.Context, id string) error
}

func NewHandler(cfg config.Config, logger *slog.Logger, service *gateway.Service, cpStore controlplane.Store, providerRuntimes ...ProviderRuntime) *Handler {
	var providerRuntime ProviderRuntime
	if len(providerRuntimes) > 0 {
		providerRuntime = providerRuntimes[0]
	}
	taskStore := taskstate.NewMemoryStore()
	return &Handler{
		config:          cfg,
		logger:          logger,
		service:         service,
		authenticator:   auth.NewAuthenticator(cfg.Server, cpStore),
		controlPlane:    cpStore,
		providerRuntime: providerRuntime,
		taskStore:       taskStore,
		taskRunner: orchestrator.NewRunner(logger, taskStore, service.Tracer(), orchestrator.Config{
			DefaultModel: cfg.Router.DefaultModel,
		}),
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
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	result, err := h.service.ListModels(ctx)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.models.list.failed",
			slog.String("event.name", "gateway.models.list.failed"),
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

// contextWithPrincipal attaches principal identity to the context for telemetry.
func (h *Handler) contextWithPrincipal(ctx context.Context, principal auth.Principal) context.Context {
	return telemetry.WithPrincipal(ctx, telemetry.Principal{
		Name:     principal.Name,
		Role:     principal.Role,
		TenantID: principal.Tenant,
		Source:   principal.Source,
		KeyID:    principal.KeyID,
	})
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

// requireAny authenticates any valid principal and writes a 401 on failure.
func (h *Handler) requireAny(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := h.authorizeAny(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, errCodeUnauthorized, "missing or invalid bearer token")
		return auth.Principal{}, false
	}
	return principal, true
}

// requireAdmin authenticates an admin principal and writes a 401 on failure.
func (h *Handler) requireAdmin(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := h.authorizeAdmin(r)
	if !ok {
		WriteError(w, http.StatusUnauthorized, errCodeUnauthorized, "missing or invalid bearer token")
		return auth.Principal{}, false
	}
	return principal, true
}

// requireControlPlane authenticates an admin and verifies the control plane is configured.
func (h *Handler) requireControlPlane(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return auth.Principal{}, false
	}
	if h.controlPlane == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "control plane backend is not configured")
		return auth.Principal{}, false
	}
	return principal, true
}

// controlPlaneActor builds an actor string for audit log entries.
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

// decodeJSON decodes the request body into v and writes a 400 on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return false
	}
	return true
}

func formatUSD(micros int64) string {
	return fmt.Sprintf("%.6f", float64(micros)/1_000_000)
}
