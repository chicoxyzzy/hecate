package taskstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

type PostgresStore struct {
	db             *sql.DB
	tasksTable     string
	runsTable      string
	stepsTable     string
	approvalsTable string
	artifactsTable string
	eventsTable    string
}

func NewPostgresStore(ctx context.Context, client *storage.PostgresClient) (*PostgresStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	store := &PostgresStore{
		db:             client.DB(),
		tasksTable:     client.QualifiedTable("task_state_tasks"),
		runsTable:      client.QualifiedTable("task_state_runs"),
		stepsTable:     client.QualifiedTable("task_state_steps"),
		approvalsTable: client.QualifiedTable("task_state_approvals"),
		artifactsTable: client.QualifiedTable("task_state_artifacts"),
		eventsTable:    client.QualifiedTable("task_state_run_events"),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Backend() string { return "postgres" }

func (s *PostgresStore) CreateTask(ctx context.Context, task types.Task) (types.Task, error) {
	if strings.TrimSpace(task.ID) == "" {
		return types.Task{}, fmt.Errorf("task id is required")
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	payload, err := json.Marshal(task)
	if err != nil {
		return types.Task{}, err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, tenant, status, updated_at, payload)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (id)
		DO UPDATE SET tenant = EXCLUDED.tenant, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at, payload = EXCLUDED.payload
	`, s.tasksTable), task.ID, task.Tenant, task.Status, task.UpdatedAt, payload)
	if err != nil {
		return types.Task{}, err
	}
	return task, nil
}

func (s *PostgresStore) GetTask(ctx context.Context, id string) (types.Task, bool, error) {
	var payload []byte
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, s.tasksTable), id).Scan(&payload)
	if err == sql.ErrNoRows {
		return types.Task{}, false, nil
	}
	if err != nil {
		return types.Task{}, false, err
	}
	var task types.Task
	if err := json.Unmarshal(payload, &task); err != nil {
		return types.Task{}, false, err
	}
	return task, true, nil
}

func (s *PostgresStore) ListTasks(ctx context.Context, filter TaskFilter) ([]types.Task, error) {
	args := []any{}
	where := []string{"1=1"}
	if filter.Tenant != "" {
		args = append(args, filter.Tenant)
		where = append(where, fmt.Sprintf("tenant = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	limitSQL := ""
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		limitSQL = fmt.Sprintf("LIMIT $%d", len(args))
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE %s
		ORDER BY updated_at DESC
		%s
	`, s.tasksTable, strings.Join(where, " AND "), limitSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.Task, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var task types.Task
		if err := json.Unmarshal(payload, &task); err != nil {
			return nil, err
		}
		items = append(items, task)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateTask(ctx context.Context, task types.Task) (types.Task, error) {
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = time.Now().UTC()
	}
	return s.CreateTask(ctx, task)
}

func (s *PostgresStore) DeleteTask(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("task id is required")
	}
	for _, table := range []string{s.eventsTable, s.artifactsTable, s.approvalsTable, s.stepsTable, s.runsTable} {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE task_id = $1`, table), id); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.tasksTable), id)
	return err
}

func (s *PostgresStore) CreateRun(ctx context.Context, run types.TaskRun) (types.TaskRun, error) {
	if strings.TrimSpace(run.ID) == "" {
		return types.TaskRun{}, fmt.Errorf("run id is required")
	}
	payload, err := json.Marshal(run)
	if err != nil {
		return types.TaskRun{}, err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, task_id, number, status, started_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (id)
		DO UPDATE SET status = EXCLUDED.status, started_at = EXCLUDED.started_at, payload = EXCLUDED.payload
	`, s.runsTable), run.ID, run.TaskID, run.Number, run.Status, run.StartedAt, payload)
	if err != nil {
		return types.TaskRun{}, err
	}
	return run, nil
}

func (s *PostgresStore) GetRun(ctx context.Context, taskID, runID string) (types.TaskRun, bool, error) {
	var payload []byte
	args := []any{runID}
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, s.runsTable)
	if taskID != "" {
		args = append(args, taskID)
		query += " AND task_id = $2"
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&payload)
	if err == sql.ErrNoRows {
		return types.TaskRun{}, false, nil
	}
	if err != nil {
		return types.TaskRun{}, false, err
	}
	var run types.TaskRun
	if err := json.Unmarshal(payload, &run); err != nil {
		return types.TaskRun{}, false, err
	}
	return run, true, nil
}

