package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/sandbox"
	"github.com/hecate/agent-runtime/internal/taskstate"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Config struct {
	DefaultModel string
}

type queuedRun struct {
	ctx    context.Context
	taskID string
	runID  string
	idgen  func(prefix string) string
}

type Runner struct {
	logger     *slog.Logger
	store      taskstate.Store
	tracer     profiler.Tracer
	exec       Executor
	shell      Executor
	file       Executor
	git        Executor
	workspaces *WorkspaceManager
	config     Config
	queue      chan queuedRun
	jobMu      sync.Mutex
	jobs       map[string]context.CancelFunc
}

type StartTaskResult struct {
	Task      types.Task
	Run       types.TaskRun
	Steps     []types.TaskStep
	Artifacts []types.TaskArtifact
	TraceID   string
	SpanID    string
}

func NewRunner(logger *slog.Logger, store taskstate.Store, tracer profiler.Tracer, cfg Config) *Runner {
	if tracer == nil {
		tracer = profiler.NewInMemoryTracer(nil)
	}
	worker := sandbox.NewWorkerExecutor()
	runner := &Runner{
		logger:     logger,
		store:      store,
		tracer:     tracer,
		exec:       NewStubExecutor(),
		shell:      NewShellExecutor(worker),
		file:       NewFileExecutor(worker),
		git:        NewGitExecutor(worker),
		workspaces: NewWorkspaceManager(""),
		config:     cfg,
		queue:      make(chan queuedRun, 128),
		jobs:       make(map[string]context.CancelFunc),
	}
	go runner.processQueue()
	return runner
}

func (r *Runner) SetExecutor(exec Executor) {
	if exec == nil {
		return
	}
	r.exec = exec
}

func (r *Runner) SetShellExecutor(exec Executor) {
	if exec == nil {
		return
	}
	r.shell = exec
}

func (r *Runner) SetFileExecutor(exec Executor) {
	if exec == nil {
		return
	}
	r.file = exec
}

func (r *Runner) SetGitExecutor(exec Executor) {
	if exec == nil {
		return
	}
	r.git = exec
}

