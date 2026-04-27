package retention

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hecate/agent-runtime/internal/storage"
)

// SQLiteHistoryStore mirrors PostgresHistoryStore — same Append/List
// surface, same JSON-of-results column shape — so the gateway can swap
// retention backends purely via config without touching call sites.
//
// Differences from the Postgres flavor that aren't accidental:
//   - results column is TEXT (SQLite has no JSONB; JSON1 functions still
//     work over plain TEXT for any future querying needs).
//   - id column is `INTEGER PRIMARY KEY AUTOINCREMENT` instead of
//     BIGSERIAL — the SQLite idiom for monotonic row ids.
//   - placeholders are `?` rather than `$N`.
type SQLiteHistoryStore struct {
	db    *sql.DB
	table string
}

func NewSQLiteHistoryStore(ctx context.Context, client *storage.SQLiteClient, tableName string) (*SQLiteHistoryStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("sqlite client is required")
	}
	tableName = strings.TrimSpace(tableName)
	if tableName == "" {
		tableName = "retention_runs"
	}

	store := &SQLiteHistoryStore{
		db:    client.DB(),
		table: client.QualifiedTable(tableName),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteHistoryStore) AppendRun(ctx context.Context, record HistoryRecord) error {
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
		) VALUES (?, ?, ?, ?, ?, ?)
	`, s.table),
		record.StartedAt,
		record.FinishedAt,
		record.Trigger,
		record.Actor,
		record.RequestID,
		string(resultsJSON),
	)
	if err != nil {
		return fmt.Errorf("insert sqlite retention history: %w", err)
	}
	return nil
}

func (s *SQLiteHistoryStore) ListRuns(ctx context.Context, limit int) ([]HistoryRecord, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > maxHistoryListLimit {
		limit = maxHistoryListLimit
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT started_at, finished_at, trigger, actor, request_id, results_json
		FROM %s
		ORDER BY finished_at DESC, id DESC
		LIMIT ?
	`, s.table), limit)
	if err != nil {
		return nil, fmt.Errorf("list sqlite retention history: %w", err)
	}
	defer rows.Close()

	// Pre-allocate to the constant cap (see Postgres-store note for the
	// CodeQL rationale — same applies here).
	records := make([]HistoryRecord, 0, maxHistoryListLimit)
	for rows.Next() {
		var record HistoryRecord
		var resultsJSON sql.NullString
		if err := rows.Scan(
			&record.StartedAt,
			&record.FinishedAt,
			&record.Trigger,
			&record.Actor,
			&record.RequestID,
			&resultsJSON,
		); err != nil {
			return nil, fmt.Errorf("scan sqlite retention history: %w", err)
		}
		if resultsJSON.Valid && resultsJSON.String != "" {
			if err := json.Unmarshal([]byte(resultsJSON.String), &record.Results); err != nil {
				return nil, fmt.Errorf("decode sqlite retention history results: %w", err)
			}
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite retention history: %w", err)
	}
	return records, nil
}

func (s *SQLiteHistoryStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			started_at TEXT NOT NULL,
			finished_at TEXT NOT NULL,
			trigger TEXT NOT NULL,
			actor TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			results_json TEXT NOT NULL
		)
	`, s.table))
	if err != nil {
		return fmt.Errorf("migrate sqlite retention history store: %w", err)
	}

	// Index name uses the unquoted table identifier (SQLite tolerates
	// quoted index names but the convention across the rest of the
	// codebase is unquoted index identifiers paired with a quoted
	// target table).
	indexName := strings.Trim(s.table, `"`) + "_finished_at_idx"
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS "%s"
		ON %s (finished_at DESC, id DESC)
	`, indexName, s.table))
	if err != nil {
		return fmt.Errorf("migrate sqlite retention history index: %w", err)
	}
	return nil
}
