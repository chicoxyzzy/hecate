package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteConfig captures the on-disk path and connection knobs the
// SQLite-backed stores share. We keep TablePrefix here so multiple
// gateways pointing at the same file (rare, but supported in tests)
// don't collide. The driver name is "sqlite" — we use modernc.org/sqlite,
// the pure-Go translation of the SQLite C amalgamation, so the gateway
// stays a single static binary with no CGO requirement.
type SQLiteConfig struct {
	Path        string
	TablePrefix string
	BusyTimeout time.Duration
}

// SQLiteClient mirrors PostgresClient in shape — same Close/DB/QualifiedTable/
// TableName surface — so the per-subsystem stores can take either one
// without their callers caring. SQLite has no schemas, so QualifiedTable
// just returns the prefixed table name with no schema namespace.
type SQLiteClient struct {
	db          *sql.DB
	tablePrefix string
}

// NewSQLiteClient opens (and creates if missing) the SQLite database
// at cfg.Path. The parent directory is auto-created — operators expect
// `--sqlite-path .data/hecate.db` to Just Work without `mkdir -p` first.
//
// SQLite-specific tuning we apply on every connection:
//   - WAL journal mode: lets readers and one writer proceed concurrently
//     (default rollback journal blocks readers during writes — death for
//     a request-handling gateway).
//   - busy_timeout: SQLite's default behavior on a locked database is to
//     fail immediately; with WAL the lock window is short, but we still
//     need a timeout > 0 so concurrent transactions wait instead of
//     erroring out.
//   - foreign_keys ON: SQLite ships with FKs disabled by default, which
//     is a footgun for anyone porting Postgres-shaped schemas.
func NewSQLiteClient(ctx context.Context, cfg SQLiteConfig) (*SQLiteClient, error) {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory %q: %w", dir, err)
		}
	}

	busyMs := int64(5000)
	if cfg.BusyTimeout > 0 {
		busyMs = cfg.BusyTimeout.Milliseconds()
	}

	// _pragma= URL params are how modernc.org/sqlite accepts pragmas at
	// connection open time. Applied to every connection in the pool.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)",
		path, busyMs,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite tolerates many readers but only one writer at a time. A
	// large open-conn pool just means more goroutines fighting for the
	// write lock. Cap to a small number; the actual concurrency comes
	// from WAL letting reads pass through.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	return &SQLiteClient{
		db:          db,
		tablePrefix: sanitizeIdentifier(cfg.TablePrefix, "hecate"),
	}, nil
}

func (c *SQLiteClient) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *SQLiteClient) DB() *sql.DB {
	if c == nil {
		return nil
	}
	return c.db
}

// QualifiedTable returns the fully-qualified table name. SQLite has no
// schema namespacing, so this is just the prefixed table name wrapped
// in double quotes for safety against reserved-word collisions.
func (c *SQLiteClient) QualifiedTable(name string) string {
	return fmt.Sprintf(`"%s"`, c.TableName(name))
}

func (c *SQLiteClient) TableName(name string) string {
	base := sanitizeIdentifier(name, "table")
	if c.tablePrefix == "" {
		return base
	}
	return c.tablePrefix + "_" + base
}
