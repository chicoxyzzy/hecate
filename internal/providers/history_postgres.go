package providers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/hecate/agent-runtime/internal/storage"
)

type PostgresHealthHistoryStore struct {
	db    *sql.DB
	table string
}

func NewPostgresHealthHistoryStore(ctx context.Context, client *storage.PostgresClient, tableName string) (*PostgresHealthHistoryStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = "provider_health_history"
	}
	store := &PostgresHealthHistoryStore{
		db:    client.DB(),
		table: client.QualifiedTable(tableName),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresHealthHistoryStore) Append(ctx context.Context, record HealthHistoryRecord) error {
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
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, s.table),
		record.Provider,
		record.Event,
		record.Status,
		record.Available,
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
		return fmt.Errorf("insert postgres provider health history: %w", err)
	}
	return nil
}

func (s *PostgresHealthHistoryStore) List(ctx context.Context, filter HealthHistoryFilter) ([]HealthHistoryRecord, error) {
	limit := normalizeHealthHistoryLimit(filter.Limit)
	args := make([]any, 0, 2)
	query := fmt.Sprintf(`
		SELECT provider, event, status, available, error_message, error_class,
		       latency_ms, consecutive_failures, total_successes, total_failures,
		       timeouts, server_errors, rate_limits, open_until, timestamp_utc
		FROM %s
	`, s.table)
	if strings.TrimSpace(filter.Provider) != "" {
		query += ` WHERE provider = $1`
		args = append(args, filter.Provider)
		query += ` ORDER BY timestamp_utc DESC, id DESC LIMIT $2`
		args = append(args, limit)
	} else {
		query += ` ORDER BY timestamp_utc DESC, id DESC LIMIT $1`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list postgres provider health history: %w", err)
	}
	defer rows.Close()

	records := make([]HealthHistoryRecord, 0, maxHealthHistoryListLimit)
	for rows.Next() {
		var record HealthHistoryRecord
		if err := rows.Scan(
			&record.Provider,
			&record.Event,
			&record.Status,
			&record.Available,
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
			return nil, fmt.Errorf("scan postgres provider health history: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres provider health history: %w", err)
	}
	return records, nil
}

func (s *PostgresHealthHistoryStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			provider TEXT NOT NULL,
			event TEXT NOT NULL,
			status TEXT NOT NULL,
			available BOOLEAN NOT NULL,
			error_message TEXT NOT NULL DEFAULT '',
			error_class TEXT NOT NULL DEFAULT '',
			latency_ms BIGINT NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			total_successes BIGINT NOT NULL DEFAULT 0,
			total_failures BIGINT NOT NULL DEFAULT 0,
			timeouts BIGINT NOT NULL DEFAULT 0,
			server_errors BIGINT NOT NULL DEFAULT 0,
			rate_limits BIGINT NOT NULL DEFAULT 0,
			open_until TEXT NOT NULL DEFAULT '',
			timestamp_utc TEXT NOT NULL
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres provider health history store: %w", err)
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_provider_timestamp_idx
		ON %s (provider, timestamp_utc DESC, id DESC)
	`, strings.ReplaceAll(s.table, ".", "_"), s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres provider health history index: %w", err)
	}
	return nil
}
