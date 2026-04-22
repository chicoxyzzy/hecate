package api

import (
	"log/slog"
	"net/http"
)

func NewServer(logger *slog.Logger, handler *Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handler.HandleHealth)
	mux.HandleFunc("GET /v1/whoami", handler.HandleSession)
	mux.HandleFunc("GET /v1/chat/sessions", handler.HandleChatSessions)
	mux.HandleFunc("POST /v1/chat/sessions", handler.HandleCreateChatSession)
	mux.HandleFunc("GET /v1/chat/sessions/{id}", handler.HandleChatSession)
	mux.HandleFunc("PATCH /v1/chat/sessions/{id}", handler.HandleUpdateChatSession)
	mux.HandleFunc("DELETE /v1/chat/sessions/{id}", handler.HandleDeleteChatSession)
	mux.HandleFunc("GET /v1/traces", handler.HandleTrace)
	mux.HandleFunc("GET /admin/retention/runs", handler.HandleRetentionRuns)
	mux.HandleFunc("POST /admin/retention/run", handler.HandleRetentionRun)
	mux.HandleFunc("GET /admin/budget", handler.HandleBudgetStatus)
	mux.HandleFunc("GET /admin/accounts/summary", handler.HandleAccountSummary)
	mux.HandleFunc("GET /admin/requests", handler.HandleRequestLedger)
	mux.HandleFunc("POST /admin/budget/topup", handler.HandleBudgetTopUp)
	mux.HandleFunc("POST /admin/budget/limit", handler.HandleBudgetSetLimit)
	mux.HandleFunc("POST /admin/budget/reset", handler.HandleBudgetReset)
	mux.HandleFunc("GET /admin/control-plane", handler.HandleControlPlaneStatus)
	mux.HandleFunc("POST /admin/control-plane/tenants", handler.HandleControlPlaneUpsertTenant)
	mux.HandleFunc("POST /admin/control-plane/tenants/enabled", handler.HandleControlPlaneSetTenantEnabled)
	mux.HandleFunc("POST /admin/control-plane/tenants/delete", handler.HandleControlPlaneDeleteTenant)
	mux.HandleFunc("POST /admin/control-plane/api-keys", handler.HandleControlPlaneUpsertAPIKey)
	mux.HandleFunc("POST /admin/control-plane/api-keys/enabled", handler.HandleControlPlaneSetAPIKeyEnabled)
	mux.HandleFunc("POST /admin/control-plane/api-keys/rotate", handler.HandleControlPlaneRotateAPIKey)
	mux.HandleFunc("POST /admin/control-plane/api-keys/delete", handler.HandleControlPlaneDeleteAPIKey)
	mux.HandleFunc("GET /admin/providers", handler.HandleProviderStatus)
	mux.HandleFunc("GET /v1/models", handler.HandleModels)
	mux.HandleFunc("POST /v1/chat/completions", handler.HandleChatCompletions)

	return Chain(
		mux,
		RequestIDMiddleware,
		LoggingMiddleware(logger),
		RecoveryMiddleware(logger),
	)
}
