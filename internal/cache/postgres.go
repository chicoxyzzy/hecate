package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

type PostgresStore struct {
	db         *sql.DB
	table      string
	defaultTTL time.Duration
}

func NewPostgresStore(ctx context.Context, client *storage.PostgresClient, defaultTTL time.Duration) (*PostgresStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	store := &PostgresStore{
		db:         client.DB(),
		table:      client.QualifiedTable("cache_exact"),
		defaultTTL: defaultTTL,
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Get(ctx context.Context, key string) (*types.ChatResponse, bool) {
	var payload []byte
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT response FROM %s WHERE cache_key = $1 AND expires_at > NOW()`, s.table),
		key,
	).Scan(&payload)
	if err != nil {
		return nil, false
	}

	var response types.ChatResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, false
	}
	return &response, true
}

func (s *PostgresStore) CleanupExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= NOW()`, s.table))
	if err != nil {
		return fmt.Errorf("delete expired postgres exact cache rows: %w", err)
	}
	return nil
}

func (s *PostgresStore) Prune(ctx context.Context, maxAge time.Duration, maxCount int) (int, error) {
	deleted := int64(0)

	if _, err := s.db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= NOW()`, s.table)); err != nil {
		return 0, fmt.Errorf("delete expired postgres exact cache rows: %w", err)
	}

	if maxAge > 0 {
		result, err := s.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE updated_at < $1`, s.table),
			time.Now().Add(-maxAge).UTC(),
		)
		if err != nil {
			return 0, fmt.Errorf("delete aged postgres exact cache rows: %w", err)
		}
		count, _ := result.RowsAffected()
		deleted += count
	}

	if maxCount > 0 {
		result, err := s.db.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM %s
			WHERE cache_key IN (
				SELECT cache_key
				FROM %s
				ORDER BY updated_at DESC
				OFFSET $1
			)
		`, s.table, s.table), maxCount)
		if err != nil {
			return 0, fmt.Errorf("enforce postgres exact cache max count: %w", err)
		}
		count, _ := result.RowsAffected()
		deleted += count
	}

	return int(deleted), nil
}

func (s *PostgresStore) Set(ctx context.Context, key string, response *types.ChatResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(s.defaultTTL)
	if s.defaultTTL <= 0 {
		expiresAt = time.Now().Add(24 * time.Hour)
	}

	_, err = s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (cache_key, response, expires_at, updated_at)
			VALUES ($1, $2::jsonb, $3, NOW())
			ON CONFLICT (cache_key)
			DO UPDATE SET response = EXCLUDED.response, expires_at = EXCLUDED.expires_at, updated_at = NOW()
		`, s.table),
		key,
		payload,
		expiresAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("write postgres exact cache: %w", err)
	}
	return nil
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			cache_key TEXT PRIMARY KEY,
			response JSONB NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate postgres exact cache: %w", err)
	}
	return nil
}
