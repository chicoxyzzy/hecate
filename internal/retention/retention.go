package retention

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/profiler"
)

const (
	SubsystemTraces        = "trace_snapshots"
	SubsystemBudgetEvents  = "budget_events"
	SubsystemAuditEvents   = "audit_events"
	SubsystemExactCache    = "exact_cache"
	SubsystemSemanticCache = "semantic_cache"
)

type TracePruner interface {
	Prune(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
}

type BudgetEventPruner interface {
	PruneEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
}

type AuditEventPruner interface {
	PruneAuditEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
}

type CachePruner interface {
	Prune(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
}

type RunRequest struct {
	Trigger    string
	Subsystems []string
	Actor      string
	RequestID  string
}

type SubsystemResult struct {
	Name     string        `json:"name"`
	Deleted  int           `json:"deleted"`
	MaxAge   time.Duration `json:"-"`
	MaxCount int           `json:"max_count"`
	Error    string        `json:"error,omitempty"`
	Skipped  bool          `json:"skipped,omitempty"`
}

type RunResult struct {
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
	Trigger    string            `json:"trigger"`
	Results    []SubsystemResult `json:"results"`
}

type Manager struct {
	logger   *slog.Logger
	cfg      config.RetentionConfig
	tracer   profiler.Tracer
	traces   TracePruner
	budgets  BudgetEventPruner
	audit    AuditEventPruner
	exact    CachePruner
	semantic CachePruner
	history  HistoryStore
}

func NewManager(
	logger *slog.Logger,
	cfg config.RetentionConfig,
	tracer profiler.Tracer,
	traces TracePruner,
	budgets BudgetEventPruner,
	audit AuditEventPruner,
	exact CachePruner,
	semantic CachePruner,
	history HistoryStore,
) *Manager {
	return &Manager{
		logger:   logger,
		cfg:      cfg,
		tracer:   tracer,
		traces:   traces,
		budgets:  budgets,
		audit:    audit,
		exact:    exact,
		semantic: semantic,
		history:  history,
	}
}

func (m *Manager) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

func (m *Manager) Run(ctx context.Context, req RunRequest) RunResult {
	startedAt := time.Now().UTC()
	trigger := req.Trigger
	if trigger == "" {
		trigger = "manual"
	}

	traceRequestID := fmt.Sprintf("retention:%s:%d", trigger, startedAt.UnixNano())
	trace := m.tracer.Start(traceRequestID)
	defer trace.Finalize()
	trace.Record("retention.run.started", map[string]any{
		"retention.trigger": trigger,
	})

	results := make([]SubsystemResult, 0, 5)
	runSubsystem := func(name string, policy config.RetentionPolicy, pruner CachePruner) {
		result := SubsystemResult{
			Name:     name,
			MaxAge:   policy.MaxAge,
			MaxCount: policy.MaxCount,
		}
		if !shouldRun(req.Subsystems, name) {
			result.Skipped = true
			results = append(results, result)
			return
		}
		if pruner == nil {
			result.Skipped = true
			results = append(results, result)
			return
		}
		deleted, err := pruner.Prune(ctx, policy.MaxAge, policy.MaxCount)
		result.Deleted = deleted
		if err != nil {
			result.Error = err.Error()
			trace.Record("retention.subsystem.failed", map[string]any{
				"retention.subsystem": name,
				"error.message":       err.Error(),
			})
		} else {
			trace.Record("retention.subsystem.finished", map[string]any{
				"retention.subsystem": name,
				"retention.deleted":   deleted,
			})
			m.logger.Info("retention subsystem finished",
				slog.String("subsystem", name),
				slog.Int("deleted", deleted),
				slog.Duration("max_age", policy.MaxAge),
				slog.Int("max_count", policy.MaxCount),
				slog.String("trigger", trigger),
			)
		}
		results = append(results, result)
	}

	runTraceSubsystem := func() {
		result := SubsystemResult{
			Name:     SubsystemTraces,
			MaxAge:   m.cfg.TraceSnapshots.MaxAge,
			MaxCount: m.cfg.TraceSnapshots.MaxCount,
		}
		if !shouldRun(req.Subsystems, result.Name) || m.traces == nil {
			result.Skipped = true
			results = append(results, result)
			return
		}
		deleted, err := m.traces.Prune(ctx, m.cfg.TraceSnapshots.MaxAge, m.cfg.TraceSnapshots.MaxCount)
		result.Deleted = deleted
		if err != nil {
			result.Error = err.Error()
			trace.Record("retention.subsystem.failed", map[string]any{
				"retention.subsystem": result.Name,
				"error.message":       err.Error(),
			})
		} else {
			trace.Record("retention.subsystem.finished", map[string]any{
				"retention.subsystem": result.Name,
				"retention.deleted":   deleted,
			})
			m.logger.Info("retention subsystem finished",
				slog.String("subsystem", result.Name),
				slog.Int("deleted", deleted),
				slog.Duration("max_age", m.cfg.TraceSnapshots.MaxAge),
				slog.Int("max_count", m.cfg.TraceSnapshots.MaxCount),
				slog.String("trigger", trigger),
			)
		}
		results = append(results, result)
	}

	runBudgetSubsystem := func() {
		result := SubsystemResult{
			Name:     SubsystemBudgetEvents,
			MaxAge:   m.cfg.BudgetEvents.MaxAge,
			MaxCount: m.cfg.BudgetEvents.MaxCount,
		}
		if !shouldRun(req.Subsystems, result.Name) || m.budgets == nil {
			result.Skipped = true
			results = append(results, result)
			return
		}
		deleted, err := m.budgets.PruneEvents(ctx, m.cfg.BudgetEvents.MaxAge, m.cfg.BudgetEvents.MaxCount)
		result.Deleted = deleted
		if err != nil {
			result.Error = err.Error()
			trace.Record("retention.subsystem.failed", map[string]any{
				"retention.subsystem": result.Name,
				"error.message":       err.Error(),
			})
		} else {
			trace.Record("retention.subsystem.finished", map[string]any{
				"retention.subsystem": result.Name,
				"retention.deleted":   deleted,
			})
			m.logger.Info("retention subsystem finished",
				slog.String("subsystem", result.Name),
				slog.Int("deleted", deleted),
				slog.Duration("max_age", m.cfg.BudgetEvents.MaxAge),
				slog.Int("max_count", m.cfg.BudgetEvents.MaxCount),
				slog.String("trigger", trigger),
			)
		}
		results = append(results, result)
	}

	runAuditSubsystem := func() {
		result := SubsystemResult{
			Name:     SubsystemAuditEvents,
			MaxAge:   m.cfg.AuditEvents.MaxAge,
			MaxCount: m.cfg.AuditEvents.MaxCount,
		}
		if !shouldRun(req.Subsystems, result.Name) || m.audit == nil {
			result.Skipped = true
			results = append(results, result)
			return
		}
		deleted, err := m.audit.PruneAuditEvents(ctx, m.cfg.AuditEvents.MaxAge, m.cfg.AuditEvents.MaxCount)
		result.Deleted = deleted
		if err != nil {
			result.Error = err.Error()
			trace.Record("retention.subsystem.failed", map[string]any{
				"retention.subsystem": result.Name,
				"error.message":       err.Error(),
			})
		} else {
			trace.Record("retention.subsystem.finished", map[string]any{
				"retention.subsystem": result.Name,
				"retention.deleted":   deleted,
			})
			m.logger.Info("retention subsystem finished",
				slog.String("subsystem", result.Name),
				slog.Int("deleted", deleted),
				slog.Duration("max_age", m.cfg.AuditEvents.MaxAge),
				slog.Int("max_count", m.cfg.AuditEvents.MaxCount),
				slog.String("trigger", trigger),
			)
		}
		results = append(results, result)
	}

	runTraceSubsystem()
	runBudgetSubsystem()
	runAuditSubsystem()
	runSubsystem(SubsystemExactCache, m.cfg.ExactCache, m.exact)
	runSubsystem(SubsystemSemanticCache, m.cfg.SemanticCache, m.semantic)

	finishedAt := time.Now().UTC()
	trace.Record("retention.run.finished", map[string]any{
		"retention.trigger": trigger,
		"retention.results": len(results),
	})

	run := RunResult{
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Trigger:    trigger,
		Results:    results,
	}
	if m.history != nil {
		record := HistoryRecord{
			StartedAt:  run.StartedAt.UTC().Format(time.RFC3339Nano),
			FinishedAt: run.FinishedAt.UTC().Format(time.RFC3339Nano),
			Trigger:    run.Trigger,
			Actor:      req.Actor,
			RequestID:  req.RequestID,
			Results:    cloneSubsystemResults(run.Results),
		}
		if err := m.history.AppendRun(ctx, record); err != nil {
			trace.Record("retention.history.failed", map[string]any{
				"error.message": err.Error(),
			})
			m.logger.Warn("retention history append failed", slog.Any("error", err))
		} else {
			trace.Record("retention.history.persisted", map[string]any{
				"retention.trigger": run.Trigger,
			})
		}
	}
	return run
}

func (m *Manager) ListRuns(ctx context.Context, limit int) ([]HistoryRecord, error) {
	if m == nil || m.history == nil {
		return nil, nil
	}
	return m.history.ListRuns(ctx, limit)
}

func (m *Manager) RunLoop(ctx context.Context) {
	if m == nil || !m.cfg.Enabled || m.cfg.Interval <= 0 {
		return
	}

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Run(ctx, RunRequest{Trigger: "scheduled"})
		}
	}
}

func shouldRun(selected []string, subsystem string) bool {
	if len(selected) == 0 {
		return true
	}
	return slices.Contains(selected, subsystem)
}
