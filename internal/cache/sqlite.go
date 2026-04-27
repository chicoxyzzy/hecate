package cache

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

// SQLiteStore mirrors PostgresStore — same Get/Set/Prune contract, same
// TTL semantics — so the gateway can swap exact-cache backends purely via
// config. Differences from the Postgres flavor that aren't accidental:
//
//   - response column is TEXT (SQLite has no JSONB; we marshal/unmarshal
//     in Go).
//   - timestamps are stored as TEXT in RFC3339 — matches the retention
//     SQLite store and keeps the on-disk shape easy to inspect by hand.
//   - placeholders are `?` rather than `$N`.
//
// SQLite has no schema namespacing, so QualifiedTable returns just the
// prefixed table name with no schema dot.
type SQLiteStore struct {
	db         *sql.DB
	table      string
	defaultTTL time.Duration
}

// NewSQLiteStore opens the cache_exact table on the shared SQLite client.
// The `prefix` parameter is folded into the table base name so multiple
// logical caches can coexist on one file (`cache_exact` by default,
// `<prefix>_cache_exact` when prefix is set).
func NewSQLiteStore(ctx context.Context, client *storage.SQLiteClient, prefix string, defaultTTL time.Duration) (*SQLiteStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("sqlite client is required")
	}

	base := "cache_exact"
	if trimmed := strings.TrimSpace(prefix); trimmed != "" {
		base = trimmed + "_" + base
	}

	store := &SQLiteStore{
		db:         client.DB(),
		table:      client.QualifiedTable(base),
		defaultTTL: defaultTTL,
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Get(ctx context.Context, key string) (*types.ChatResponse, bool) {
	var payload string
	var expiresAt string
	err := s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT response, expires_at FROM %s WHERE cache_key = ?`, s.table),
		key,
	).Scan(&payload, &expiresAt)
	if err != nil {
		return nil, false
	}

	// SQLite has no NOW() comparison against TEXT timestamps, so we
	// filter expiry in Go. Same semantics as the Postgres branch — an
	// expired row simply misses; the Prune path is what reclaims space.
	if expiresAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, expiresAt); err == nil && !t.After(time.Now().UTC()) {
			return nil, false
		}
	}

	var response types.ChatResponse
	if err := json.Unmarshal([]byte(payload), &response); err != nil {
		return nil, false
	}
	return &response, true
}

func (s *SQLiteStore) CleanupExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= ?`, s.table),
		time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("delete expired sqlite exact cache rows: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Set(ctx context.Context, key string, response *types.ChatResponse) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}

	expiresAt := time.Now().Add(s.defaultTTL)
	if s.defaultTTL <= 0 {
		expiresAt = time.Now().Add(24 * time.Hour)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	_, err = s.db.ExecContext(ctx,
		fmt.Sprintf(`
			INSERT INTO %s (cache_key, response, expires_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(cache_key)
			DO UPDATE SET response = excluded.response, expires_at = excluded.expires_at, updated_at = excluded.updated_at
		`, s.table),
		key,
		string(payload),
		expiresAt.UTC().Format(time.RFC3339Nano),
		now,
	)
	if err != nil {
		return fmt.Errorf("write sqlite exact cache: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Prune(ctx context.Context, maxAge time.Duration, maxCount int) (int, error) {
	deleted := int64(0)
	now := time.Now().UTC()

	result, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE expires_at <= ?`, s.table),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return 0, fmt.Errorf("delete expired sqlite exact cache rows: %w", err)
	}
	count, _ := result.RowsAffected()
	deleted += count

	if maxAge > 0 {
		result, err := s.db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE updated_at < ?`, s.table),
			now.Add(-maxAge).Format(time.RFC3339Nano),
		)
		if err != nil {
			return 0, fmt.Errorf("delete aged sqlite exact cache rows: %w", err)
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
				LIMIT -1 OFFSET ?
			)
		`, s.table, s.table), maxCount)
		if err != nil {
			return 0, fmt.Errorf("enforce sqlite exact cache max count: %w", err)
		}
		count, _ := result.RowsAffected()
		deleted += count
	}

	return int(deleted), nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			cache_key TEXT PRIMARY KEY,
			response TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate sqlite exact cache: %w", err)
	}

	// Index name uses the unquoted table identifier — same convention as
	// internal/retention/history_sqlite.go.
	indexName := strings.Trim(s.table, `"`) + "_updated_at_idx"
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS "%s"
		ON %s (updated_at DESC)
	`, indexName, s.table))
	if err != nil {
		return fmt.Errorf("migrate sqlite exact cache index: %w", err)
	}
	return nil
}
