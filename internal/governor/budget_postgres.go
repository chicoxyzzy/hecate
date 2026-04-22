package governor

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
)

var budgetIdentifierPattern = regexp.MustCompile(`[^a-z0-9_]+`)

type PostgresBudgetStore struct {
	db          *sql.DB
	table       string
	eventsTable string
}

func NewPostgresBudgetStore(ctx context.Context, client *storage.PostgresClient) (*PostgresBudgetStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}

	store := &PostgresBudgetStore{
		db:          client.DB(),
		table:       client.QualifiedTable("budget"),
		eventsTable: client.QualifiedTable("budget_events"),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresBudgetStore) Current(ctx context.Context, key string) (int64, error) {
	var value int64
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT spent_micros_usd FROM %s WHERE budget_key = $1`, s.table),
		key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return value, err
}

func (s *PostgresBudgetStore) Record(ctx context.Context, event UsageEvent) error {
	if event.BudgetKey == "" || event.CostMicros <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, spent_micros_usd, limit_micros_usd, updated_at)
			VALUES ($1, $2, 0, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET spent_micros_usd = %s.spent_micros_usd + EXCLUDED.spent_micros_usd, updated_at = NOW()
		`, s.table, s.table),
		event.BudgetKey,
		event.CostMicros,
	)
	return err
}

func (s *PostgresBudgetStore) Reset(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, spent_micros_usd, limit_micros_usd, updated_at)
			VALUES ($1, 0, 0, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET spent_micros_usd = 0, updated_at = NOW()
		`, s.table),
		key,
	)
	return err
}

func (s *PostgresBudgetStore) Limit(ctx context.Context, key string) (int64, error) {
	var value int64
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT limit_micros_usd FROM %s WHERE budget_key = $1`, s.table),
		key,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return value, err
}

func (s *PostgresBudgetStore) SetLimit(ctx context.Context, key string, value int64) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, spent_micros_usd, limit_micros_usd, updated_at)
			VALUES ($1, 0, $2, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET limit_micros_usd = EXCLUDED.limit_micros_usd, updated_at = NOW()
		`, s.table),
		key,
		value,
	)
	return err
}

func (s *PostgresBudgetStore) AddLimit(ctx context.Context, key string, delta int64) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, spent_micros_usd, limit_micros_usd, updated_at)
			VALUES ($1, 0, $2, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET limit_micros_usd = %s.limit_micros_usd + EXCLUDED.limit_micros_usd, updated_at = NOW()
		`, s.table, s.table),
		key,
		delta,
	)
	return err
}

func (s *PostgresBudgetStore) AppendEvent(ctx context.Context, event BudgetEvent) error {
	if event.Key == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			budget_key,
			event_type,
			scope,
			provider,
			tenant,
			model,
			request_id,
			actor,
			detail,
			amount_micros_usd,
			balance_micros_usd,
			limit_micros_usd,
			occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, s.eventsTable),
		event.Key,
		event.Type,
		event.Scope,
		event.Provider,
		event.Tenant,
		event.Model,
		event.RequestID,
		event.Actor,
		event.Detail,
		event.AmountMicrosUSD,
		event.BalanceMicrosUSD,
		event.LimitMicrosUSD,
		event.OccurredAt,
	)
	return err
}

func (s *PostgresBudgetStore) ListEvents(ctx context.Context, key string, limit int) ([]BudgetEvent, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT
			budget_key,
			event_type,
			scope,
			provider,
			tenant,
			model,
			request_id,
			actor,
			detail,
			amount_micros_usd,
			balance_micros_usd,
			limit_micros_usd,
			occurred_at
		FROM %s
		WHERE budget_key = $1
		ORDER BY occurred_at DESC, id DESC
		LIMIT $2
	`, s.eventsTable), key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]BudgetEvent, 0, limit)
	for rows.Next() {
		var event BudgetEvent
		if err := rows.Scan(
			&event.Key,
			&event.Type,
			&event.Scope,
			&event.Provider,
			&event.Tenant,
			&event.Model,
			&event.RequestID,
			&event.Actor,
			&event.Detail,
			&event.AmountMicrosUSD,
			&event.BalanceMicrosUSD,
			&event.LimitMicrosUSD,
			&event.OccurredAt,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *PostgresBudgetStore) PruneEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error) {
	deleted := int64(0)

	if maxAge > 0 {
		result, err := s.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE occurred_at < $1`, s.eventsTable),
			time.Now().Add(-maxAge).UTC(),
		)
		if err != nil {
			return 0, fmt.Errorf("delete aged postgres budget events: %w", err)
		}
		count, _ := result.RowsAffected()
		deleted += count
	}

	if maxCount > 0 {
		result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM %s
			WHERE id IN (
				SELECT id
				FROM (
					SELECT id,
					       ROW_NUMBER() OVER (PARTITION BY budget_key ORDER BY occurred_at DESC, id DESC) AS row_num
					FROM %s
				) ranked
				WHERE ranked.row_num > $1
			)
		`, s.eventsTable, s.eventsTable), maxCount)
		if err != nil {
			return 0, fmt.Errorf("enforce postgres budget event max count: %w", err)
		}
		count, _ := result.RowsAffected()
		deleted += count
	}

	return int(deleted), nil
}

func (s *PostgresBudgetStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			budget_key TEXT PRIMARY KEY,
			spent_micros_usd BIGINT NOT NULL DEFAULT 0,
			limit_micros_usd BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres budget store: %w", err)
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			budget_key TEXT NOT NULL,
			event_type TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			tenant TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT '',
			amount_micros_usd BIGINT NOT NULL DEFAULT 0,
			balance_micros_usd BIGINT NOT NULL DEFAULT 0,
			limit_micros_usd BIGINT NOT NULL DEFAULT 0,
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, s.eventsTable))
	if err != nil {
		return fmt.Errorf("migrate postgres budget event store: %w", err)
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS "%s"
		ON %s (budget_key, occurred_at DESC)
	`, sanitizeBudgetIdentifier(s.table+"_events_budget_key_occurred_at_idx"), s.eventsTable))
	if err != nil {
		return fmt.Errorf("migrate postgres budget event index: %w", err)
	}
	return nil
}

func sanitizeBudgetIdentifier(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, `"`, "_")
	value = budgetIdentifierPattern.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "budget_events_idx"
	}
	return value
}
