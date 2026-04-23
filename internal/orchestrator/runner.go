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
		config: cfg,
	}
}

func (r *Runner) StartTask(ctx context.Context, task types.Task, idgen func(prefix string) string) (*StartTaskResult, error) {
	if r.store == nil {
		return nil, fmt.Errorf("task store is not configured")
	}
	if idgen == nil {
		return nil, fmt.Errorf("resource id generator is required")
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

	step := types.TaskStep{
		ID:       idgen("step"),
		TaskID:   task.ID,
		RunID:    run.ID,
		Index:    1,
		Kind:     "model",
		Title:    "Stub planning step",
		Status:   "completed",
		Phase:    "planning",
		Result:   telemetry.ResultSuccess,
		ToolName: "builtin.stub_planner",
		Input: map[string]any{
			"title":  task.Title,
			"prompt": task.Prompt,
		},
		OutputSummary: map[string]any{
			"summary":     "Stub orchestrator generated a first planning step.",
			"next_action": "review generated summary artifact",
		},
		StartedAt:  now,
		FinishedAt: now,
		RequestID:  requestID,
		TraceID:    trace.TraceID,
	}
	trace.Record("orchestrator.step.completed", map[string]any{
		telemetry.AttrHecatePhase:  step.Phase,
		telemetry.AttrHecateResult: step.Result,
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
			telemetry.AttrHecatePhase:     "planning",
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

	summary := fmt.Sprintf("Stub run %d for task %q created a first planning step and is ready for a real executor.", run.Number, task.Title)
	artifact := types.TaskArtifact{
		ID:          idgen("artifact"),
		TaskID:      task.ID,
		RunID:       run.ID,
		StepID:      step.ID,
		Kind:        "summary",
		Name:        "run-summary.txt",
		Description: "Stub run summary artifact",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: summary,
		SizeBytes:   int64(len(summary)),
		Status:      "ready",
		CreatedAt:   now,
		RequestID:   requestID,
		TraceID:     trace.TraceID,
	}
	trace.Record("orchestrator.artifact.created", map[string]any{
		telemetry.AttrHecatePhase:    "artifact",
		telemetry.AttrHecateResult:   telemetry.ResultSuccess,
		"hecate.task.id":             task.ID,
		"hecate.run.id":              run.ID,
		"hecate.step.id":             step.ID,
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

	run.Status = "completed"
	run.StepCount = 1
	run.ArtifactCount = 1
	run.FinishedAt = now
	run.OtelStatusCode = "ok"
	if _, err := r.store.UpdateRun(ctx, run); err != nil {
		return nil, err
	}

	task.LatestRunID = run.ID
	task.Status = "completed"
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
		Steps:     []types.TaskStep{step},
		Artifacts: []types.TaskArtifact{artifact},
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
