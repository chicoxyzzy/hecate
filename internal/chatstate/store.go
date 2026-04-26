package chatstate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

type Filter struct {
	Tenant string
	Limit  int
	Offset int
}

type Store interface {
	Backend() string
	CreateSession(ctx context.Context, session types.ChatSession) (types.ChatSession, error)
	GetSession(ctx context.Context, id string) (types.ChatSession, bool, error)
	ListSessions(ctx context.Context, filter Filter) ([]types.ChatSession, error)
	AppendTurn(ctx context.Context, sessionID string, turn types.ChatSessionTurn) (types.ChatSession, error)
	DeleteSession(ctx context.Context, id string) error
	UpdateSession(ctx context.Context, id string, title string) (types.ChatSession, error)
}

type MemoryStore struct {
	mu       sync.Mutex
	sessions map[string]types.ChatSession
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sessions: make(map[string]types.ChatSession)}
}

func (s *MemoryStore) Backend() string {
	return "memory"
}

func (s *MemoryStore) CreateSession(_ context.Context, session types.ChatSession) (types.ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session.ID == "" {
		return types.ChatSession{}, fmt.Errorf("session id is required")
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}
	session.UpdatedAt = session.CreatedAt
	session.Turns = append([]types.ChatSessionTurn(nil), session.Turns...)
	s.sessions[session.ID] = session
	return cloneSession(session), nil
}

func (s *MemoryStore) GetSession(_ context.Context, id string) (types.ChatSession, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return types.ChatSession{}, false, nil
	}
	return cloneSession(session), true, nil
}

func (s *MemoryStore) ListSessions(_ context.Context, filter Filter) ([]types.ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	items := make([]types.ChatSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if filter.Tenant != "" && session.Tenant != filter.Tenant {
			continue
		}
		cloned := cloneSession(session)
		cloned.Turns = nil
		items = append(items, cloned)
	}
	sortSessionsDesc(items)
	if filter.Offset > 0 {
		if filter.Offset >= len(items) {
			return nil, nil
		}
		items = items[filter.Offset:]
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return items, nil
}

func (s *MemoryStore) AppendTurn(_ context.Context, sessionID string, turn types.ChatSessionTurn) (types.ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[sessionID]
	if !ok {
		return types.ChatSession{}, fmt.Errorf("chat session %q not found", sessionID)
	}
	if turn.CreatedAt.IsZero() {
		turn.CreatedAt = time.Now().UTC()
	}
	session.Turns = append(session.Turns, turn)
	session.UpdatedAt = turn.CreatedAt
	s.sessions[sessionID] = session
	return cloneSession(session), nil
}

func (s *MemoryStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return fmt.Errorf("chat session %q not found", id)
	}
	delete(s.sessions, id)
	return nil
}

func (s *MemoryStore) UpdateSession(_ context.Context, id string, title string) (types.ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return types.ChatSession{}, fmt.Errorf("chat session %q not found", id)
	}
	session.Title = title
	session.UpdatedAt = time.Now().UTC()
	s.sessions[id] = session
	return cloneSession(session), nil
}

type PostgresStore struct {
	client        *storage.PostgresClient
	sessionsTable string
	turnsTable    string
}

func NewPostgresStore(ctx context.Context, client *storage.PostgresClient) (*PostgresStore, error) {
	if client == nil {
		return nil, fmt.Errorf("postgres client is required")
	}
	store := &PostgresStore{
		client:        client,
		sessionsTable: client.QualifiedTable("chat_sessions"),
		turnsTable:    client.QualifiedTable("chat_session_turns"),
	}
	if err := store.migrate(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *PostgresStore) Backend() string {
	return "postgres"
}

func (s *PostgresStore) CreateSession(ctx context.Context, session types.ChatSession) (types.ChatSession, error) {
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
			`INSERT INTO %s (id, title, tenant, user_name, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (id) DO UPDATE
			 SET title = EXCLUDED.title,
			     tenant = EXCLUDED.tenant,
			     user_name = EXCLUDED.user_name,
			     updated_at = EXCLUDED.updated_at`,
			s.sessionsTable,
		),
		session.ID,
		session.Title,
		session.Tenant,
		session.User,
		session.CreatedAt.UTC(),
		session.UpdatedAt.UTC(),
	)
	if err != nil {
		return types.ChatSession{}, fmt.Errorf("write postgres chat session: %w", err)
	}
	return s.loadSession(ctx, session.ID)
}

func (s *PostgresStore) GetSession(ctx context.Context, id string) (types.ChatSession, bool, error) {
	session, err := s.loadSession(ctx, id)
	if err != nil {
		if err == storage.ErrNil {
			return types.ChatSession{}, false, nil
		}
		return types.ChatSession{}, false, err
	}
	return session, true, nil
}

