package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/auth"
	"github.com/hecate/agent-runtime/internal/taskstate"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

func (h *Handler) HandleCreateTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}

	var req CreateTaskRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	title := strings.TrimSpace(req.Title)
	prompt := strings.TrimSpace(req.Prompt)
	if title == "" {
		if prompt == "" {
			title = "New task"
		} else {
			title = prompt
			if len(title) > 80 {
				title = strings.TrimSpace(title[:80]) + "..."
			}
		}
	}
	if prompt == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "prompt is required")
		return
	}

	workspaceMode := strings.TrimSpace(req.WorkspaceMode)
	if workspaceMode == "" {
		workspaceMode = "ephemeral"
	}
	priority := strings.TrimSpace(req.Priority)
	if priority == "" {
		priority = "normal"
	}

	now := time.Now().UTC()
	task := types.Task{
		ID:                 newTaskID(),
		Title:              title,
		Prompt:             prompt,
		Tenant:             principal.Tenant,
		User:               principal.Name,
		Repo:               strings.TrimSpace(req.Repo),
		BaseBranch:         strings.TrimSpace(req.BaseBranch),
		WorkspaceMode:      workspaceMode,
		ExecutionKind:      strings.TrimSpace(req.ExecutionKind),
		ShellCommand:       strings.TrimSpace(req.ShellCommand),
		GitCommand:         strings.TrimSpace(req.GitCommand),
		WorkingDirectory:   strings.TrimSpace(req.WorkingDirectory),
		FileOperation:      strings.TrimSpace(req.FileOperation),
		FilePath:           strings.TrimSpace(req.FilePath),
		FileContent:        req.FileContent,
		SandboxAllowedRoot: strings.TrimSpace(req.SandboxAllowedRoot),
		SandboxReadOnly:    req.SandboxReadOnly,
		SandboxNetwork:     req.SandboxNetwork,
		TimeoutMS:          req.TimeoutMS,
		Status:             "queued",
		Priority:           priority,
		RequestedModel:     strings.TrimSpace(req.RequestedModel),
		RequestedProvider:  strings.TrimSpace(req.RequestedProvider),
		BudgetMicrosUSD:    req.BudgetMicrosUSD,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	created, err := h.taskStore.CreateTask(ctx, task)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.create.failed",
			slog.String("event.name", "gateway.tasks.create.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, TaskResponse{
		Object: "task",
		Data:   buildTaskItem(ctx, h.taskStore, created),
	})
}

func (h *Handler) HandleTasks(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}

	limit := 50
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

	filter := taskstate.TaskFilter{
		Status: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:  limit,
	}
	if principal.IsAdmin() {
		filter.Tenant = strings.TrimSpace(r.URL.Query().Get("tenant"))
	} else {
		filter.Tenant = principal.Tenant
	}

	result, err := h.taskStore.ListTasks(ctx, filter)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.list.failed",
			slog.String("event.name", "gateway.tasks.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	items := make([]TaskItem, 0, len(result))
	for _, task := range result {
		if !principal.IsAdmin() && principal.Tenant != "" && task.Tenant != principal.Tenant {
			continue
		}
		items = append(items, buildTaskItem(ctx, h.taskStore, task))
	}
	WriteJSON(w, http.StatusOK, TasksResponse{
		Object: "tasks",
		Data:   items,
	})
}

func (h *Handler) HandleTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}

	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task id is required")
		return
	}

	task, found, err := h.taskStore.GetTask(ctx, id)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.get.failed",
			slog.String("event.name", "gateway.tasks.get.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task not found")
		return
	}
	if !principal.IsAdmin() && principal.Tenant != "" && task.Tenant != principal.Tenant {
		WriteError(w, http.StatusForbidden, errCodeForbidden, "task is outside the active tenant scope")
		return
	}

	WriteJSON(w, http.StatusOK, TaskResponse{
		Object: "task",
		Data:   buildTaskItem(ctx, h.taskStore, task),
	})
}

