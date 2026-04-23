package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/profiler"
	"github.com/hecate/agent-runtime/internal/taskstate"
	"github.com/hecate/agent-runtime/internal/telemetry"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Config struct {
	DefaultModel string
}

type Runner struct {
	logger *slog.Logger
	store  taskstate.Store
	tracer profiler.Tracer
	exec   Executor
	shell  Executor
	config Config
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
	return &Runner{
		logger: logger,
		store:  store,
		tracer: tracer,
		exec:   NewStubExecutor(),
		shell:  NewShellExecutor(),
		config: cfg,
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

	executor := r.executorForTask(task)
	execution, err := executor.Execute(ctx, ExecutionSpec{
		Task:       task,
		Run:        run,
		RequestID:  requestID,
		TraceID:    trace.TraceID,
		RootSpanID: trace.RootSpanID(),
		StartedAt:  now,
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

	trace.Record("orchestrator.run.finished", map[string]any{
		telemetry.AttrHecatePhase:  "orchestration",
		telemetry.AttrHecateResult: telemetry.ResultSuccess,
		"hecate.task.id":           task.ID,
		"hecate.run.id":            run.ID,
	})
	trace.Record("orchestrator.task.finished", map[string]any{
		telemetry.AttrHecatePhase:  "orchestration",
		telemetry.AttrHecateResult: telemetry.ResultSuccess,
		"hecate.task.id":           task.ID,
	})

	run.Status = firstNonEmpty(execution.Status, "completed")
	run.StepCount = len(persistedSteps)
	run.ArtifactCount = len(persistedArtifacts)
	run.FinishedAt = now
	run.LastError = execution.LastError
	run.OtelStatusCode = firstNonEmpty(execution.OtelStatusCode, "ok")
	run.OtelStatusMessage = execution.OtelStatusMessage
	if _, err := r.store.UpdateRun(ctx, run); err != nil {
		return nil, err
	}

	task.LatestRunID = run.ID
	task.Status = run.Status
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	task.FinishedAt = now
	task.UpdatedAt = now
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
	return r.exec
}
