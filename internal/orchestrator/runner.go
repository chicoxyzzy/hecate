package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
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
	DefaultModel           string
	ApprovalPolicies       []string
	QueueBackend           string
	QueueWorkers           int
	QueueBuffer            int
	QueueLeaseSeconds      int
	MaxConcurrentPerTenant int
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
	queue      RunQueue
	queueLease time.Duration
	workerID   string
	jobMu      sync.Mutex
	jobs       map[string]context.CancelFunc
	policies   map[string]struct{}
}

type StartTaskResult struct {
	Task      types.Task
	Run       types.TaskRun
	Steps     []types.TaskStep
	Artifacts []types.TaskArtifact
	TraceID   string
	SpanID    string
}

type RuntimeStats struct {
	CheckedAt               time.Time
	QueueDepth              int
	QueueCapacity           int
	QueueBackend            string
	WorkerCount             int
	InFlightJobs            int
	QueuedRuns              int
	RunningRuns             int
	AwaitingApprovalRuns    int
	OldestQueuedAgeSeconds  int64
	OldestRunningAgeSeconds int64
	StoreBackend            string
}

func NewRunner(logger *slog.Logger, store taskstate.Store, tracer profiler.Tracer, cfg Config) *Runner {
	if tracer == nil {
		tracer = profiler.NewInMemoryTracer(nil)
	}
	worker := sandbox.NewWorkerExecutor()
	queueBuffer := cfg.QueueBuffer
	if queueBuffer <= 0 {
		queueBuffer = 128
	}
	queueLease := time.Duration(cfg.QueueLeaseSeconds) * time.Second
	if queueLease <= 0 {
		queueLease = 30 * time.Second
	}
	queue := NewMemoryRunQueue(queueBuffer, queueLease)
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
		queue:      queue,
		queueLease: queueLease,
		workerID:   defaultWorkerID(),
		jobs:       make(map[string]context.CancelFunc),
		policies:   make(map[string]struct{}),
	}
	for _, policy := range cfg.ApprovalPolicies {
		policy = strings.TrimSpace(policy)
		if policy == "" {
			continue
		}
		runner.policies[policy] = struct{}{}
	}
	if len(runner.policies) == 0 {
		runner.policies["shell_exec"] = struct{}{}
	}
	workers := cfg.QueueWorkers
	if workers <= 0 {
		workers = 1
	}
	for worker := 0; worker < workers; worker++ {
		go runner.processQueue()
	}
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

func (r *Runner) RuntimeStats(ctx context.Context) (RuntimeStats, error) {
	queueDepth := 0
	queueCapacity := 0
	if r.queue != nil {
		if depth, err := r.queue.Depth(ctx); err == nil {
			queueDepth = depth
		}
		queueCapacity = r.queue.Capacity()
	}
	stats := RuntimeStats{
		CheckedAt:     time.Now().UTC(),
		QueueDepth:    queueDepth,
		QueueCapacity: queueCapacity,
		WorkerCount:   maxInt(r.config.QueueWorkers, 1),
	}
	if r.queue != nil {
		stats.QueueBackend = r.queue.Backend()
	}
	r.jobMu.Lock()
	stats.InFlightJobs = len(r.jobs)
	r.jobMu.Unlock()
	if r.store == nil {
		return stats, nil
	}
	stats.StoreBackend = r.store.Backend()
	now := time.Now().UTC()

	queuedRuns, err := r.store.ListRunsByFilter(ctx, taskstate.RunFilter{Statuses: []string{"queued"}, Limit: 2000})
	if err != nil {
		return RuntimeStats{}, err
	}
	stats.QueuedRuns = len(queuedRuns)
	oldestQueued := findOldestRunStart(queuedRuns)
	if !oldestQueued.IsZero() {
		stats.OldestQueuedAgeSeconds = int64(now.Sub(oldestQueued).Seconds())
	}

	runningRuns, err := r.store.ListRunsByFilter(ctx, taskstate.RunFilter{Statuses: []string{"running"}, Limit: 2000})
	if err != nil {
		return RuntimeStats{}, err
	}
	stats.RunningRuns = len(runningRuns)
	oldestRunning := findOldestRunStart(runningRuns)
	if !oldestRunning.IsZero() {
		stats.OldestRunningAgeSeconds = int64(now.Sub(oldestRunning).Seconds())
	}

	awaitingApprovals, err := r.store.ListRunsByFilter(ctx, taskstate.RunFilter{Statuses: []string{"awaiting_approval"}, Limit: 2000})
	if err != nil {
		return RuntimeStats{}, err
	}
	stats.AwaitingApprovalRuns = len(awaitingApprovals)
	return stats, nil
}