func (h *Handler) HandleStartTask(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	if h.taskRunner == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task runner is not configured")
		return
	}

	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	result, err := h.taskRunner.StartTask(ctx, task, newOpaqueTaskResourceID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.start.failed",
			slog.String("event.name", "gateway.tasks.start.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	if result.TraceID != "" {
		w.Header().Set("X-Trace-Id", result.TraceID)
	}
	if result.SpanID != "" {
		w.Header().Set("X-Span-Id", result.SpanID)
	}
	WriteJSON(w, http.StatusOK, TaskRunResponse{
		Object: "task_run",
		Data:   renderTaskRun(result.Run),
	})
}

func (h *Handler) HandleTaskApprovals(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}

	approvals, err := h.taskStore.ListApprovals(ctx, task.ID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.approvals.list.failed",
			slog.String("event.name", "gateway.tasks.approvals.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	items := make([]TaskApprovalItem, 0, len(approvals))
	for _, approval := range approvals {
		items = append(items, renderTaskApproval(approval))
	}
	WriteJSON(w, http.StatusOK, TaskApprovalsResponse{
		Object: "task_approvals",
		Data:   items,
	})
}

func (h *Handler) HandleTaskApproval(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	approvalID := strings.TrimSpace(r.PathValue("approval_id"))
	if approvalID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "approval id is required")
		return
	}

	approval, found, err := h.taskStore.GetApproval(ctx, task.ID, approvalID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.approvals.get.failed",
			slog.String("event.name", "gateway.tasks.approvals.get.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task approval not found")
		return
	}

	WriteJSON(w, http.StatusOK, TaskApprovalResponse{
		Object: "task_approval",
		Data:   renderTaskApproval(approval),
	})
}

func (h *Handler) HandleResolveTaskApproval(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	if h.taskRunner == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task runner is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	approvalID := strings.TrimSpace(r.PathValue("approval_id"))
	if approvalID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "approval id is required")
		return
	}

	approval, found, err := h.taskStore.GetApproval(ctx, task.ID, approvalID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.approvals.resolve.failed",
			slog.String("event.name", "gateway.tasks.approvals.resolve.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task approval not found")
		return
	}
	if approval.Status != "pending" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task approval is not pending")
		return
	}

	var req ResolveTaskApprovalRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	decision, ok := normalizeApprovalDecision(req.Decision)
	if !ok {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "decision must be approve or reject")
		return
	}

	now := time.Now().UTC()
	approval.Status = decision
	approval.ResolutionNote = strings.TrimSpace(req.Note)
	approval.ResolvedBy = principal.Name
	approval.ResolvedAt = now
	approval, err = h.taskStore.UpdateApproval(ctx, approval)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	switch decision {
	case "approved":
		result, err := h.taskRunner.ResumeTaskAfterApproval(ctx, task, approval, newOpaqueTaskResourceID)
		if err != nil {
			telemetry.Error(h.logger, ctx, "gateway.tasks.approvals.resume.failed",
				slog.String("event.name", "gateway.tasks.approvals.resume.failed"),
				slog.Any("error", err),
			)
			WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
			return
		}
		if result.TraceID != "" {
			w.Header().Set("X-Trace-Id", result.TraceID)
		}
		if result.SpanID != "" {
			w.Header().Set("X-Span-Id", result.SpanID)
		}
	case "rejected":
		run, found, err := h.taskStore.GetRun(ctx, task.ID, approval.RunID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
			return
		}
		if !found {
			WriteError(w, http.StatusNotFound, errCodeNotFound, "task run not found")
			return
		}
		run.Status = "cancelled"
		run.LastError = "approval rejected"
		run.FinishedAt = now
		run.OtelStatusCode = "error"
		run.OtelStatusMessage = "approval rejected"
		if _, err := h.taskStore.UpdateRun(ctx, run); err != nil {
			WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
			return
		}

		task.Status = "cancelled"
		task.LatestRunID = run.ID
		if task.StartedAt.IsZero() {
			task.StartedAt = run.StartedAt
		}
		task.FinishedAt = now
		task.UpdatedAt = now
		task.LastError = "approval rejected"
		if requestID := strings.TrimSpace(telemetry.RequestIDFromContext(ctx)); requestID != "" {
			task.LatestRequestID = requestID
		}
		if _, err := h.taskStore.UpdateTask(ctx, task); err != nil {
			WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
			return
		}
	}

	WriteJSON(w, http.StatusOK, TaskApprovalResponse{
		Object: "task_approval",
		Data:   renderTaskApproval(approval),
	})
}