func (r *Runner) StartTask(ctx context.Context, task types.Task, idgen func(prefix string) string) (*StartTaskResult, error) {
	if r.store == nil {
		return nil, fmt.Errorf("task store is not configured")
	}
	if idgen == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	if r.exec == nil {
		return nil, fmt.Errorf("executor is not configured")
	}
	if r.workspaces == nil {
		return nil, fmt.Errorf("workspace manager is not configured")
	}

	requestID := strings.TrimSpace(telemetry.RequestIDFromContext(ctx))
	if requestID == "" {
		requestID = idgen("request")
	}

	trace := r.tracer.Start(requestID)
	defer trace.Finalize()

	ctx = telemetry.WithTraceIDs(ctx, trace.TraceID, trace.RootSpanID())
	now := time.Now().UTC()

	trace.Record("orchestrator.task.started", map[string]any{
		telemetry.AttrHecatePhase:  "orchestration",
		telemetry.AttrHecateResult: telemetry.ResultSuccess,
		"hecate.task.id":           task.ID,
		"hecate.task.status":       task.Status,
		"hecate.task.repo":         task.Repo,
		"hecate.task.base_branch":  task.BaseBranch,
	})

	runs, err := r.store.ListRuns(ctx, task.ID)
	if err != nil {
		trace.Record("orchestrator.run.failed", map[string]any{
			telemetry.AttrHecatePhase:     "orchestration",
			telemetry.AttrHecateResult:    telemetry.ResultError,
			telemetry.AttrHecateErrorKind: "run_list_failed",
			telemetry.AttrErrorType:       "run_list_failed",
			telemetry.AttrErrorMessage:    err.Error(),
			"hecate.task.id":              task.ID,
		})
		return nil, err
	}

	run := types.TaskRun{
		ID:           idgen("run"),
		TaskID:       task.ID,
		Number:       len(runs) + 1,
		Status:       "queued",
		Orchestrator: "builtin",
		Model:        firstNonEmpty(task.RequestedModel, r.config.DefaultModel),
		Provider:     task.RequestedProvider,
		WorkspaceID:  "workspace_" + task.ID,
		StartedAt:    now,
		RequestID:    requestID,
		TraceID:      trace.TraceID,
		RootSpanID:   trace.RootSpanID(),
	}
	if r.approvalRequiredForTask(task) {
		run.Status = "awaiting_approval"
	}
	run.WorkspacePath, err = r.workspaces.Provision(ctx, task, run)
	if err != nil {
		trace.Record("orchestrator.run.failed", map[string]any{
			telemetry.AttrHecatePhase:     "orchestration",
			telemetry.AttrHecateResult:    telemetry.ResultError,
			telemetry.AttrHecateErrorKind: "workspace_provision_failed",
			telemetry.AttrErrorType:       "workspace_provision_failed",
			telemetry.AttrErrorMessage:    err.Error(),
			"hecate.task.id":              task.ID,
			"hecate.run.id":               run.ID,
		})
		return nil, err
	}
	run, err = r.store.CreateRun(ctx, run)
	if err != nil {
		trace.Record("orchestrator.run.failed", map[string]any{
			telemetry.AttrHecatePhase:     "orchestration",
			telemetry.AttrHecateResult:    telemetry.ResultError,
			telemetry.AttrHecateErrorKind: "run_create_failed",
			telemetry.AttrErrorType:       "run_create_failed",
			telemetry.AttrErrorMessage:    err.Error(),
			"hecate.task.id":              task.ID,
			"hecate.run.id":               run.ID,
		})
		return nil, err
	}

	trace.Record("orchestrator.run.started", map[string]any{
		telemetry.AttrHecatePhase:       "orchestration",
		telemetry.AttrHecateResult:      telemetry.ResultSuccess,
		"hecate.task.id":                task.ID,
		"hecate.run.id":                 run.ID,
		"hecate.run.number":             run.Number,
		"hecate.run.status":             run.Status,
		telemetry.AttrGenAIRequestModel: run.Model,
	})

	task.LatestRunID = run.ID
	task.Status = run.Status
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	task.FinishedAt = time.Time{}
	task.UpdatedAt = now
	task.RootTraceID = trace.TraceID
	task.LatestTraceID = trace.TraceID
	task.LatestRequestID = requestID

	if r.approvalRequiredForTask(task) {
		if _, err := r.createApprovalForTask(ctx, trace, task, run, requestID, now, idgen); err != nil {
			return nil, err
		}
		run.ApprovalCount = 1
		run, err = r.store.UpdateRun(ctx, run)
		if err != nil {
			return nil, err
		}
		task.Status = "awaiting_approval"
	} else if err := r.enqueueRun(task.ID, run.ID, idgen); err != nil {
		return nil, err
	}

	if _, err := r.store.UpdateTask(ctx, task); err != nil {
		return nil, err
	}

	return &StartTaskResult{
		Task:    task,
		Run:     run,
		TraceID: trace.TraceID,
		SpanID:  trace.RootSpanID(),
	}, nil
}