func (r *Runner) SetQueue(queue RunQueue) {
	if queue == nil {
		return
	}
	r.queue = queue
}

func (r *Runner) ReconcilePendingRuns(ctx context.Context) error {
	if r.store == nil {
		return nil
	}
	runs, err := r.store.ListRunsByFilter(ctx, taskstate.RunFilter{
		Statuses: []string{"queued", "running"},
		Limit:    500,
	})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, run := range runs {
		task, found, err := r.store.GetTask(ctx, run.TaskID)
		if err != nil || !found {
			continue
		}
		run.Status = "cancelled"
		run.LastError = "run interrupted by runtime restart"
		run.FinishedAt = now
		run.OtelStatusCode = "error"
		run.OtelStatusMessage = run.LastError
		if _, err := r.store.UpdateRun(ctx, run); err != nil {
			continue
		}
		task.Status = "cancelled"
		task.LatestRunID = run.ID
		task.LastError = run.LastError
		task.UpdatedAt = now
		task.FinishedAt = now
		_, _ = r.store.UpdateTask(ctx, task)
		_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.reconciled_restart", "", "", nil)
	}
	return nil
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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.created", requestID, trace.TraceID, nil)

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
		_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.awaiting_approval", requestID, trace.TraceID, nil)
		task.Status = "awaiting_approval"
	} else if err := r.enqueueRun(task.ID, run.ID); err != nil {
		return nil, err
	} else {
		_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.queued", requestID, trace.TraceID, nil)
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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.finished", requestID, trace.TraceID, map[string]any{"status": run.Status})
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.queued", requestID, trace.TraceID, map[string]any{"resume": true})

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

	if err := r.enqueueRun(task.ID, run.ID); err != nil {
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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.cancelled", requestID, traceID, map[string]any{"reason": message})

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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "task.updated", requestID, traceID, nil)
	return run, nil
}

