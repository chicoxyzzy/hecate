package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	return &Runner{
		logger:     logger,
		store:      store,
		tracer:     tracer,
		exec:       NewStubExecutor(),
		shell:      NewShellExecutor(worker),
		file:       NewFileExecutor(worker),
		git:        NewGitExecutor(worker),
		workspaces: NewWorkspaceManager(""),
		config:     cfg,
	}
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
		Status:       "running",
		Orchestrator: "builtin",
		Model:        firstNonEmpty(task.RequestedModel, r.config.DefaultModel),
		Provider:     task.RequestedProvider,
		WorkspaceID:  "workspace_" + task.ID,
		WorkspacePath: func() string {
			if task.Repo == "" {
				return ""
			}
			return task.Repo
		}(),
		StartedAt:  now,
		RequestID:  requestID,
		TraceID:    trace.TraceID,
		RootSpanID: trace.RootSpanID(),
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

	if r.approvalRequiredForTask(task) {
		if _, err := r.createApprovalForTask(ctx, trace, task, run, requestID, now, idgen); err != nil {
			return nil, err
		}
		run.ApprovalCount = 1
		run, err = r.store.UpdateRun(ctx, run)
		if err != nil {
			return nil, err
		}

		task.LatestRunID = run.ID
		task.Status = "awaiting_approval"
		if task.StartedAt.IsZero() {
			task.StartedAt = now
		}
		task.FinishedAt = time.Time{}
		task.UpdatedAt = now
		task.RootTraceID = trace.TraceID
		task.LatestTraceID = trace.TraceID
		task.LatestRequestID = requestID
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

	return r.executeRun(ctx, trace, task, run, requestID, idgen)
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

	return r.executeRun(ctx, trace, task, run, requestID, idgen)
}

func (r *Runner) executeRun(ctx context.Context, trace *profiler.Trace, task types.Task, run types.TaskRun, requestID string, idgen func(prefix string) string) (*StartTaskResult, error) {
	executor := r.executorForTask(task)
	execution, err := executor.Execute(ctx, ExecutionSpec{
		Task:       taskForRun(task, run),
		Run:        run,
		RequestID:  requestID,
		TraceID:    trace.TraceID,
		RootSpanID: trace.RootSpanID(),
		StartedAt:  time.Now().UTC(),
		NewID:      idgen,
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
		if step.Status == "failed" || step.Result == telemetry.ResultError {
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
		step, err = r.store.AppendStep(ctx, step)
		if err != nil {
			trace.Record("orchestrator.step.failed", map[string]any{
				telemetry.AttrHecatePhase:     firstNonEmpty(step.Phase, "execution"),
				telemetry.AttrHecateResult:    telemetry.ResultError,
				telemetry.AttrHecateErrorKind: "step_create_failed",
				telemetry.AttrErrorType:       "step_create_failed",
				telemetry.AttrErrorMessage:    err.Error(),
				"hecate.task.id":              task.ID,
				"hecate.run.id":               run.ID,
				"hecate.step.id":              step.ID,
			})
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
		artifact, err = r.store.CreateArtifact(ctx, artifact)
		if err != nil {
			trace.Record("orchestrator.artifact.failed", map[string]any{
				telemetry.AttrHecatePhase:     "artifact",
				telemetry.AttrHecateResult:    telemetry.ResultError,
				telemetry.AttrHecateErrorKind: "artifact_create_failed",
				telemetry.AttrErrorType:       "artifact_create_failed",
				telemetry.AttrErrorMessage:    err.Error(),
				"hecate.task.id":              task.ID,
				"hecate.run.id":               run.ID,
				"hecate.artifact.id":          artifact.ID,
			})
			return nil, err
		}
		persistedArtifacts = append(persistedArtifacts, artifact)
	}

	resultKind := telemetry.ResultSuccess
	if execution.Status == "failed" {
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