func (r *Runner) ResumeTaskAfterApproval(ctx context.Context, task types.Task, approval types.TaskApproval, idgen func(prefix string) string) (*StartTaskResult, error) {
	if r.store == nil {
		return nil, fmt.Errorf("task store is not configured")
	}
	if idgen == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	run, found, err := r.store.GetRun(ctx, task.ID, approval.RunID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("task run %q not found", approval.RunID)
	}
	if run.Status != "awaiting_approval" {
		return nil, fmt.Errorf("task run %q is not awaiting approval", run.ID)
	}

	requestID := strings.TrimSpace(telemetry.RequestIDFromContext(ctx))
	if requestID == "" {
		requestID = idgen("request")
	}

	trace := r.tracer.Start(requestID)
	defer trace.Finalize()

	ctx = telemetry.WithTraceIDs(ctx, trace.TraceID, trace.RootSpanID())
	now := time.Now().UTC()
	trace.Record("orchestrator.approval.resolved", map[string]any{
		telemetry.AttrHecatePhase:  "approval",
		telemetry.AttrHecateResult: telemetry.ResultSuccess,
		"hecate.task.id":           task.ID,
		"hecate.run.id":            run.ID,
		"hecate.approval.id":       approval.ID,
		"hecate.approval.kind":     approval.Kind,
		"hecate.approval.status":   approval.Status,
	})

	run.Status = "queued"
	run.RequestID = requestID
	run.TraceID = trace.TraceID
	run.RootSpanID = trace.RootSpanID()
	run.LastError = ""
	run.FinishedAt = time.Time{}
	if _, err := r.store.UpdateRun(ctx, run); err != nil {
		return nil, err
	}

	task.Status = "queued"
	task.LatestRunID = run.ID
	task.UpdatedAt = now
	task.FinishedAt = time.Time{}
	task.LastError = ""
	task.LatestTraceID = trace.TraceID
	task.LatestRequestID = requestID
	if _, err := r.store.UpdateTask(ctx, task); err != nil {
		return nil, err
	}

	if err := r.enqueueRun(task.ID, run.ID, idgen); err != nil {
		return nil, err
	}

	return &StartTaskResult{
		Task:    task,
		Run:     run,
		TraceID: trace.TraceID,
		SpanID:  trace.RootSpanID(),
	}, nil
}

func (r *Runner) RejectTaskAfterApproval(ctx context.Context, task types.Task, approval types.TaskApproval, idgen func(prefix string) string) (*StartTaskResult, error) {
	if r.store == nil {
		return nil, fmt.Errorf("task store is not configured")
	}
	if idgen == nil {
		return nil, fmt.Errorf("resource id generator is required")
	}
	run, found, err := r.store.GetRun(ctx, task.ID, approval.RunID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("task run %q not found", approval.RunID)
	}
	if run.Status != "awaiting_approval" {
		return nil, fmt.Errorf("task run %q is not awaiting approval", run.ID)
	}

	requestID := strings.TrimSpace(telemetry.RequestIDFromContext(ctx))
	if requestID == "" {
		requestID = firstNonEmpty(approval.RequestID, run.RequestID)
	}
	if requestID == "" {
		requestID = idgen("request")
	}

	trace := r.tracer.Start(requestID)
	defer trace.Finalize()

	ctx = telemetry.WithTraceIDs(ctx, trace.TraceID, trace.RootSpanID())
	trace.Record("orchestrator.approval.resolved", map[string]any{
		telemetry.AttrHecatePhase:  "approval",
		telemetry.AttrHecateResult: telemetry.ResultSuccess,
		"hecate.task.id":           task.ID,
		"hecate.run.id":            run.ID,
		"hecate.approval.id":       approval.ID,
		"hecate.approval.kind":     approval.Kind,
		"hecate.approval.status":   approval.Status,
	})

	run, err = r.cancelRunWithMessage(ctx, task, run, "approval rejected", requestID, trace.TraceID)
	if err != nil {
		return nil, err
	}
	task, _, err = r.store.GetTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	return &StartTaskResult{
		Task:    task,
		Run:     run,
		TraceID: trace.TraceID,
		SpanID:  trace.RootSpanID(),
	}, nil
}

func (r *Runner) CancelRun(ctx context.Context, task types.Task, runID string) (types.TaskRun, error) {
	run, found, err := r.store.GetRun(ctx, task.ID, runID)
	if err != nil {
		return types.TaskRun{}, err
	}
	if !found {
		return types.TaskRun{}, fmt.Errorf("task run %q not found", runID)
	}
	if types.IsTerminalTaskRunStatus(run.Status) {
		return run, nil
	}

	requestID := strings.TrimSpace(telemetry.RequestIDFromContext(ctx))
	traceIDs := telemetry.TraceIDsFromContext(ctx)
	return r.cancelRunWithMessage(ctx, task, run, "run cancelled", requestID, traceIDs.TraceID)
}