func (h *Handler) HandleTaskRuns(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}

	runs, err := h.taskStore.ListRuns(ctx, task.ID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.runs.list.failed",
			slog.String("event.name", "gateway.tasks.runs.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}

	items := make([]TaskRunItem, 0, len(runs))
	for _, run := range runs {
		items = append(items, renderTaskRun(run))
	}
	WriteJSON(w, http.StatusOK, TaskRunsResponse{
		Object: "task_runs",
		Data:   items,
	})
}

func (h *Handler) HandleTaskRun(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if runID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "run id is required")
		return
	}
	run, found, err := h.taskStore.GetRun(ctx, task.ID, runID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.runs.get.failed",
			slog.String("event.name", "gateway.tasks.runs.get.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task run not found")
		return
	}

	WriteJSON(w, http.StatusOK, TaskRunResponse{
		Object: "task_run",
		Data:   renderTaskRun(run),
	})
}

func (h *Handler) HandleCancelTaskRun(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	if h.taskRunner == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task runner is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if runID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "run id is required")
		return
	}
	run, err := h.taskRunner.CancelRun(ctx, task, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			WriteError(w, http.StatusNotFound, errCodeNotFound, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, TaskRunResponse{
		Object: "task_run",
		Data:   renderTaskRun(run),
	})
}

func (h *Handler) HandleTaskRunSteps(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if runID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "run id is required")
		return
	}
	if _, found, err := h.taskStore.GetRun(ctx, task.ID, runID); err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	} else if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task run not found")
		return
	}

	steps, err := h.taskStore.ListSteps(ctx, runID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.steps.list.failed",
			slog.String("event.name", "gateway.tasks.steps.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	items := make([]TaskStepItem, 0, len(steps))
	for _, step := range steps {
		items = append(items, renderTaskStep(step))
	}
	WriteJSON(w, http.StatusOK, TaskStepsResponse{
		Object: "task_steps",
		Data:   items,
	})
}

func (h *Handler) HandleTaskRunStep(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	runID := strings.TrimSpace(r.PathValue("run_id"))
	stepID := strings.TrimSpace(r.PathValue("step_id"))
	if runID == "" || stepID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "run id and step id are required")
		return
	}
	if _, found, err := h.taskStore.GetRun(ctx, task.ID, runID); err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	} else if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task run not found")
		return
	}
	step, found, err := h.taskStore.GetStep(ctx, runID, stepID)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.steps.get.failed",
			slog.String("event.name", "gateway.tasks.steps.get.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task step not found")
		return
	}
	WriteJSON(w, http.StatusOK, TaskStepResponse{
		Object: "task_step",
		Data:   renderTaskStep(step),
	})
}

func (h *Handler) HandleTaskArtifacts(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}

	artifacts, err := h.taskStore.ListArtifacts(ctx, taskstate.ArtifactFilter{TaskID: task.ID})
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.artifacts.list.failed",
			slog.String("event.name", "gateway.tasks.artifacts.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	items := make([]TaskArtifactItem, 0, len(artifacts))
	for _, artifact := range artifacts {
		items = append(items, renderTaskArtifact(artifact))
	}
	WriteJSON(w, http.StatusOK, TaskArtifactsResponse{
		Object: "task_artifacts",
		Data:   items,
	})
}

func (h *Handler) HandleTaskRunArtifacts(w http.ResponseWriter, r *http.Request) {
	principal, ok := h.requireAny(w, r)
	if !ok {
		return
	}
	ctx := h.contextWithPrincipal(r.Context(), principal)
	if h.taskStore == nil {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task store is not configured")
		return
	}
	task, ok := h.loadAuthorizedTask(ctx, w, r, principal)
	if !ok {
		return
	}
	runID := strings.TrimSpace(r.PathValue("run_id"))
	if runID == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "run id is required")
		return
	}
	if _, found, err := h.taskStore.GetRun(ctx, task.ID, runID); err != nil {
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	} else if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task run not found")
		return
	}

	artifacts, err := h.taskStore.ListArtifacts(ctx, taskstate.ArtifactFilter{TaskID: task.ID, RunID: runID})
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.run_artifacts.list.failed",
			slog.String("event.name", "gateway.tasks.run_artifacts.list.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return
	}
	items := make([]TaskArtifactItem, 0, len(artifacts))
	for _, artifact := range artifacts {
		items = append(items, renderTaskArtifact(artifact))
	}
	WriteJSON(w, http.StatusOK, TaskArtifactsResponse{
		Object: "task_artifacts",
		Data:   items,
	})
}

