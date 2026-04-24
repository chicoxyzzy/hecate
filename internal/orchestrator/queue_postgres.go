package orchestrator

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
)

type PostgresRunQueue struct {
	db           *sql.DB
	table        string
	leaseFor     time.Duration
	pollInterval time.Duration
}

func NewPostgresRunQueue(ctx context.Context, client *storage.PostgresClient, leaseFor time.Duration) (*PostgresRunQueue, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	if leaseFor <= 0 {
		leaseFor = 30 * time.Second
	}
	queue := &PostgresRunQueue{
		db:           client.DB(),
		table:        client.QualifiedTable("task_run_queue"),
		leaseFor:     leaseFor,
		pollInterval: 100 * time.Millisecond,
	}
	if err := queue.migrate(ctx); err != nil {
		return nil, err
	}
	return queue, nil
}

func (q *PostgresRunQueue) Backend() string { return "postgres" }

func (q *PostgresRunQueue) Enqueue(ctx context.Context, job QueueJob) error {
	_, err := q.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (task_id, run_id, status, available_at, lease_owner, lease_until, attempts, last_error, updated_at)
		VALUES ($1, $2, 'pending', NOW(), '', NULL, 0, '', NOW())
		ON CONFLICT (run_id)
		DO NOTHING
	`, q.table), job.TaskID, job.RunID)
	return err
}

func (q *PostgresRunQueue) Claim(ctx context.Context, workerID string, waitFor time.Duration) (QueueClaim, bool, error) {
	if waitFor <= 0 {
		waitFor = 2 * time.Second
	}
	deadline := time.Now().Add(waitFor)
	for time.Now().Before(deadline) {
		claim, ok, err := q.claimOnce(ctx, workerID)
		if err != nil {
			return QueueClaim{}, false, err
		}
		if ok {
			return claim, true, nil
		}
		select {
		case <-ctx.Done():
			return QueueClaim{}, false, ctx.Err()
		case <-time.After(q.pollInterval):
		}
	}
	return QueueClaim{}, false, nil
}

func (q *PostgresRunQueue) claimOnce(ctx context.Context, workerID string) (QueueClaim, bool, error) {
	tx, err := q.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return QueueClaim{}, false, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var (
		id     int64
		taskID string
		runID  string
	)
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT id, task_id, run_id
		FROM %s
		WHERE (status = 'pending' AND available_at <= NOW())
		   OR (status = 'leased' AND lease_until IS NOT NULL AND lease_until < NOW())
		ORDER BY id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, q.table)).Scan(&id, &taskID, &runID)
	if err == sql.ErrNoRows {
		_ = tx.Commit()
		return QueueClaim{}, false, nil
	}
	if err != nil {
		return QueueClaim{}, false, err
	}

	var leaseUntil time.Time
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET status = 'leased',
		    lease_owner = $2,
		    lease_until = NOW() + ($3 * INTERVAL '1 second'),
		    attempts = attempts + 1,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING lease_until
	`, q.table), id, workerID, int(q.leaseFor.Seconds())).Scan(&leaseUntil)
	if err != nil {
		return QueueClaim{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return QueueClaim{}, false, err
	}
	return QueueClaim{
		ClaimID:    fmt.Sprintf("%d", id),
		Job:        QueueJob{TaskID: taskID, RunID: runID},
		LeaseUntil: leaseUntil,
	}, true, nil
}

func (q *PostgresRunQueue) Ack(ctx context.Context, claimID string) error {
	_, err := q.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, q.table), claimID)
	return err
}

func (q *PostgresRunQueue) Nack(ctx context.Context, claimID, reason string) error {
	_, err := q.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET status = 'pending',
		    available_at = NOW() + INTERVAL '200 milliseconds',
		    lease_owner = '',
		    lease_until = NULL,
		    last_error = $2,
		    updated_at = NOW()
		WHERE id = $1
	`, q.table), claimID, reason)
	return err
}

func (q *PostgresRunQueue) ExtendLease(ctx context.Context, claimID string, leaseFor time.Duration) error {
	if leaseFor <= 0 {
		leaseFor = q.leaseFor
	}
	_, err := q.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s
		SET lease_until = NOW() + ($2 * INTERVAL '1 second'),
		    updated_at = NOW()
		WHERE id = $1 AND status = 'leased'
	`, q.table), claimID, int(leaseFor.Seconds()))
	return err
}

func (q *PostgresRunQueue) Depth(ctx context.Context) (int, error) {
	var count int
	err := q.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = 'pending'`, q.table)).Scan(&count)
	return count, err
}

func (q *PostgresRunQueue) Capacity() int {
	return 0
}

func (q *PostgresRunQueue) migrate(ctx context.Context) error {
	_, err := q.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			task_id TEXT NOT NULL,
			run_id TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'pending',
			available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_until TIMESTAMPTZ NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, q.table))
	if err != nil {
		return fmt.Errorf("migrate postgres run queue: %w", err)
	}
	_, err = q.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS "task_run_queue_status_available_idx"
		ON %s (status, available_at, lease_until)
	`, q.table))
	if err != nil {
		return fmt.Errorf("index postgres run queue: %w", err)
	}
	return nil
}
