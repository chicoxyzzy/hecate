package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/gateway"
	"github.com/hecate/agent-runtime/internal/governor"
	"github.com/hecate/agent-runtime/internal/retention"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func (h *Handler) HandleProviderStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	result, err := h.service.ProviderStatus(ctx)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.providers.status.failed",
			slog.String("event.name", "gateway.providers.status.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
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

func (h *Handler) HandleRuntimeStats(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskRunner == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task runner is not configured")
		return
	}

	stats, err := h.taskRunner.RuntimeStats(ctx)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.runtime.stats.failed",
			slog.String("event.name", "gateway.runtime.stats.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, RuntimeStatsResponse{
		Object: "runtime_stats",
		Data: RuntimeStatsResponseItem{
			CheckedAt:               stats.CheckedAt.UTC().Format(time.RFC3339Nano),
			QueueDepth:              stats.QueueDepth,
			QueueCapacity:           stats.QueueCapacity,
			QueueBackend:            stats.QueueBackend,
			WorkerCount:             stats.WorkerCount,
			InFlightJobs:            stats.InFlightJobs,
			QueuedRuns:              stats.QueuedRuns,
			RunningRuns:             stats.RunningRuns,
			AwaitingApprovalRuns:    stats.AwaitingApprovalRuns,
			OldestQueuedAgeSeconds:  stats.OldestQueuedAgeSeconds,
			OldestRunningAgeSeconds: stats.OldestRunningAgeSeconds,
			StoreBackend:            stats.StoreBackend,
		},
	})
}

func (h *Handler) HandleRetentionRuns(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "limit query parameter must be a non-negative integer")
			return
		}
		if value > 200 {
			value = 200
		}
		limit = value
	}

	result, err := h.service.ListRetentionRuns(ctx, limit)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.retention.list.failed",
			slog.String("event.name", "gateway.retention.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	items := make([]RetentionRunData, 0, len(result.Runs))
	for _, run := range result.Runs {
		items = append(items, renderRetentionRunData(run.StartedAt, run.FinishedAt, run.Trigger, run.Actor, run.RequestID, run.Results))
	}

	WriteJSON(w, http.StatusOK, RetentionRunsResponse{
		Object: "retention_runs",
		Data:   items,
	})
}

type RetentionRunRequest struct {
	Subsystems []string `json:"subsystems"`
}

func (h *Handler) HandleRetentionRun(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	var req RetentionRunRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "request body must be valid JSON")
			return
		}
	}

	result, err := h.service.RunRetention(ctx, retention.RunRequest{
		Trigger:    "manual",
		Subsystems: req.Subsystems,
		Actor:      controlPlaneActor(principal, r),
		RequestID:  strings.TrimSpace(RequestIDFromContext(r.Context())),
	})
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.retention.run.failed",
			slog.String("event.name", "gateway.retention.run.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, RetentionRunResponse{
		Object: "retention_run",
		Data: renderRetentionRunData(
			result.Run.StartedAt.UTC().Format(time.RFC3339Nano),
			result.Run.FinishedAt.UTC().Format(time.RFC3339Nano),
			result.Run.Trigger,
			controlPlaneActor(principal, r),
			strings.TrimSpace(RequestIDFromContext(r.Context())),
			result.Run.Results,
		),
	})
}

func (h *Handler) HandleBudgetStatus(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	result, err := h.service.BudgetStatusWithFilter(ctx, budgetFilterFromRequest(r))
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.budget.status.failed",
			slog.String("event.name", "gateway.budget.status.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleAccountSummary(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	filter := budgetFilterFromRequest(r)
	result, err := h.service.AccountSummaryWithFilter(ctx, filter)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.accounts.summary.failed",
			slog.String("event.name", "gateway.accounts.summary.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	estimates := make([]AccountModelEstimateRecord, 0, len(result.Estimates))
	for _, estimate := range result.Estimates {
		estimates = append(estimates, AccountModelEstimateRecord{
			Provider:                        estimate.Provider,
			ProviderKind:                    estimate.ProviderKind,
			Model:                           estimate.Model,
			Default:                         estimate.Default,
			DiscoverySource:                 estimate.DiscoverySource,
			Priced:                          estimate.Priced,
			InputMicrosUSDPerMillionTokens:  estimate.InputMicrosUSDPerMillionTokens,
			OutputMicrosUSDPerMillionTokens: estimate.OutputMicrosUSDPerMillionTokens,
			EstimatedRemainingPromptTokens:  estimate.EstimatedRemainingPromptTokens,
			EstimatedRemainingOutputTokens:  estimate.EstimatedRemainingOutputTokens,
		})
	}

	WriteJSON(w, http.StatusOK, AccountSummaryResponse{
		Object: "account_summary",
		Data: AccountSummaryResponseItem{
			Account:   renderBudgetStatusRecord(result.Status),
			Estimates: estimates,
		},
	})
}

func (h *Handler) HandleRequestLedger(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "limit query parameter must be a non-negative integer")
			return
		}
		if value > 200 {
			value = 200
		}
		limit = value
	}

	result, err := h.service.RequestLedger(ctx, limit)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.requests.ledger.failed",
			slog.String("event.name", "gateway.requests.ledger.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, RequestLedgerResponse{
		Object: "request_ledger",
		Data:   renderBudgetHistoryRecords(result.Entries),
	})
}