func (r *Runner) cancelRunWithMessage(ctx context.Context, task types.Task, run types.TaskRun, message, requestID, traceID string) (types.TaskRun, error) {
	r.jobMu.Lock()
	cancel, ok := r.jobs[run.ID]
	r.jobMu.Unlock()
	if ok {
		cancel()
	}

	now := time.Now().UTC()
	run.Status = "cancelled"
	run.LastError = message
	run.FinishedAt = now
	run.OtelStatusCode = "error"
	run.OtelStatusMessage = message
	if requestID != "" {
		run.RequestID = requestID
	}
	if traceID != "" {
		run.TraceID = traceID
	}
	var err error
	run, err = r.store.UpdateRun(ctx, run)
	if err != nil {
		return types.TaskRun{}, err
	}

	steps, _ := r.store.ListSteps(ctx, run.ID)
	for _, step := range steps {
		if step.Status == "running" {
			step.Status = "cancelled"
			step.Result = telemetry.ResultError
			step.Error = message
			step.ErrorKind = "run_cancelled"
			step.FinishedAt = now
			_, _ = r.store.UpdateStep(ctx, step)
		}
	}

	artifacts, _ := r.store.ListArtifacts(ctx, taskstate.ArtifactFilter{TaskID: task.ID, RunID: run.ID})
	for _, artifact := range artifacts {
		if artifact.Status == "streaming" {
			artifact.Status = "cancelled"
			_, _ = r.store.UpdateArtifact(ctx, artifact)
		}
	}

	task.Status = "cancelled"
	task.LatestRunID = run.ID
	task.LastError = message
	if task.StartedAt.IsZero() {
		task.StartedAt = run.StartedAt
	}
	task.FinishedAt = now
	task.UpdatedAt = now
	if requestID != "" {
		task.LatestRequestID = requestID
	}
	if traceID != "" {
		task.LatestTraceID = traceID
	}
	if _, err := r.store.UpdateTask(ctx, task); err != nil {
		return types.TaskRun{}, err
	}
	return run, nil
}

func (r *Runner) processQueue() {
	for job := range r.queue {
		r.processQueuedRun(job)
	}
}

func (r *Runner) processQueuedRun(job queuedRun) {
	defer r.unregisterJob(job.runID)
	if job.ctx.Err() != nil {
		return
	}

	task, found, err := r.store.GetTask(context.Background(), job.taskID)
	if err != nil || !found {
		return
	}
	run, found, err := r.store.GetRun(context.Background(), job.taskID, job.runID)
	if err != nil || !found {
		return
	}
	if run.Status != "queued" {
		return
	}

	requestID := strings.TrimSpace(run.RequestID)
	if requestID == "" {
		requestID = job.idgen("request")
	}
	trace := r.tracer.Start(requestID)
	defer trace.Finalize()

	ctx := telemetry.WithTraceIDs(job.ctx, trace.TraceID, trace.RootSpanID())
	now := time.Now().UTC()
	run.Status = "running"
	run.RequestID = requestID
	run.TraceID = trace.TraceID
	run.RootSpanID = trace.RootSpanID()
	if run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	run.LastError = ""
	run.FinishedAt = time.Time{}
	if _, err := r.store.UpdateRun(ctx, run); err != nil {
		return
	}

	task.Status = "running"
	task.LatestRunID = run.ID
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	task.UpdatedAt = now
	task.FinishedAt = time.Time{}
	task.LastError = ""
	task.RootTraceID = trace.TraceID
	task.LatestTraceID = trace.TraceID
	task.LatestRequestID = requestID
	if _, err := r.store.UpdateTask(ctx, task); err != nil {
		return
	}

	trace.Record("orchestrator.run.started", map[string]any{
		telemetry.AttrHecatePhase:       "orchestration",
		telemetry.AttrHecateResult:      telemetry.ResultSuccess,
		"hecate.task.id":                task.ID,
		"hecate.run.id":                 run.ID,
		"hecate.run.number":             run.Number,
		"hecate.run.status":             run.Status,
		telemetry.AttrGenAIRequestModel: run.Model,
	})

	if _, err := r.executeRun(ctx, trace, task, run, requestID, job.idgen); err != nil {
		finalStatus := "failed"
		lastError := err.Error()
		if job.ctx.Err() != nil {
			finalStatus = "cancelled"
			lastError = "run cancelled"
		}
		_ = r.finalizeFailedRun(ctx, trace, task, run, requestID, finalStatus, lastError)
	}
}