func (s *PostgresStore) ListSessions(ctx context.Context, filter Filter) ([]types.ChatSession, error) {
	query := fmt.Sprintf(`SELECT id, title, tenant, user_name, created_at, updated_at FROM %s`, s.sessionsTable)
	args := make([]any, 0, 2)
	if filter.Tenant != "" {
		query += ` WHERE tenant = $1`
		args = append(args, filter.Tenant)
	}
	query += ` ORDER BY updated_at DESC, created_at DESC`
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(` LIMIT $%d`, len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(` OFFSET $%d`, len(args))
	}
	rows, err := s.client.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list postgres chat sessions: %w", err)
	}
	defer rows.Close()

	var items []types.ChatSession
	for rows.Next() {
		var session types.ChatSession
		if err := rows.Scan(&session.ID, &session.Title, &session.Tenant, &session.User, &session.CreatedAt, &session.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan postgres chat session: %w", err)
		}
		items = append(items, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate postgres chat sessions: %w", err)
	}
	return items, nil
}

func (s *PostgresStore) AppendTurn(ctx context.Context, sessionID string, turn types.ChatSessionTurn) (types.ChatSession, error) {
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
				$1, $2, $3, $4, $5, $6, $7,
				$8, $9, $10, $11, $12,
				$13, $14, $15, $16, $17
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
		return types.ChatSession{}, fmt.Errorf("append postgres chat turn: %w", err)
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`UPDATE %s SET updated_at = $2 WHERE id = $1`, s.sessionsTable),
		sessionID,
		turn.CreatedAt.UTC(),
	); err != nil {
		return types.ChatSession{}, fmt.Errorf("update postgres chat session timestamp: %w", err)
	}
	return s.loadSession(ctx, sessionID)
}

func (s *PostgresStore) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.turnsTable),
		id,
	); err != nil {
		return fmt.Errorf("delete postgres chat session turns: %w", err)
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, s.sessionsTable),
		id,
	); err != nil {
		return fmt.Errorf("delete postgres chat session: %w", err)
	}
	return nil
}

func (s *PostgresStore) UpdateSession(ctx context.Context, id string, title string) (types.ChatSession, error) {
	now := time.Now().UTC()
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`UPDATE %s SET title = $1, updated_at = $2 WHERE id = $3`, s.sessionsTable),
		title, now, id,
	); err != nil {
		return types.ChatSession{}, fmt.Errorf("update postgres chat session: %w", err)
	}
	return s.loadSession(ctx, id)
}

func (s *PostgresStore) migrate(ctx context.Context) error {
	if err := s.client.EnsureSchema(ctx); err != nil {
		return err
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (
				id TEXT PRIMARY KEY,
				title TEXT NOT NULL,
				tenant TEXT NOT NULL,
				user_name TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL,
				updated_at TIMESTAMPTZ NOT NULL
			)`,
			s.sessionsTable,
		),
	); err != nil {
		return fmt.Errorf("migrate postgres chat sessions: %w", err)
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
				cost_micros_usd BIGINT NOT NULL,
				prompt_tokens INTEGER NOT NULL,
				completion_tokens INTEGER NOT NULL,
				total_tokens INTEGER NOT NULL,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			s.turnsTable,
			s.sessionsTable,
		),
	); err != nil {
		return fmt.Errorf("migrate postgres chat session turns: %w", err)
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (session_id, created_at)`, s.client.TableName("chat_session_turns_session_created_idx"), s.turnsTable),
	); err != nil {
		return fmt.Errorf("migrate postgres chat session turns index: %w", err)
	}
	if _, err := s.client.DB().ExecContext(
		ctx,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tenant, updated_at)`, s.client.TableName("chat_sessions_tenant_updated_idx"), s.sessionsTable),
	); err != nil {
		return fmt.Errorf("migrate postgres chat sessions tenant index: %w", err)
	}
	return nil
}

func (s *PostgresStore) loadSession(ctx context.Context, id string) (types.ChatSession, error) {
	var session types.ChatSession
	err := s.client.DB().QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT id, title, tenant, user_name, created_at, updated_at FROM %s WHERE id = $1`, s.sessionsTable),
		id,
	).Scan(&session.ID, &session.Title, &session.Tenant, &session.User, &session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return types.ChatSession{}, storage.ErrNil
		}
		return types.ChatSession{}, fmt.Errorf("read postgres chat session: %w", err)
	}

	rows, err := s.client.DB().QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT id, request_id, user_role, user_content, assistant_role, assistant_content,
			        requested_provider, provider, provider_kind, requested_model, model,
			        cost_micros_usd, prompt_tokens, completion_tokens, total_tokens, created_at
			 FROM %s
			 WHERE session_id = $1
			 ORDER BY created_at ASC`,
			s.turnsTable,
		),
		id,
	)
	if err != nil {
		return types.ChatSession{}, fmt.Errorf("list postgres chat turns: %w", err)
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
			return types.ChatSession{}, fmt.Errorf("scan postgres chat turn: %w", err)
		}
		session.Turns = append(session.Turns, turn)
	}
	if err := rows.Err(); err != nil {
		return types.ChatSession{}, fmt.Errorf("iterate postgres chat turns: %w", err)
	}
	return session, nil
}

func cloneSession(session types.ChatSession) types.ChatSession {
	cloned := session
	cloned.Turns = append([]types.ChatSessionTurn(nil), session.Turns...)
	return cloned
}

func sortSessionsDesc(items []types.ChatSession) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
}
