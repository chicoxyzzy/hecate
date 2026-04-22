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

func (s *PostgresBudgetStore) Snapshot(ctx context.Context, key string) (AccountState, bool, error) {
	var account AccountState
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT budget_key, balance_micros_usd, credited_micros_usd, debited_micros_usd, updated_at FROM %s WHERE budget_key = $1`, s.table),
		key,
	).Scan(&account.Key, &account.BalanceMicrosUSD, &account.CreditedMicrosUSD, &account.DebitedMicrosUSD, &account.UpdatedAt)
	if err == sql.ErrNoRows {
		return AccountState{}, false, nil
	}
	return account, err == nil, err
}

func (s *PostgresBudgetStore) Debit(ctx context.Context, event UsageEvent) (AccountState, error) {
	if event.BudgetKey == "" || event.CostMicros <= 0 {
		return AccountState{Key: event.BudgetKey}, nil
	}
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, balance_micros_usd, credited_micros_usd, debited_micros_usd, updated_at)
			VALUES ($1, $2 * -1, 0, $2, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET
				balance_micros_usd = %s.balance_micros_usd - EXCLUDED.debited_micros_usd,
				debited_micros_usd = %s.debited_micros_usd + EXCLUDED.debited_micros_usd,
				updated_at = NOW()
		`, s.table, s.table, s.table),
		event.BudgetKey,
		event.CostMicros,
	)
	if err != nil {
		return AccountState{}, err
	}
	account, _, err := s.Snapshot(ctx, event.BudgetKey)
	return account, err
}

func (s *PostgresBudgetStore) Reset(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, balance_micros_usd, credited_micros_usd, debited_micros_usd, updated_at)
			VALUES ($1, 0, 0, 0, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET balance_micros_usd = 0, credited_micros_usd = 0, debited_micros_usd = 0, updated_at = NOW()
		`, s.table),
		key,
	)
	return err
}

func (s *PostgresBudgetStore) SetBalance(ctx context.Context, key string, value int64) (AccountState, error) {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, balance_micros_usd, credited_micros_usd, debited_micros_usd, updated_at)
			VALUES ($1, $2, 0, 0, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET balance_micros_usd = EXCLUDED.balance_micros_usd, updated_at = NOW()
		`, s.table),
		key,
		value,
	)
	if err != nil {
		return AccountState{}, err
	}
	account, _, err := s.Snapshot(ctx, key)
	return account, err
}

func (s *PostgresBudgetStore) Credit(ctx context.Context, key string, delta int64) (AccountState, error) {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, balance_micros_usd, credited_micros_usd, debited_micros_usd, updated_at)
			VALUES ($1, $2, $2, 0, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET
				balance_micros_usd = %s.balance_micros_usd + EXCLUDED.balance_micros_usd,
				credited_micros_usd = %s.credited_micros_usd + EXCLUDED.credited_micros_usd,
				updated_at = NOW()
		`, s.table, s.table, s.table),
		key,
		delta,
	)
	if err != nil {
		return AccountState{}, err
	}
	account, _, err := s.Snapshot(ctx, key)
	return account, err
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
			credited_micros_usd,
			debited_micros_usd,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
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
		event.CreditedMicrosUSD,
		event.DebitedMicrosUSD,
		event.PromptTokens,
		event.CompletionTokens,
		event.TotalTokens,
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
			credited_micros_usd,
			debited_micros_usd,
			prompt_tokens,
			completion_tokens,
			total_tokens,
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
			&event.CreditedMicrosUSD,
			&event.DebitedMicrosUSD,
			&event.PromptTokens,
			&event.CompletionTokens,
			&event.TotalTokens,
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

func (s *PostgresBudgetStore) ListRecentEvents(ctx context.Context, limit int) ([]BudgetEvent, error) {
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
			credited_micros_usd,
			debited_micros_usd,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			occurred_at
		FROM %s
		ORDER BY occurred_at DESC, id DESC
		LIMIT $1
	`, s.eventsTable), limit)
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
			&event.CreditedMicrosUSD,
			&event.DebitedMicrosUSD,
			&event.PromptTokens,
			&event.CompletionTokens,
			&event.TotalTokens,
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
			balance_micros_usd BIGINT NOT NULL DEFAULT 0,
			credited_micros_usd BIGINT NOT NULL DEFAULT 0,
			debited_micros_usd BIGINT NOT NULL DEFAULT 0,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres budget store: %w", err)
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		ALTER TABLE %s
		ADD COLUMN IF NOT EXISTS balance_micros_usd BIGINT NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS credited_micros_usd BIGINT NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS debited_micros_usd BIGINT NOT NULL DEFAULT 0
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres budget account columns: %w", err)
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
			credited_micros_usd BIGINT NOT NULL DEFAULT 0,
			debited_micros_usd BIGINT NOT NULL DEFAULT 0,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, s.eventsTable))
	if err != nil {
		return fmt.Errorf("migrate postgres budget event store: %w", err)
	}

	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		ALTER TABLE %s
		ADD COLUMN IF NOT EXISTS credited_micros_usd BIGINT NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS debited_micros_usd BIGINT NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS prompt_tokens INTEGER NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS completion_tokens INTEGER NOT NULL DEFAULT 0,
		ADD COLUMN IF NOT EXISTS total_tokens INTEGER NOT NULL DEFAULT 0
	`, s.eventsTable))
	if err != nil {
		return fmt.Errorf("migrate postgres budget event columns: %w", err)
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