func (r *Runner) enqueueRun(taskID, runID string, idgen func(prefix string) string) error {
	ctx, cancel := context.WithCancel(context.Background())
	r.jobMu.Lock()
	r.jobs[runID] = cancel
	r.jobMu.Unlock()
	select {
	case r.queue <- queuedRun{ctx: ctx, taskID: taskID, runID: runID, idgen: idgen}:
		return nil
	default:
		cancel()
		r.unregisterJob(runID)
		return fmt.Errorf("run queue is full")
	}
}

func (r *Runner) unregisterJob(runID string) {
	r.jobMu.Lock()
	defer r.jobMu.Unlock()
	delete(r.jobs, runID)
}

func (r *Runner) executeRun(ctx context.Context, trace *profiler.Trace, task types.Task, run types.TaskRun, requestID string, idgen func(prefix string) string) (*StartTaskResult, error) {
	executor := r.executorForTask(task)
	execution, err := executor.Execute(ctx, ExecutionSpec{
		Task:           taskForRun(task, run),
		Run:            run,
		RequestID:      requestID,
		TraceID:        trace.TraceID,
		RootSpanID:     trace.RootSpanID(),
		StartedAt:      time.Now().UTC(),
		NewID:          idgen,
		UpsertStep:     func(step types.TaskStep) error { return r.upsertStep(ctx, step) },
		UpsertArtifact: func(artifact types.TaskArtifact) error { return r.upsertArtifact(ctx, artifact) },
	})
	if err != nil {
		trace.Record("orchestrator.run.failed", map[string]any{
			telemetry.AttrHecatePhase:     "orchestration",
			telemetry.AttrHecateResult:    telemetry.ResultError,
			telemetry.AttrHecateErrorKind: "executor_failed",
			telemetry.AttrErrorType:       "executor_failed",
			telemetry.AttrErrorMessage:    err.Error(),
			"hecate.task.id":              task.ID,
			"hecate.run.id":               run.ID,
		})
		return nil, err
	}

	persistedSteps := make([]types.TaskStep, 0, len(execution.Steps))
	for _, step := range execution.Steps {
		eventName := "orchestrator.step.completed"
		if step.Status == "failed" || step.Status == "cancelled" || step.Result == telemetry.ResultError {
			eventName = "orchestrator.step.failed"
		}
		trace.Record(eventName, map[string]any{
			telemetry.AttrHecatePhase:  firstNonEmpty(step.Phase, "execution"),
			telemetry.AttrHecateResult: firstNonEmpty(step.Result, telemetry.ResultSuccess),
			"hecate.task.id":           task.ID,
			"hecate.run.id":            run.ID,
			"hecate.step.id":           step.ID,
			"hecate.step.kind":         step.Kind,
			"hecate.step.index":        step.Index,
			"hecate.step.tool_name":    step.ToolName,
		})
		step.SpanID = spanIDByName(trace, "orchestrator.step")
		step.ParentSpanID = trace.RootSpanID()
		if err := r.upsertStep(ctx, step); err != nil {
			return nil, err
		}
		persistedSteps = append(persistedSteps, step)
	}

	persistedArtifacts := make([]types.TaskArtifact, 0, len(execution.Artifacts))
	for _, artifact := range execution.Artifacts {
		trace.Record("orchestrator.artifact.created", map[string]any{
			telemetry.AttrHecatePhase:    "artifact",
			telemetry.AttrHecateResult:   telemetry.ResultSuccess,
			"hecate.task.id":             task.ID,
			"hecate.run.id":              run.ID,
			"hecate.step.id":             artifact.StepID,
			"hecate.artifact.id":         artifact.ID,
			"hecate.artifact.kind":       artifact.Kind,
			"hecate.artifact.size_bytes": artifact.SizeBytes,
		})
		artifact.SpanID = spanIDByName(trace, "orchestrator.artifact")
		if err := r.upsertArtifact(ctx, artifact); err != nil {
			return nil, err
		}
		persistedArtifacts = append(persistedArtifacts, artifact)
	}

	resultKind := telemetry.ResultSuccess
	if execution.Status == "failed" || execution.Status == "cancelled" {
		resultKind = telemetry.ResultError
	}
	trace.Record("orchestrator.run.finished", map[string]any{
		telemetry.AttrHecatePhase:  "orchestration",
		telemetry.AttrHecateResult: resultKind,
		"hecate.task.id":           task.ID,
		"hecate.run.id":            run.ID,
	})
	trace.Record("orchestrator.task.finished", map[string]any{
		telemetry.AttrHecatePhase:  "orchestration",
		telemetry.AttrHecateResult: resultKind,
		"hecate.task.id":           task.ID,
	})

	finishedAt := time.Now().UTC()
	run.Status = firstNonEmpty(execution.Status, "completed")
	run.StepCount = len(persistedSteps)
	run.ArtifactCount = len(persistedArtifacts)
	run.FinishedAt = finishedAt
	run.LastError = execution.LastError
	run.OtelStatusCode = firstNonEmpty(execution.OtelStatusCode, "ok")
	run.OtelStatusMessage = execution.OtelStatusMessage
	if _, err := r.store.UpdateRun(ctx, run); err != nil {
		return nil, err
	}

	task.LatestRunID = run.ID
	task.Status = run.Status
	if task.StartedAt.IsZero() {
		task.StartedAt = run.StartedAt
	}
	task.FinishedAt = finishedAt
	task.UpdatedAt = finishedAt
	task.RootTraceID = trace.TraceID
	task.LatestTraceID = trace.TraceID
	task.LatestRequestID = requestID
	task.LastError = execution.LastError
	if _, err := r.store.UpdateTask(ctx, task); err != nil {
		return nil, err
	}

	return &StartTaskResult{
		Task:      task,
		Run:       run,
		Steps:     persistedSteps,
		Artifacts: persistedArtifacts,
		TraceID:   trace.TraceID,
		SpanID:    trace.RootSpanID(),
	}, nil
}

