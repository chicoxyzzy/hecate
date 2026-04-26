package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hecate/agent-runtime/internal/storage"
)

type HistoryRecord struct {
	StartedAt  string            `json:"started_at"`
	FinishedAt string            `json:"finished_at"`
	Trigger    string            `json:"trigger"`
	Actor      string            `json:"actor,omitempty"`
	RequestID  string            `json:"request_id,omitempty"`
	Results    []SubsystemResult `json:"results"`
}

type HistoryStore interface {
	AppendRun(ctx context.Context, record HistoryRecord) error
	ListRuns(ctx context.Context, limit int) ([]HistoryRecord, error)
}

type MemoryHistoryStore struct {
	mu   sync.Mutex
	runs []HistoryRecord
}

type historyRedisClient interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
}

type RedisHistoryStore struct {
	client historyRedisClient
	key    string
	mu     sync.Mutex
}

type historyState struct {
	Runs []HistoryRecord `json:"runs"`
}

func NewMemoryHistoryStore() *MemoryHistoryStore {
	return &MemoryHistoryStore{
		runs: make([]HistoryRecord, 0, 16),
	}
}

func NewRedisHistoryStore(client *storage.RedisClient, prefix, key string) (*RedisHistoryStore, error) {
	return NewRedisHistoryStoreFromClient(client, prefix, key)
}

func NewRedisHistoryStoreFromClient(client historyRedisClient, prefix, key string) (*RedisHistoryStore, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client is required")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "retention-history"
	}
	if prefix != "" {
		key = prefix + ":" + key
	}

	store := &RedisHistoryStore{
		client: client,
		key:    key,
	}
	if _, err := store.readState(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *MemoryHistoryStore) AppendRun(_ context.Context, record HistoryRecord) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append([]HistoryRecord{cloneHistoryRecord(record)}, s.runs...)
	return nil
}

func (s *MemoryHistoryStore) ListRuns(_ context.Context, limit int) ([]HistoryRecord, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneHistoryRecords(limitHistoryRecords(s.runs, limit)), nil
}

func (s *RedisHistoryStore) AppendRun(ctx context.Context, record HistoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return err
	}
	state.Runs = append([]HistoryRecord{cloneHistoryRecord(record)}, state.Runs...)
	return s.writeState(ctx, state)
}

func (s *RedisHistoryStore) ListRuns(ctx context.Context, limit int) ([]HistoryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, err := s.readState(ctx)
	if err != nil {
		return nil, err
	}
	return cloneHistoryRecords(limitHistoryRecords(state.Runs, limit)), nil
}

func (s *RedisHistoryStore) readState(ctx context.Context) (historyState, error) {
	payload, err := s.client.Get(ctx, s.key)
	if errors.Is(err, storage.ErrNil) {
		state := historyState{Runs: make([]HistoryRecord, 0, 16)}
		if writeErr := s.writeState(ctx, state); writeErr != nil {
			return historyState{}, writeErr
		}
		return state, nil
	}
	if err != nil {
		return historyState{}, fmt.Errorf("read retention history redis state: %w", err)
	}
	var state historyState
	if err := json.Unmarshal(payload, &state); err != nil {
		return historyState{}, fmt.Errorf("decode retention history redis state: %w", err)
	}
	if state.Runs == nil {
		state.Runs = make([]HistoryRecord, 0, 16)
	}
	return state, nil
}

func (s *RedisHistoryStore) writeState(ctx context.Context, state historyState) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode retention history redis state: %w", err)
	}
	if err := s.client.Set(ctx, s.key, payload); err != nil {
		return fmt.Errorf("write retention history redis state: %w", err)
	}
	return nil
}

func cloneHistoryRecords(records []HistoryRecord) []HistoryRecord {
	if len(records) == 0 {
		return nil
	}
	out := make([]HistoryRecord, 0, len(records))
	for _, record := range records {
		out = append(out, cloneHistoryRecord(record))
	}
	return out
}

func cloneHistoryRecord(record HistoryRecord) HistoryRecord {
	record.Results = cloneSubsystemResults(record.Results)
	return record
}

func cloneSubsystemResults(items []SubsystemResult) []SubsystemResult {
	if len(items) == 0 {
		return nil
	}
	out := make([]SubsystemResult, len(items))
	copy(out, items)
	return out
}

func limitHistoryRecords(records []HistoryRecord, limit int) []HistoryRecord {
	if limit <= 0 || limit >= len(records) {
		return records
	}
	return records[:limit]
}
