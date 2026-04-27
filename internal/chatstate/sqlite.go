package chatstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

// SQLiteStore mirrors PostgresStore — same Store-interface surface, same
// sessions/turns table shape — so the gateway can swap chat-session
// backends purely via config without touching call sites.
//
// Differences from the Postgres flavor that aren't accidental:
//   - placeholders are `?` rather than `$N`.
//   - timestamp columns are TEXT (SQLite has no TIMESTAMPTZ); the driver
//     handles time.Time round-tripping via RFC3339-style encoding.
//   - cascade-on-delete is declared on the turns table foreign key
//     (PRAGMA foreign_keys = ON is set by SQLiteClient on every conn).
//   - no schema namespacing — QualifiedTable returns "hecate_<name>".
type SQLiteStore struct {
	client        *storage.SQLiteClient
	sessionsTable string
	turnsTable    string
}

func NewSQLiteStore(ctx context.Context, client *storage.SQLiteClient) (*SQLiteStore, error) {
	if client == nil || client.DB() == nil {
		return nil, fmt.Errorf("sqlite client is required")
	}
	store := &SQLiteStore{
		client:        client,
		sessionsTable: client.QualifiedTable("chat_sessions"),
		turnsTable:    client.QualifiedTable("chat_session_turns"),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Backend() string {
	return "sqlite"
}

func (s *SQLiteStore) CreateSession(ctx context.Context, session types.ChatSession) (types.ChatSession, error) {
	if session.ID == "" {
		return types.ChatSession{}, fmt.Errorf("session id is required")
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}
	session.UpdatedAt = session.CreatedAt
	_, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s (id, title, system_prompt, tenant, user_name, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE
			 SET title = excluded.title,
			     system_prompt = excluded.system_prompt,
			     tenant = excluded.tenant,
			     user_name = excluded.user_name,
			     updated_at = excluded.updated_at`,
			s.sessionsTable,
		),
		session.ID,
		session.Title,
		session.SystemPrompt,
		session.Tenant,
		session.User,
		session.CreatedAt.UTC(),
		session.UpdatedAt.UTC(),
	)
	if err != nil {
		return types.ChatSession{}, fmt.Errorf("write sqlite chat session: %w", err)
	}
	return s.loadSession(ctx, session.ID)
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (types.ChatSession, bool, error) {
	session, err := s.loadSession(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.ChatSession{}, false, nil
		}
		return types.ChatSession{}, false, err
	}
	return session, true, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, filter Filter) ([]types.ChatSession, error) {
	query := fmt.Sprintf(`SELECT id, title, system_prompt, tenant, user_name, created_at, updated_at FROM %s`, s.sessionsTable)
	args := make([]any, 0, 3)
	if filter.Tenant != "" {
		query += ` WHERE tenant = ?`
		args = append(args, filter.Tenant)
	}
	query += ` ORDER BY updated_at DESC, created_at DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		// SQLite requires LIMIT before OFFSET; if no explicit limit was
		// requested but an offset is, fall back to a sentinel large limit
		// (matching SQLite docs' recommended `LIMIT -1` for "all rows").
		if filter.Limit <= 0 {
			query += ` LIMIT -1`
		}
		query += ` OFFSET ?`
		args = append(args, filter.Offset)
	}
	rows, err := s.client.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sqlite chat sessions: %w", err)
	}
	defer rows.Close()

	var items []types.ChatSession
	for rows.Next() {
		var session types.ChatSession
		if err := rows.Scan(&session.ID, &session.Title, &session.SystemPrompt, &session.Tenant, &session.User, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sqlite chat session: %w", err)
		}
		items = append(items, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite chat sessions: %w", err)
	}
	return items, nil
}

func (s *SQLiteStore) AppendTurn(ctx context.Context, sessionID string, turn types.ChatSessionTurn) (types.ChatSession, error) {
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now().UTC()
	}
	_, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(
			`INSERT INTO %s (
				id, session_id, request_id, user_role, user_content, assistant_role, assistant_content,
				requested_provider, provider, provider_kind, requested_model, model,
				cost_micros_usd, prompt_tokens, completion_tokens, total_tokens, created_at
			) VALUES (
				?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?, ?
			)`,
			s.turnsTable,
		),
		turn.ID,
		sessionID,
		turn.RequestID,
		turn.UserMessage.Role,
		turn.UserMessage.Content,
		turn.AssistantMessage.Role,
		turn.AssistantMessage.Content,
		turn.RequestedProvider,
		turn.Provider,
		turn.ProviderKind,
		turn.RequestedModel,
		turn.Model,
		turn.CostMicrosUSD,
		turn.PromptTokens,
		turn.CompletionTokens,
		turn.TotalTokens,
		turn.CreatedAt.UTC(),
	)
	if err != nil {
		return types.ChatSession{}, fmt.Errorf("append sqlite chat turn: %w", err)
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`UPDATE %s SET updated_at = ? WHERE id = ?`, s.sessionsTable),
		turn.CreatedAt.UTC(),
		sessionID,
	); err != nil {
		return types.ChatSession{}, fmt.Errorf("update sqlite chat session timestamp: %w", err)
	}
	return s.loadSession(ctx, sessionID)
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	// Foreign key with ON DELETE CASCADE on the turns table handles the
	// child rows; we still issue an explicit DELETE on turns first as a
	// belt-and-braces guard for environments where PRAGMA foreign_keys
	// somehow drifted off (the Postgres flavor does the same).
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE session_id = ?`, s.turnsTable),
		id,
	); err != nil {
		return fmt.Errorf("delete sqlite chat session turns: %w", err)
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, s.sessionsTable),
		id,
	); err != nil {
		return fmt.Errorf("delete sqlite chat session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateSession(ctx context.Context, id string, title string) (types.ChatSession, error) {
	now := time.Now().UTC()
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`UPDATE %s SET title = ?, updated_at = ? WHERE id = ?`, s.sessionsTable),
		title, now, id,
	); err != nil {
		return types.ChatSession{}, fmt.Errorf("update sqlite chat session: %w", err)
	}
	return s.loadSession(ctx, id)
}

func (s *SQLiteStore) UpdateSessionSystemPrompt(ctx context.Context, id string, prompt string) (types.ChatSession, error) {
	now := time.Now().UTC()
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`UPDATE %s SET system_prompt = ?, updated_at = ? WHERE id = ?`, s.sessionsTable),
		prompt, now, id,
	); err != nil {
		return types.ChatSession{}, fmt.Errorf("update sqlite chat session system prompt: %w", err)
	}
	return s.loadSession(ctx, id)
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL,
				system_prompt TEXT NOT NULL DEFAULT '',
				tenant TEXT NOT NULL,
				user_name TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL,
				updated_at TIMESTAMP NOT NULL
			)`,
			s.sessionsTable,
		),
	); err != nil {
		return fmt.Errorf("migrate sqlite chat sessions: %w", err)
	}
	// Backfill column for databases that pre-date system_prompt. SQLite
	// has no `ADD COLUMN IF NOT EXISTS`, so we probe PRAGMA table_info
	// and ALTER only when missing.
	hasSystemPrompt, err := s.columnExists(ctx, s.sessionsTable, "system_prompt")
	if err != nil {
		return fmt.Errorf("inspect sqlite chat sessions columns: %w", err)
	}
	if !hasSystemPrompt {
		if _, err := s.client.DB().ExecContext(
			ctx,
			fmt.Sprintf(`ALTER TABLE %s ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''`, s.sessionsTable),
		); err != nil {
			return fmt.Errorf("migrate sqlite chat sessions system_prompt: %w", err)
		}
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (
				id TEXT PRIMARY KEY,
				session_id TEXT NOT NULL REFERENCES %s (id) ON DELETE CASCADE,
				request_id TEXT NOT NULL,
				user_role TEXT NOT NULL,
				user_content TEXT NOT NULL,
				assistant_role TEXT NOT NULL,
				assistant_content TEXT NOT NULL,
				requested_provider TEXT NOT NULL,
				provider TEXT NOT NULL,
				provider_kind TEXT NOT NULL,
				requested_model TEXT NOT NULL,
				model TEXT NOT NULL,
				cost_micros_usd INTEGER NOT NULL,
				prompt_tokens INTEGER NOT NULL,
				completion_tokens INTEGER NOT NULL,
				total_tokens INTEGER NOT NULL,
				created_at TIMESTAMP NOT NULL
			)`,
			s.turnsTable,
			s.sessionsTable,
		),
	); err != nil {
		return fmt.Errorf("migrate sqlite chat session turns: %w", err)
	}

	// Index names use unquoted identifiers (matching the convention in
	// internal/retention/history_sqlite.go) paired with quoted target
	// tables.
	turnsIndex := strings.Trim(s.turnsTable, `"`) + "_session_created_idx"
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (session_id, created_at)`, turnsIndex, s.turnsTable),
	); err != nil {
		return fmt.Errorf("migrate sqlite chat session turns index: %w", err)
	}
	sessionsIndex := strings.Trim(s.sessionsTable, `"`) + "_tenant_updated_idx"
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS "%s" ON %s (tenant, updated_at)`, sessionsIndex, s.sessionsTable),
	); err != nil {
		return fmt.Errorf("migrate sqlite chat sessions tenant index: %w", err)
	}
	return nil
}

// columnExists checks PRAGMA table_info for the given column. The
// quotedTable argument is the already-quoted identifier as stored on the
// store (e.g. `"hecate_chat_sessions"`); we strip the quotes for the
// pragma form.
func (s *SQLiteStore) columnExists(ctx context.Context, quotedTable, column string) (bool, error) {
	bare := strings.Trim(quotedTable, `"`)
	rows, err := s.client.DB().QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, bare))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func (s *SQLiteStore) loadSession(ctx context.Context, id string) (types.ChatSession, error) {
	var session types.ChatSession
	err := s.client.DB().QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT id, title, system_prompt, tenant, user_name, created_at, updated_at FROM %s WHERE id = ?`, s.sessionsTable),
		id,
	).Scan(&session.ID, &session.Title, &session.SystemPrompt, &session.Tenant, &session.User, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.ChatSession{}, sql.ErrNoRows
		}
		return types.ChatSession{}, fmt.Errorf("read sqlite chat session: %w", err)
	}

	rows, err := s.client.DB().QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT id, request_id, user_role, user_content, assistant_role, assistant_content,
			        requested_provider, provider, provider_kind, requested_model, model,
			        cost_micros_usd, prompt_tokens, completion_tokens, total_tokens, created_at
			 FROM %s
			 WHERE session_id = ?
			 ORDER BY created_at ASC`,
			s.turnsTable,
		),
		id,
	)
	if err != nil {
		return types.ChatSession{}, fmt.Errorf("list sqlite chat turns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var turn types.ChatSessionTurn
		if err := rows.Scan(
			&turn.ID,
			&turn.RequestID,
			&turn.UserMessage.Role,
			&turn.UserMessage.Content,
			&turn.AssistantMessage.Role,
			&turn.AssistantMessage.Content,
			&turn.RequestedProvider,
			&turn.Provider,
			&turn.ProviderKind,
			&turn.RequestedModel,
			&turn.Model,
			&turn.CostMicrosUSD,
			&turn.PromptTokens,
			&turn.CompletionTokens,
			&turn.TotalTokens,
			&turn.CreatedAt,
		); err != nil {
			return types.ChatSession{}, fmt.Errorf("scan sqlite chat turn: %w", err)
		}
		session.Turns = append(session.Turns, turn)
	}
	if err := rows.Err(); err != nil {
		return types.ChatSession{}, fmt.Errorf("iterate sqlite chat turns: %w", err)
	}
	return session, nil
}
