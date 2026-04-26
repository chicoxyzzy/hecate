package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hecate/agent-runtime/internal/storage"
)

const maxHistoryListLimit = 1_000

type PostgresHistoryStore struct {
	db    *sql.DB
	table string
}

func NewPostgresHistoryStore(ctx context.Context, client *storage.PostgresClient, tableName string) (*PostgresHistoryStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = "retention_runs"
	}

	store := &PostgresHistoryStore{
		db:    client.DB(),
		table: client.QualifiedTable(tableName),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresHistoryStore) AppendRun(ctx context.Context, record HistoryRecord) error {
	resultsJSON, err := json.Marshal(record.Results)
	if err != nil {
		return fmt.Errorf("encode retention history results: %w", err)
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			started_at,
			finished_at,
			trigger,
			actor,
			request_id,
			results_json
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, s.table),
		record.StartedAt,
		record.FinishedAt,
		record.Trigger,
		record.Actor,
		record.RequestID,
		resultsJSON,
	)
	if err != nil {
		return fmt.Errorf("insert postgres retention history: %w", err)
	}
	return nil
}

func (s *PostgresHistoryStore) ListRuns(ctx context.Context, limit int) ([]HistoryRecord, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > maxHistoryListLimit {
		limit = maxHistoryListLimit
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT started_at, finished_at, trigger, actor, request_id, results_json
		FROM %s
		ORDER BY finished_at DESC, id DESC
		LIMIT $1
	`, s.table), limit)
	if err != nil {
		return nil, fmt.Errorf("list postgres retention history: %w", err)
	}
	defer rows.Close()

	// Pre-allocate to the constant cap rather than `limit` so the user-controlled
	// value never reaches make()'s size argument. CodeQL's taint analysis flags
	// any flow from request input into make() — including via min(limit, const) —
	// so feeding it the constant directly is the safe form. The actual row count
	// is still bounded by the SQL LIMIT clause.
	records := make([]HistoryRecord, 0, maxHistoryListLimit)
	for rows.Next() {
		var record HistoryRecord
		var resultsJSON []byte
		if err := rows.Scan(
			&record.StartedAt,
			&record.FinishedAt,
			&record.Trigger,
			&record.Actor,
			&record.RequestID,
			&resultsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan postgres retention history: %w", err)
		}
		if len(resultsJSON) > 0 {
			if err := json.Unmarshal(resultsJSON, &record.Results); err != nil {
				return nil, fmt.Errorf("decode postgres retention history results: %w", err)
			}
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres retention history: %w", err)
	}
	return records, nil
}

func (s *PostgresHistoryStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			trigger TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			results_json JSONB NOT NULL
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres retention history store: %w", err)
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_finished_at_idx
		ON %s (finished_at DESC, id DESC)
	`, strings.ReplaceAll(s.table, ".", "_"), s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres retention history index: %w", err)
	}
	return nil
}