func (s *PostgresStore) ListRuns(ctx context.Context, taskID string) ([]types.TaskRun, error) {
	return s.ListRunsByFilter(ctx, RunFilter{TaskID: taskID})
}

func (s *PostgresStore) ListRunsByFilter(ctx context.Context, filter RunFilter) ([]types.TaskRun, error) {
	args := []any{}
	where := []string{"1=1"}
	if filter.TaskID != "" {
		args = append(args, filter.TaskID)
		where = append(where, fmt.Sprintf("task_id = $%d", len(args)))
	}
	if len(filter.Statuses) > 0 {
		args = append(args, filter.Statuses)
		where = append(where, fmt.Sprintf("status = ANY($%d)", len(args)))
	}
	limitSQL := ""
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		limitSQL = fmt.Sprintf("LIMIT $%d", len(args))
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE %s
		ORDER BY number DESC, started_at DESC
		%s
	`, s.runsTable, strings.Join(where, " AND "), limitSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.TaskRun, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var run types.TaskRun
		if err := json.Unmarshal(payload, &run); err != nil {
			return nil, err
		}
		items = append(items, run)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateRun(ctx context.Context, run types.TaskRun) (types.TaskRun, error) {
	return s.CreateRun(ctx, run)
}

func (s *PostgresStore) AppendStep(ctx context.Context, step types.TaskStep) (types.TaskStep, error) {
	if strings.TrimSpace(step.ID) == "" {
		return types.TaskStep{}, fmt.Errorf("step id is required")
	}
	payload, err := json.Marshal(step)
	if err != nil {
		return types.TaskStep{}, err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, task_id, run_id, step_index, status, started_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (id)
		DO UPDATE SET status = EXCLUDED.status, payload = EXCLUDED.payload
	`, s.stepsTable), step.ID, step.TaskID, step.RunID, step.Index, step.Status, step.StartedAt, payload)
	if err != nil {
		return types.TaskStep{}, err
	}
	return step, nil
}

func (s *PostgresStore) GetStep(ctx context.Context, runID, stepID string) (types.TaskStep, bool, error) {
	var payload []byte
	args := []any{stepID}
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, s.stepsTable)
	if runID != "" {
		args = append(args, runID)
		query += " AND run_id = $2"
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&payload)
	if err == sql.ErrNoRows {
		return types.TaskStep{}, false, nil
	}
	if err != nil {
		return types.TaskStep{}, false, err
	}
	var step types.TaskStep
	if err := json.Unmarshal(payload, &step); err != nil {
		return types.TaskStep{}, false, err
	}
	return step, true, nil
}

func (s *PostgresStore) ListSteps(ctx context.Context, runID string) ([]types.TaskStep, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE run_id = $1
		ORDER BY step_index ASC, id ASC
	`, s.stepsTable), runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.TaskStep, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var step types.TaskStep
		if err := json.Unmarshal(payload, &step); err != nil {
			return nil, err
		}
		items = append(items, step)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateStep(ctx context.Context, step types.TaskStep) (types.TaskStep, error) {
	return s.AppendStep(ctx, step)
}

func (s *PostgresStore) CreateApproval(ctx context.Context, approval types.TaskApproval) (types.TaskApproval, error) {
	if strings.TrimSpace(approval.ID) == "" {
		return types.TaskApproval{}, fmt.Errorf("approval id is required")
	}
	payload, err := json.Marshal(approval)
	if err != nil {
		return types.TaskApproval{}, err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, task_id, run_id, status, created_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		ON CONFLICT (id)
		DO UPDATE SET status = EXCLUDED.status, payload = EXCLUDED.payload
	`, s.approvalsTable), approval.ID, approval.TaskID, approval.RunID, approval.Status, approval.CreatedAt, payload)
	if err != nil {
		return types.TaskApproval{}, err
	}
	return approval, nil
}

