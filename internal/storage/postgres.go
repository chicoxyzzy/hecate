package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PostgresConfig struct {
	DSN          string
	Schema       string
	TablePrefix  string
	MaxOpenConns int
	MaxIdleConns int
}

type PostgresClient struct {
	db          *sql.DB
	schema      string
	tablePrefix string
}

var identifierPattern = regexp.MustCompile(`[^a-z0-9_]+`)

func NewPostgresClient(ctx context.Context, cfg PostgresConfig) (*PostgresClient, error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	client := &PostgresClient{
		db:          db,
		schema:      sanitizeIdentifier(cfg.Schema, "public"),
		tablePrefix: sanitizeIdentifier(cfg.TablePrefix, "hecate"),
	}
	if err := client.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return client, nil
}

func (c *PostgresClient) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *PostgresClient) DB() *sql.DB {
	if c == nil {
		return nil
	}
	return c.db
}

func (c *PostgresClient) EnsureSchema(ctx context.Context) error {
	if c == nil || c.db == nil {
		return fmt.Errorf("postgres client is not initialized")
	}
	_, err := c.db.ExecContext(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, c.schema))
	if err != nil {
		return fmt.Errorf("create postgres schema: %w", err)
	}
	return nil
}

func (c *PostgresClient) QualifiedTable(name string) string {
	return fmt.Sprintf(`"%s"."%s"`, c.schema, c.TableName(name))
}

func (c *PostgresClient) TableName(name string) string {
	base := sanitizeIdentifier(name, "table")
	if c.tablePrefix == "" {
		return base
	}
	return c.tablePrefix + "_" + base
}

func sanitizeIdentifier(value, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	value = identifierPattern.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return fallback
	}
	return value
}
