package governor

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/hecate/agent-runtime/internal/storage"
)

type PostgresBudgetStore struct {
	db    *sql.DB
	table string
}

func NewPostgresBudgetStore(ctx context.Context, client *storage.PostgresClient) (*PostgresBudgetStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}

	store := &PostgresBudgetStore{
		db:    client.DB(),
		table: client.QualifiedTable("budget"),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresBudgetStore) Spent(ctx context.Context, key string) (int64, error) {
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

func (s *PostgresBudgetStore) AddSpent(ctx context.Context, key string, delta int64) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (budget_key, spent_micros_usd, limit_micros_usd, updated_at)
			VALUES ($1, $2, 0, NOW())
			ON CONFLICT (budget_key)
			DO UPDATE SET spent_micros_usd = %s.spent_micros_usd + EXCLUDED.spent_micros_usd, updated_at = NOW()
		`, s.table, s.table),
		key,
		delta,
	)
	return err
}

func (s *PostgresBudgetStore) ResetSpent(ctx context.Context, key string) error {
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
	return nil
}