func (s *PostgresStore) GetApproval(ctx context.Context, taskID, approvalID string) (types.TaskApproval, bool, error) {
	var payload []byte
	args := []any{approvalID}
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, s.approvalsTable)
	if taskID != "" {
		args = append(args, taskID)
		query += " AND task_id = $2"
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&payload)
	if err == sql.ErrNoRows {
		return types.TaskApproval{}, false, nil
	}
	if err != nil {
		return types.TaskApproval{}, false, err
	}
	var approval types.TaskApproval
	if err := json.Unmarshal(payload, &approval); err != nil {
		return types.TaskApproval{}, false, err
	}
	return approval, true, nil
}

func (s *PostgresStore) ListApprovals(ctx context.Context, taskID string) ([]types.TaskApproval, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE task_id = $1
		ORDER BY created_at DESC, id DESC
	`, s.approvalsTable), taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.TaskApproval, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var approval types.TaskApproval
		if err := json.Unmarshal(payload, &approval); err != nil {
			return nil, err
		}
		items = append(items, approval)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateApproval(ctx context.Context, approval types.TaskApproval) (types.TaskApproval, error) {
	return s.CreateApproval(ctx, approval)
}

func (s *PostgresStore) CreateArtifact(ctx context.Context, artifact types.TaskArtifact) (types.TaskArtifact, error) {
	if strings.TrimSpace(artifact.ID) == "" {
		return types.TaskArtifact{}, fmt.Errorf("artifact id is required")
	}
	payload, err := json.Marshal(artifact)
	if err != nil {
		return types.TaskArtifact{}, err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, task_id, run_id, step_id, kind, status, created_at, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
		ON CONFLICT (id)
		DO UPDATE SET status = EXCLUDED.status, payload = EXCLUDED.payload
	`, s.artifactsTable), artifact.ID, artifact.TaskID, artifact.RunID, artifact.StepID, artifact.Kind, artifact.Status, artifact.CreatedAt, payload)
	if err != nil {
		return types.TaskArtifact{}, err
	}
	return artifact, nil
}

func (s *PostgresStore) GetArtifact(ctx context.Context, taskID, artifactID string) (types.TaskArtifact, bool, error) {
	var payload []byte
	args := []any{artifactID}
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE id = $1`, s.artifactsTable)
	if taskID != "" {
		args = append(args, taskID)
		query += " AND task_id = $2"
	}
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&payload)
	if err == sql.ErrNoRows {
		return types.TaskArtifact{}, false, nil
	}
	if err != nil {
		return types.TaskArtifact{}, false, err
	}
	var artifact types.TaskArtifact
	if err := json.Unmarshal(payload, &artifact); err != nil {
		return types.TaskArtifact{}, false, err
	}
	return artifact, true, nil
}

func (s *PostgresStore) ListArtifacts(ctx context.Context, filter ArtifactFilter) ([]types.TaskArtifact, error) {
	args := []any{}
	where := []string{"1=1"}
	if filter.TaskID != "" {
		args = append(args, filter.TaskID)
		where = append(where, fmt.Sprintf("task_id = $%d", len(args)))
	}
	if filter.RunID != "" {
		args = append(args, filter.RunID)
		where = append(where, fmt.Sprintf("run_id = $%d", len(args)))
	}
	if filter.StepID != "" {
		args = append(args, filter.StepID)
		where = append(where, fmt.Sprintf("step_id = $%d", len(args)))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		where = append(where, fmt.Sprintf("kind = $%d", len(args)))
	}
	limitSQL := ""
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		limitSQL = fmt.Sprintf("LIMIT $%d", len(args))
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE %s
		ORDER BY created_at DESC, id DESC
		%s
	`, s.artifactsTable, strings.Join(where, " AND "), limitSQL), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.TaskArtifact, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var artifact types.TaskArtifact
		if err := json.Unmarshal(payload, &artifact); err != nil {
			return nil, err
		}
		items = append(items, artifact)
	}
	return items, rows.Err()
}

