package governor

import (
	"context"
	"strconv"
	"sync"

	"github.com/hecate/agent-runtime/internal/storage"
)

type BudgetStore interface {
	Spent(ctx context.Context, key string) (int64, error)
	AddSpent(ctx context.Context, key string, delta int64) error
	ResetSpent(ctx context.Context, key string) error
	Limit(ctx context.Context, key string) (int64, error)
	SetLimit(ctx context.Context, key string, value int64) error
	AddLimit(ctx context.Context, key string, delta int64) error
}

type MemoryBudgetStore struct {
	mu     sync.Mutex
	spent  map[string]int64
	limits map[string]int64
}

func NewMemoryBudgetStore() *MemoryBudgetStore {
	return &MemoryBudgetStore{
		spent:  make(map[string]int64),
		limits: make(map[string]int64),
	}
}

func (s *MemoryBudgetStore) Spent(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spent[key], nil
}

func (s *MemoryBudgetStore) AddSpent(_ context.Context, key string, delta int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spent[key] += delta
	return nil
}

func (s *MemoryBudgetStore) ResetSpent(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.spent, key)
	return nil
}

func (s *MemoryBudgetStore) Limit(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.limits[key], nil
}

func (s *MemoryBudgetStore) SetLimit(_ context.Context, key string, value int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits[key] = value
	return nil
}

func (s *MemoryBudgetStore) AddLimit(_ context.Context, key string, delta int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits[key] += delta
	return nil
}

type RedisBudgetStore struct {
	client *storage.RedisClient
	prefix string
}

func NewRedisBudgetStore(client *storage.RedisClient, prefix string) *RedisBudgetStore {
	return &RedisBudgetStore{client: client, prefix: prefix}
}

func (s *RedisBudgetStore) Spent(ctx context.Context, key string) (int64, error) {
	payload, err := s.client.Get(ctx, s.spentKey(key))
	if err != nil {
		if err == storage.ErrNil {
			return 0, nil
		}
		return 0, err
	}
	value, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (s *RedisBudgetStore) AddSpent(ctx context.Context, key string, delta int64) error {
	_, err := s.client.IncrBy(ctx, s.spentKey(key), delta)
	return err
}

func (s *RedisBudgetStore) ResetSpent(ctx context.Context, key string) error {
	return s.client.SetEX(ctx, s.spentKey(key), 0, []byte("0"))
}

func (s *RedisBudgetStore) Limit(ctx context.Context, key string) (int64, error) {
	payload, err := s.client.Get(ctx, s.limitKey(key))
	if err != nil {
		if err == storage.ErrNil {
			return 0, nil
		}
		return 0, err
	}
	value, err := strconv.ParseInt(string(payload), 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}

func (s *RedisBudgetStore) SetLimit(ctx context.Context, key string, value int64) error {
	return s.client.SetEX(ctx, s.limitKey(key), 0, []byte(strconv.FormatInt(value, 10)))
}

func (s *RedisBudgetStore) AddLimit(ctx context.Context, key string, delta int64) error {
	_, err := s.client.IncrBy(ctx, s.limitKey(key), delta)
	return err
}

func (s *RedisBudgetStore) redisKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + ":budget:" + key
}

func (s *RedisBudgetStore) spentKey(key string) string {
	return s.redisKey(key) + ":spent"
}

func (s *RedisBudgetStore) limitKey(key string) string {
	return s.redisKey(key) + ":limit"
}