func (r *Runner) processQueue() {
	for {
		if r.queue == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		claim, ok, err := r.queue.Claim(context.Background(), r.workerID, 2*time.Second)
		if err != nil {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		if !ok {
			continue
		}
		r.processQueuedRun(claim)
	}
}

func (r *Runner) processQueuedRun(claim QueueClaim) {
	task, found, err := r.store.GetTask(context.Background(), claim.Job.TaskID)
	if err != nil || !found {
		_ = r.queue.Ack(context.Background(), claim.ClaimID)
		return
	}
	run, found, err := r.store.GetRun(context.Background(), claim.Job.TaskID, claim.Job.RunID)
	if err != nil || !found {
		_ = r.queue.Ack(context.Background(), claim.ClaimID)
		return
	}
	if run.Status != "queued" {
		_ = r.queue.Ack(context.Background(), claim.ClaimID)
		return
	}
	allowed, limitErr := r.canStartRunForTenant(context.Background(), task, run.ID)
	if limitErr != nil {
		_ = r.queue.Nack(context.Background(), claim.ClaimID, limitErr.Error())
		return
	}
	if !allowed {
		_, _ = r.emitRunEvent(context.Background(), task.ID, run.ID, "run.throttled_tenant_concurrency", run.RequestID, run.TraceID, map[string]any{
			"tenant": task.Tenant,
			"limit":  r.config.MaxConcurrentPerTenant,
		})
		_ = r.queue.Nack(context.Background(), claim.ClaimID, "tenant concurrency limit")
		return
	}

	requestID := strings.TrimSpace(run.RequestID)
	if requestID == "" {
		requestID = defaultResourceID("request")
	}
	trace := r.tracer.Start(requestID)
	defer trace.Finalize()

	jobCtx, jobCancel := context.WithCancel(context.Background())
	r.registerJob(run.ID, jobCancel)
	defer r.unregisterJob(run.ID)
	defer jobCancel()

	stopHeartbeat := make(chan struct{})
	go r.heartbeatClaim(claim.ClaimID, stopHeartbeat)
	defer close(stopHeartbeat)

	ctx := telemetry.WithTraceIDs(jobCtx, trace.TraceID, trace.RootSpanID())
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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run.running", requestID, trace.TraceID, nil)

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

	if _, err := r.executeRun(ctx, trace, task, run, requestID); err != nil {
		finalStatus := "failed"
		lastError := err.Error()
		if jobCtx.Err() != nil {
			finalStatus = "cancelled"
			lastError = "run cancelled"
		}
		_ = r.finalizeFailedRun(ctx, trace, task, run, requestID, finalStatus, lastError)
	}
	_ = r.queue.Ack(context.Background(), claim.ClaimID)
}

func (r *Runner) canStartRunForTenant(ctx context.Context, task types.Task, runID string) (bool, error) {
	if r.config.MaxConcurrentPerTenant <= 0 {
		return true, nil
	}
	tenant := strings.TrimSpace(task.Tenant)
	if tenant == "" {
		return true, nil
	}
	runs, err := r.store.ListRunsByFilter(ctx, taskstate.RunFilter{
		Statuses: []string{"running"},
		Limit:    4000,
	})
	if err != nil {
		return false, err
	}
	count := 0
	for _, item := range runs {
		if item.ID == runID {
			continue
		}
		candidateTask, found, getErr := r.store.GetTask(ctx, item.TaskID)
		if getErr != nil || !found {
			continue
		}
		if strings.TrimSpace(candidateTask.Tenant) == tenant {
			count++
			if count >= r.config.MaxConcurrentPerTenant {
				return false, nil
			}
		}
	}
	return true, nil
}

func (r *Runner) enqueueRun(taskID, runID string) error {
	if r.queue == nil {
		return fmt.Errorf("run queue is not configured")
	}
	return r.queue.Enqueue(context.Background(), QueueJob{TaskID: taskID, RunID: runID})
}

func (r *Runner) registerJob(runID string, cancel context.CancelFunc) {
	r.jobMu.Lock()
	defer r.jobMu.Unlock()
	r.jobs[runID] = cancel
}

func (r *Runner) unregisterJob(runID string) {
	r.jobMu.Lock()
	defer r.jobMu.Unlock()
	delete(r.jobs, runID)
}

func (r *Runner) executeRun(ctx context.Context, trace *profiler.Trace, task types.Task, run types.TaskRun, requestID string) (*StartTaskResult, error) {
	executor := r.executorForTask(task)
	execution, err := executor.Execute(ctx, ExecutionSpec{
		Task:           taskForRun(task, run),
		Run:            run,
		RequestID:      requestID,
		TraceID:        trace.TraceID,
		RootSpanID:     trace.RootSpanID(),
		StartedAt:      time.Now().UTC(),
		NewID:          defaultResourceID,
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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "run."+status, requestID, trace.TraceID, map[string]any{"error": message})
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
		if err == nil {
			_, _ = r.emitRunEvent(ctx, step.TaskID, step.RunID, "step.updated", step.RequestID, step.TraceID, map[string]any{"step_id": step.ID})
		}
		return err
	}
	_, err := r.store.AppendStep(ctx, step)
	if err == nil {
		_, _ = r.emitRunEvent(ctx, step.TaskID, step.RunID, "step.created", step.RequestID, step.TraceID, map[string]any{"step_id": step.ID})
	}
	return err
}

func (r *Runner) upsertArtifact(ctx context.Context, artifact types.TaskArtifact) error {
	if existing, found, err := r.store.GetArtifact(ctx, artifact.TaskID, artifact.ID); err != nil {
		return err
	} else if found {
		artifact.SpanID = firstNonEmpty(artifact.SpanID, existing.SpanID)
		_, err = r.store.UpdateArtifact(ctx, artifact)
		if err == nil {
			_, _ = r.emitRunEvent(ctx, artifact.TaskID, artifact.RunID, "artifact.updated", artifact.RequestID, artifact.TraceID, map[string]any{"artifact_id": artifact.ID})
		}
		return err
	}
	_, err := r.store.CreateArtifact(ctx, artifact)
	if err == nil {
		_, _ = r.emitRunEvent(ctx, artifact.TaskID, artifact.RunID, "artifact.created", artifact.RequestID, artifact.TraceID, map[string]any{"artifact_id": artifact.ID})
	}
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
	_, reason := r.approvalSpecForTask(task)
	return reason != ""
}

func (r *Runner) approvalSpecForTask(task types.Task) (kind string, reason string) {
	if task.ExecutionKind == "shell" && strings.TrimSpace(task.ShellCommand) != "" {
		if _, ok := r.policies["shell_exec"]; ok {
			return "shell_command", "Shell execution requires approval before execution."
		}
	}
	if task.ExecutionKind == "git" && strings.TrimSpace(task.GitCommand) != "" {
		if _, ok := r.policies["git_exec"]; ok {
			return "git_exec", "Git execution requires approval before execution."
		}
	}
	if task.ExecutionKind == "file" && strings.TrimSpace(task.FilePath) != "" {
		if _, ok := r.policies["file_write"]; ok {
			return "file_write", "File writes require approval before execution."
		}
	}
	if task.SandboxNetwork {
		if _, ok := r.policies["network_egress"]; ok {
			return "network_egress", "Network-enabled tasks require approval before execution."
		}
	}
	return "", ""
}

func (r *Runner) createApprovalForTask(ctx context.Context, trace *profiler.Trace, task types.Task, run types.TaskRun, requestID string, createdAt time.Time, idgen func(prefix string) string) (types.TaskApproval, error) {
	kind, reason := r.approvalSpecForTask(task)
	approval := types.TaskApproval{
		ID:          idgen("approval"),
		TaskID:      task.ID,
		RunID:       run.ID,
		Kind:        kind,
		Status:      "pending",
		Reason:      reason,
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
	_, _ = r.emitRunEvent(ctx, task.ID, run.ID, "approval.requested", requestID, trace.TraceID, map[string]any{
		"approval_id": approval.ID,
		"kind":        approval.Kind,
		"status":      approval.Status,
	})
	return approval, nil
}

func (r *Runner) emitRunEvent(ctx context.Context, taskID, runID, eventType, requestID, traceID string, extra map[string]any) (types.TaskRunEvent, error) {
	if r.store == nil || runID == "" {
		return types.TaskRunEvent{}, nil
	}
	run, _, err := r.store.GetRun(ctx, taskID, runID)
	if err != nil {
		return types.TaskRunEvent{}, err
	}
	steps, _ := r.store.ListSteps(ctx, runID)
	artifacts, _ := r.store.ListArtifacts(ctx, taskstate.ArtifactFilter{TaskID: taskID, RunID: runID})
	data := map[string]any{
		"run":       run,
		"steps":     steps,
		"artifacts": artifacts,
	}
	for key, value := range extra {
		data[key] = value
	}
	return r.store.AppendRunEvent(ctx, types.TaskRunEvent{
		TaskID:    taskID,
		RunID:     runID,
		EventType: eventType,
		Data:      data,
		RequestID: requestID,
		TraceID:   traceID,
		CreatedAt: time.Now().UTC(),
	})
}

func (r *Runner) heartbeatClaim(claimID string, stop <-chan struct{}) {
	if r.queue == nil || claimID == "" {
		return
	}
	interval := r.queueLease / 2
	if interval <= 0 {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			_ = r.queue.ExtendLease(context.Background(), claimID, r.queueLease)
		}
	}
}

func defaultWorkerID() string {
	hostname, _ := os.Hostname()
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "worker"
	}
	return hostname + "-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

func defaultResourceID(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "id"
	}
	return prefix + "_" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
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

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func findOldestRunStart(runs []types.TaskRun) time.Time {
	var oldest time.Time
	for _, run := range runs {
		if run.StartedAt.IsZero() {
			continue
		}
		if oldest.IsZero() || run.StartedAt.Before(oldest) {
			oldest = run.StartedAt
		}
	}
	return oldest
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