func (s *PostgresStore) UpdateArtifact(ctx context.Context, artifact types.TaskArtifact) (types.TaskArtifact, error) {
	return s.CreateArtifact(ctx, artifact)
}

func (s *PostgresStore) AppendRunEvent(ctx context.Context, event types.TaskRunEvent) (types.TaskRunEvent, error) {
	if strings.TrimSpace(event.RunID) == "" {
		return types.TaskRunEvent{}, fmt.Errorf("run id is required")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	payload, err := json.Marshal(event.Data)
	if err != nil {
		return types.TaskRunEvent{}, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (task_id, run_id, event_type, event_data, request_id, trace_id, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
		RETURNING sequence
	`, s.eventsTable), event.TaskID, event.RunID, event.EventType, payload, event.RequestID, event.TraceID, event.CreatedAt).Scan(&id)
	if err != nil {
		return types.TaskRunEvent{}, err
	}
	event.Sequence = id
	event.ID = fmt.Sprintf("%d", id)
	return event, nil
}

func (s *PostgresStore) ListRunEvents(ctx context.Context, taskID, runID string, afterSequence int64, limit int) ([]types.TaskRunEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	args := []any{runID, afterSequence, limit}
	query := fmt.Sprintf(`
		SELECT sequence, task_id, run_id, event_type, event_data, created_at, request_id, trace_id
		FROM %s
		WHERE run_id = $1 AND sequence > $2
	`, s.eventsTable)
	if taskID != "" {
		args = append(args, taskID)
		query += fmt.Sprintf(" AND task_id = $%d", len(args))
	}
	query += " ORDER BY sequence ASC LIMIT $3"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]types.TaskRunEvent, 0)
	for rows.Next() {
		var event types.TaskRunEvent
		var payload []byte
		if err := rows.Scan(&event.Sequence, &event.TaskID, &event.RunID, &event.EventType, &payload, &event.CreatedAt, &event.RequestID, &event.TraceID); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &event.Data)
		event.ID = fmt.Sprintf("%d", event.Sequence)
		items = append(items, event)
	}
	return items, rows.Err()
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, tenant TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), payload JSONB NOT NULL)`, s.tasksTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (tenant, status, updated_at DESC)`, "task_state_tasks_tenant_status_updated_idx", s.tasksTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, number INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT '', started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), payload JSONB NOT NULL)`, s.runsTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (task_id, number DESC, started_at DESC)`, "task_state_runs_task_number_started_idx", s.runsTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (status, started_at DESC)`, "task_state_runs_status_started_idx", s.runsTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, run_id TEXT NOT NULL, step_index INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT '', started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), payload JSONB NOT NULL)`, s.stepsTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (run_id, step_index ASC, id ASC)`, "task_state_steps_run_idx_idx", s.stepsTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, run_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), payload JSONB NOT NULL)`, s.approvalsTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (task_id, created_at DESC, id DESC)`, "task_state_approvals_task_created_idx", s.approvalsTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, run_id TEXT NOT NULL, step_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), payload JSONB NOT NULL)`, s.artifactsTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (task_id, run_id, created_at DESC, id DESC)`, "task_state_artifacts_task_run_created_idx", s.artifactsTable),
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (sequence BIGSERIAL PRIMARY KEY, task_id TEXT NOT NULL, run_id TEXT NOT NULL, event_type TEXT NOT NULL DEFAULT '', event_data JSONB NOT NULL DEFAULT '{}'::jsonb, request_id TEXT NOT NULL DEFAULT '', trace_id TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`, s.eventsTable),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (run_id, sequence ASC)`, "task_state_run_events_run_sequence_idx", s.eventsTable),
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate postgres task store: %w", err)
		}
	}
	return nil
}