func (r *Runner) finalizeFailedRun(ctx context.Context, trace *profiler.Trace, task types.Task, run types.TaskRun, requestID, status, message string) error {
	now := time.Now().UTC()
	run.Status = status
	run.LastError = message
	run.FinishedAt = now
	run.OtelStatusCode = "error"
	run.OtelStatusMessage = message
	if _, err := r.store.UpdateRun(ctx, run); err != nil {
		return err
	}
	task.Status = status
	task.LatestRunID = run.ID
	task.LastError = message
	task.FinishedAt = now
	task.UpdatedAt = now
	task.LatestTraceID = trace.TraceID
	task.LatestRequestID = requestID
	_, err := r.store.UpdateTask(ctx, task)
	return err
}

func (r *Runner) upsertStep(ctx context.Context, step types.TaskStep) error {
	if existing, found, err := r.store.GetStep(ctx, step.RunID, step.ID); err != nil {
		return err
	} else if found {
		step.SpanID = firstNonEmpty(step.SpanID, existing.SpanID)
		step.ParentSpanID = firstNonEmpty(step.ParentSpanID, existing.ParentSpanID)
		_, err = r.store.UpdateStep(ctx, step)
		return err
	}
	_, err := r.store.AppendStep(ctx, step)
	return err
}

func (r *Runner) upsertArtifact(ctx context.Context, artifact types.TaskArtifact) error {
	if existing, found, err := r.store.GetArtifact(ctx, artifact.TaskID, artifact.ID); err != nil {
		return err
	} else if found {
		artifact.SpanID = firstNonEmpty(artifact.SpanID, existing.SpanID)
		_, err = r.store.UpdateArtifact(ctx, artifact)
		return err
	}
	_, err := r.store.CreateArtifact(ctx, artifact)
	return err
}

