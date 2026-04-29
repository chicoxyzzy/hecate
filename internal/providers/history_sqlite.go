package providers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/hecate/agent-runtime/internal/storage"
)

type SQLiteHealthHistoryStore struct {
	db    *sql.DB
	table string
}

func NewSQLiteHealthHistoryStore(ctx context.Context, client *storage.SQLiteClient, tableName string) (*SQLiteHealthHistoryStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("sqlite client is required")
	}
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = "provider_health_history"
	}
	store := &SQLiteHealthHistoryStore{
		db:    client.DB(),
		table: client.QualifiedTable(tableName),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteHealthHistoryStore) Append(ctx context.Context, record HealthHistoryRecord) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			provider,
			event,
			status,
			available,
			error_message,
			error_class,
			latency_ms,
			consecutive_failures,
			total_successes,
			total_failures,
			timeouts,
			server_errors,
			rate_limits,
			open_until,
			timestamp_utc
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.table),
		record.Provider,
		record.Event,
		record.Status,
		boolToInt(record.Available),
		record.Error,
		record.ErrorClass,
		record.LatencyMS,
		record.ConsecutiveFailures,
		record.TotalSuccesses,
		record.TotalFailures,
		record.Timeouts,
		record.ServerErrors,
		record.RateLimits,
		record.OpenUntil,
		record.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("insert sqlite provider health history: %w", err)
	}
	return nil
}

func (s *SQLiteHealthHistoryStore) List(ctx context.Context, filter HealthHistoryFilter) ([]HealthHistoryRecord, error) {
	limit := normalizeHealthHistoryLimit(filter.Limit)
	args := make([]any, 0, 2)
	query := fmt.Sprintf(`
		SELECT provider, event, status, available, error_message, error_class,
		       latency_ms, consecutive_failures, total_successes, total_failures,
		       timeouts, server_errors, rate_limits, open_until, timestamp_utc
		FROM %s
	`, s.table)
	if strings.TrimSpace(filter.Provider) != "" {
		query += ` WHERE provider = ?`
		args = append(args, filter.Provider)
	}
	query += ` ORDER BY timestamp_utc DESC, id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sqlite provider health history: %w", err)
	}
	defer rows.Close()

	records := make([]HealthHistoryRecord, 0, maxHealthHistoryListLimit)
	for rows.Next() {
		var record HealthHistoryRecord
		var available int
		if err := rows.Scan(
			&record.Provider,
			&record.Event,
			&record.Status,
			&available,
			&record.Error,
			&record.ErrorClass,
			&record.LatencyMS,
			&record.ConsecutiveFailures,
			&record.TotalSuccesses,
			&record.TotalFailures,
			&record.Timeouts,
			&record.ServerErrors,
			&record.RateLimits,
			&record.OpenUntil,
			&record.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan sqlite provider health history: %w", err)
		}
		record.Available = available != 0
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite provider health history: %w", err)
	}
	return records, nil
}

func (s *SQLiteHealthHistoryStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider TEXT NOT NULL,
			event TEXT NOT NULL,
			status TEXT NOT NULL,
			available INTEGER NOT NULL,
			error_message TEXT NOT NULL DEFAULT '',
			error_class TEXT NOT NULL DEFAULT '',
			latency_ms INTEGER NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			total_successes INTEGER NOT NULL DEFAULT 0,
			total_failures INTEGER NOT NULL DEFAULT 0,
			timeouts INTEGER NOT NULL DEFAULT 0,
			server_errors INTEGER NOT NULL DEFAULT 0,
			rate_limits INTEGER NOT NULL DEFAULT 0,
			open_until TEXT NOT NULL DEFAULT '',
			timestamp_utc TEXT NOT NULL
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate sqlite provider health history store: %w", err)
	}
	indexName := strings.Trim(s.table, `"`) + "_provider_timestamp_idx"
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS "%s"
		ON %s (provider, timestamp_utc DESC, id DESC)
	`, indexName, s.table))
	if err != nil {
		return fmt.Errorf("migrate sqlite provider health history index: %w", err)
	}
	return nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