func buildTaskItem(ctx context.Context, store taskstate.Store, task types.Task) TaskItem {
	item := TaskItem{
		ID:                 task.ID,
		Title:              task.Title,
		Prompt:             task.Prompt,
		Tenant:             task.Tenant,
		User:               task.User,
		Repo:               task.Repo,
		BaseBranch:         task.BaseBranch,
		WorkspaceMode:      task.WorkspaceMode,
		ExecutionKind:      task.ExecutionKind,
		ShellCommand:       task.ShellCommand,
		GitCommand:         task.GitCommand,
		WorkingDirectory:   task.WorkingDirectory,
		FileOperation:      task.FileOperation,
		FilePath:           task.FilePath,
		FileContent:        task.FileContent,
		SandboxAllowedRoot: task.SandboxAllowedRoot,
		SandboxReadOnly:    task.SandboxReadOnly,
		SandboxNetwork:     task.SandboxNetwork,
		TimeoutMS:          task.TimeoutMS,
		Status:             task.Status,
		Priority:           task.Priority,
		RequestedModel:     task.RequestedModel,
		RequestedProvider:  task.RequestedProvider,
		BudgetMicrosUSD:    task.BudgetMicrosUSD,
		LatestRunID:        task.LatestRunID,
		LastError:          task.LastError,
		RootTraceID:        task.RootTraceID,
		LatestTraceID:      task.LatestTraceID,
		LatestRequestID:    task.LatestRequestID,
	}
	if !task.CreatedAt.IsZero() {
		item.CreatedAt = task.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !task.UpdatedAt.IsZero() {
		item.UpdatedAt = task.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !task.StartedAt.IsZero() {
		item.StartedAt = task.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !task.FinishedAt.IsZero() {
		item.FinishedAt = task.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	if store != nil {
		runs, _ := store.ListRuns(ctx, task.ID)
		approvals, _ := store.ListApprovals(ctx, task.ID)
		artifacts, _ := store.ListArtifacts(ctx, taskstate.ArtifactFilter{TaskID: task.ID})
		item.ArtifactCount = len(artifacts)
		pending := 0
		stepCount := 0
		for _, approval := range approvals {
			if approval.Status == "pending" {
				pending++
			}
		}
		item.PendingApprovalCount = pending
		for _, run := range runs {
			steps, _ := store.ListSteps(ctx, run.ID)
			stepCount += len(steps)
		}
		item.StepCount = stepCount
	}
	return item
}

func renderTaskRun(run types.TaskRun) TaskRunItem {
	item := TaskRunItem{
		ID:                 run.ID,
		TaskID:             run.TaskID,
		Number:             run.Number,
		Status:             run.Status,
		Orchestrator:       run.Orchestrator,
		Model:              run.Model,
		Provider:           run.Provider,
		ProviderKind:       run.ProviderKind,
		WorkspaceID:        run.WorkspaceID,
		WorkspacePath:      run.WorkspacePath,
		StepCount:          run.StepCount,
		ApprovalCount:      run.ApprovalCount,
		ArtifactCount:      run.ArtifactCount,
		TotalCostMicrosUSD: run.TotalCostMicrosUSD,
		LastError:          run.LastError,
		RequestID:          run.RequestID,
		TraceID:            run.TraceID,
		RootSpanID:         run.RootSpanID,
		OtelStatusCode:     run.OtelStatusCode,
		OtelStatusMessage:  run.OtelStatusMessage,
	}
	if !run.StartedAt.IsZero() {
		item.StartedAt = run.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !run.FinishedAt.IsZero() {
		item.FinishedAt = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return item
}

func renderTaskStep(step types.TaskStep) TaskStepItem {
	item := TaskStepItem{
		ID:            step.ID,
		TaskID:        step.TaskID,
		RunID:         step.RunID,
		ParentStepID:  step.ParentStepID,
		Index:         step.Index,
		Kind:          step.Kind,
		Title:         step.Title,
		Status:        step.Status,
		Phase:         step.Phase,
		Result:        step.Result,
		ToolName:      step.ToolName,
		Input:         step.Input,
		OutputSummary: step.OutputSummary,
		ExitCode:      step.ExitCode,
		Error:         step.Error,
		ErrorKind:     step.ErrorKind,
		ApprovalID:    step.ApprovalID,
		RequestID:     step.RequestID,
		TraceID:       step.TraceID,
		SpanID:        step.SpanID,
		ParentSpanID:  step.ParentSpanID,
	}
	if !step.StartedAt.IsZero() {
		item.StartedAt = step.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !step.FinishedAt.IsZero() {
		item.FinishedAt = step.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return item
}

func renderTaskApproval(approval types.TaskApproval) TaskApprovalItem {
	item := TaskApprovalItem{
		ID:             approval.ID,
		TaskID:         approval.TaskID,
		RunID:          approval.RunID,
		StepID:         approval.StepID,
		Kind:           approval.Kind,
		Status:         approval.Status,
		Reason:         approval.Reason,
		RequestedBy:    approval.RequestedBy,
		ResolvedBy:     approval.ResolvedBy,
		ResolutionNote: approval.ResolutionNote,
		RequestID:      approval.RequestID,
		TraceID:        approval.TraceID,
		SpanID:         approval.SpanID,
	}
	if !approval.CreatedAt.IsZero() {
		item.CreatedAt = approval.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !approval.ResolvedAt.IsZero() {
		item.ResolvedAt = approval.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	return item
}

func renderTaskArtifact(artifact types.TaskArtifact) TaskArtifactItem {
	item := TaskArtifactItem{
		ID:          artifact.ID,
		TaskID:      artifact.TaskID,
		RunID:       artifact.RunID,
		StepID:      artifact.StepID,
		Kind:        artifact.Kind,
		Name:        artifact.Name,
		Description: artifact.Description,
		MimeType:    artifact.MimeType,
		StorageKind: artifact.StorageKind,
		Path:        artifact.Path,
		ContentText: artifact.ContentText,
		ObjectRef:   artifact.ObjectRef,
		SizeBytes:   artifact.SizeBytes,
		SHA256:      artifact.SHA256,
		Status:      artifact.Status,
		RequestID:   artifact.RequestID,
		TraceID:     artifact.TraceID,
		SpanID:      artifact.SpanID,
	}
	if !artifact.CreatedAt.IsZero() {
		item.CreatedAt = artifact.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	return item
}

func (h *Handler) loadAuthorizedTask(ctx context.Context, w http.ResponseWriter, r *http.Request, principal auth.Principal) (types.Task, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "task id is required")
		return types.Task{}, false
	}

	task, found, err := h.taskStore.GetTask(ctx, id)
	if err != nil {
		telemetry.Error(h.logger, ctx, "gateway.tasks.load.failed",
			slog.String("event.name", "gateway.tasks.load.failed"),
			slog.Any("error", err),
		)
		WriteError(w, http.StatusInternalServerError, errCodeGatewayError, err.Error())
		return types.Task{}, false
	}
	if !found {
		WriteError(w, http.StatusNotFound, errCodeNotFound, "task not found")
		return types.Task{}, false
	}
	if !principal.IsAdmin() && principal.Tenant != "" && task.Tenant != principal.Tenant {
		WriteError(w, http.StatusForbidden, errCodeForbidden, "task is outside the active tenant scope")
		return types.Task{}, false
	}
	return task, true
}

func newTaskID() string {
	return newOpaqueTaskResourceID("task")
}

func newTaskRunID() string {
	return newOpaqueTaskResourceID("run")
}

func newTaskStepID() string {
	return newOpaqueTaskResourceID("step")
}

func newTaskArtifactID() string {
	return newOpaqueTaskResourceID("artifact")
}

func normalizeApprovalDecision(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approve", "approved":
		return "approved", true
	case "reject", "rejected", "deny", "denied":
		return "rejected", true
	default:
		return "", false
	}
}

func newOpaqueTaskResourceID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "_" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + "_" + hex.EncodeToString(buf)
}