func taskForRun(task types.Task, run types.TaskRun) types.Task {
	executionTask := task
	if strings.TrimSpace(run.WorkspacePath) != "" {
		executionTask.WorkingDirectory = run.WorkspacePath
		executionTask.SandboxAllowedRoot = run.WorkspacePath
	}
	return executionTask
}

func (r *Runner) approvalRequiredForTask(task types.Task) bool {
	return task.ExecutionKind == "shell" && strings.TrimSpace(task.ShellCommand) != ""
}

func (r *Runner) createApprovalForTask(ctx context.Context, trace *profiler.Trace, task types.Task, run types.TaskRun, requestID string, createdAt time.Time, idgen func(prefix string) string) (types.TaskApproval, error) {
	approval := types.TaskApproval{
		ID:          idgen("approval"),
		TaskID:      task.ID,
		RunID:       run.ID,
		Kind:        "shell_command",
		Status:      "pending",
		Reason:      "Shell commands require approval before execution.",
		RequestedBy: task.User,
		CreatedAt:   createdAt,
		RequestID:   requestID,
		TraceID:     trace.TraceID,
	}
	trace.Record("orchestrator.approval.requested", map[string]any{
		telemetry.AttrHecatePhase:  "approval",
		telemetry.AttrHecateResult: telemetry.ResultSuccess,
		"hecate.task.id":           task.ID,
		"hecate.run.id":            run.ID,
		"hecate.approval.id":       approval.ID,
		"hecate.approval.kind":     approval.Kind,
		"hecate.shell.command":     task.ShellCommand,
	})
	approval.SpanID = spanIDByName(trace, "orchestrator.approval")
	approval, err := r.store.CreateApproval(ctx, approval)
	if err != nil {
		trace.Record("orchestrator.approval.failed", map[string]any{
			telemetry.AttrHecatePhase:     "approval",
			telemetry.AttrHecateResult:    telemetry.ResultError,
			telemetry.AttrHecateErrorKind: "approval_create_failed",
			telemetry.AttrErrorType:       "approval_create_failed",
			telemetry.AttrErrorMessage:    err.Error(),
			"hecate.task.id":              task.ID,
			"hecate.run.id":               run.ID,
			"hecate.approval.id":          approval.ID,
		})
		return types.TaskApproval{}, err
	}
	return approval, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func spanIDByName(trace *profiler.Trace, name string) string {
	if trace == nil {
		return ""
	}
	for _, span := range trace.Spans() {
		if span.Name == name {
			return span.SpanID
		}
	}
	return ""
}

func (r *Runner) executorForTask(task types.Task) Executor {
	if task.ExecutionKind == "shell" && strings.TrimSpace(task.ShellCommand) != "" && r.shell != nil {
		return r.shell
	}
	if task.ExecutionKind == "file" && strings.TrimSpace(task.FilePath) != "" && r.file != nil {
		return r.file
	}
	if task.ExecutionKind == "git" && strings.TrimSpace(task.GitCommand) != "" && r.git != nil {
		return r.git
	}
	return r.exec
}