func (h *Handler) HandleBudgetReset(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	var resetReq BudgetResetRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&resetReq); err != nil {
			WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "request body must be valid JSON")
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

	result, err := h.service.ResetBudgetWithFilter(ctx, filter)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.budget.reset.failed",
			slog.String("event.name", "gateway.budget.reset.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleBudgetTopUp(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	var topUpReq BudgetTopUpRequest
	if !decodeJSON(w, r, &topUpReq) {
		return
	}
	if topUpReq.AmountMicrosUSD <= 0 {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "amount_micros_usd must be greater than zero")
		return
	}

	filter := budgetFilterFromMutation(topUpReq.Key, topUpReq.Scope, topUpReq.Provider, topUpReq.Tenant)
	result, err := h.service.TopUpBudgetWithFilter(ctx, filter, topUpReq.AmountMicrosUSD)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.budget.top_up.failed",
			slog.String("event.name", "gateway.budget.top_up.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func (h *Handler) HandleBudgetSetLimit(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAdmin(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)

	var balanceReq BudgetBalanceRequest
	if !decodeJSON(w, r, &balanceReq) {
		return
	}
	if balanceReq.BalanceMicrosUSD < 0 {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "balance_micros_usd must be zero or greater")
		return
	}

	filter := budgetFilterFromMutation(balanceReq.Key, balanceReq.Scope, balanceReq.Provider, balanceReq.Tenant)
	result, err := h.service.SetBudgetBalanceWithFilter(ctx, filter, balanceReq.BalanceMicrosUSD)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.budget.limit_set.failed",
			slog.String("event.name", "gateway.budget.limit_set.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, renderBudgetStatusResponse(result))
}

func renderRetentionRunData(startedAt, finishedAt, trigger, actor, requestID string, results []retention.SubsystemResult) RetentionRunData {
	items := make([]RetentionRunResultRecord, 0, len(results))
	for _, item := range results {
		record := RetentionRunResultRecord{
			Name:     item.Name,
			Deleted:  item.Deleted,
			MaxCount: item.MaxCount,
			Error:    item.Error,
			Skipped:  item.Skipped,
		}
		if item.MaxAge > 0 {
			record.MaxAge = item.MaxAge.String()
		}
		items = append(items, record)
	}
	return RetentionRunData{
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Trigger:    trigger,
		Actor:      actor,
		RequestID:  requestID,
		Results:    items,
	}
}

func renderBudgetStatusResponse(result *gateway.BudgetStatusResult) BudgetStatusResponse {
	return BudgetStatusResponse{
		Object: "budget_status",
		Data:   renderBudgetStatusRecord(result.Status),
	}
}

func renderBudgetStatusRecord(status types.BudgetStatus) BudgetStatusResponseItem {
	warnings := make([]BudgetWarningRecord, 0, len(status.Warnings))
	for _, warning := range status.Warnings {
		warnings = append(warnings, BudgetWarningRecord{
			ThresholdPercent:   warning.ThresholdPercent,
			ThresholdMicrosUSD: warning.ThresholdMicrosUSD,
			BalanceMicrosUSD:   warning.BalanceMicrosUSD,
			AvailableMicrosUSD: warning.AvailableMicrosUSD,
			Triggered:          warning.Triggered,
		})
	}

	return BudgetStatusResponseItem{
		Key:                status.Key,
		Scope:              status.Scope,
		Provider:           status.Provider,
		Tenant:             status.Tenant,
		Backend:            status.Backend,
		BalanceSource:      status.BalanceSource,
		DebitedMicrosUSD:   status.DebitedMicrosUSD,
		DebitedUSD:         formatUSD(status.DebitedMicrosUSD),
		CreditedMicrosUSD:  status.CreditedMicrosUSD,
		CreditedUSD:        formatUSD(status.CreditedMicrosUSD),
		BalanceMicrosUSD:   status.BalanceMicrosUSD,
		BalanceUSD:         formatUSD(status.BalanceMicrosUSD),
		AvailableMicrosUSD: status.AvailableMicrosUSD,
		AvailableUSD:       formatUSD(status.AvailableMicrosUSD),
		Enforced:           status.Enforced,
		Warnings:           warnings,
		History:            renderBudgetHistoryRecords(status.History),
	}
}

func renderBudgetHistoryRecords(entries []types.BudgetHistoryEntry) []BudgetHistoryRecord {
	history := make([]BudgetHistoryRecord, 0, len(entries))
	for _, entry := range entries {
		item := BudgetHistoryRecord{
			Type:              entry.Type,
			Scope:             entry.Scope,
			Provider:          entry.Provider,
			Tenant:            entry.Tenant,
			Model:             entry.Model,
			RequestID:         entry.RequestID,
			Actor:             entry.Actor,
			Detail:            entry.Detail,
			AmountMicrosUSD:   entry.AmountMicrosUSD,
			AmountUSD:         formatUSD(entry.AmountMicrosUSD),
			BalanceMicrosUSD:  entry.BalanceMicrosUSD,
			BalanceUSD:        formatUSD(entry.BalanceMicrosUSD),
			CreditedMicrosUSD: entry.CreditedMicrosUSD,
			CreditedUSD:       formatUSD(entry.CreditedMicrosUSD),
			DebitedMicrosUSD:  entry.DebitedMicrosUSD,
			DebitedUSD:        formatUSD(entry.DebitedMicrosUSD),
			PromptTokens:      entry.PromptTokens,
			CompletionTokens:  entry.CompletionTokens,
			TotalTokens:       entry.TotalTokens,
		}
		if !entry.Timestamp.IsZero() {
			item.Timestamp = entry.Timestamp.UTC().Format(time.RFC3339Nano)
		}
		history = append(history, item)
	}
	return history
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
