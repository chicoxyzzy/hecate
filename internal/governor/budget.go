package governor

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
)

type UsageEvent struct {
	BudgetKey  string
	RequestID  string
	Tenant     string
	Provider   string
	Model      string
	CostMicros int64
	OccurredAt time.Time
}

type UsageLedger interface {
	Current(ctx context.Context, key string) (int64, error)
	Record(ctx context.Context, event UsageEvent) error
	Reset(ctx context.Context, key string) error
}

type BudgetEvent struct {
	Key              string    `json:"key"`
	Type             string    `json:"type"`
	Scope            string    `json:"scope,omitempty"`
	Provider         string    `json:"provider,omitempty"`
	Tenant           string    `json:"tenant,omitempty"`
	Model            string    `json:"model,omitempty"`
	RequestID        string    `json:"request_id,omitempty"`
	Actor            string    `json:"actor,omitempty"`
	Detail           string    `json:"detail,omitempty"`
	AmountMicrosUSD  int64     `json:"amount_micros_usd"`
	BalanceMicrosUSD int64     `json:"balance_micros_usd"`
	LimitMicrosUSD   int64     `json:"limit_micros_usd"`
	OccurredAt       time.Time `json:"occurred_at"`
}

type BudgetHistoryStore interface {
	AppendEvent(ctx context.Context, event BudgetEvent) error
	ListEvents(ctx context.Context, key string, limit int) ([]BudgetEvent, error)
	PruneEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
}

type BudgetStateStore interface {
	Limit(ctx context.Context, key string) (int64, error)
	SetLimit(ctx context.Context, key string, value int64) error
	AddLimit(ctx context.Context, key string, delta int64) error
}

type BudgetStore interface {
	UsageLedger
	BudgetStateStore
	BudgetHistoryStore
}

type MemoryBudgetStore struct {
	mu     sync.Mutex
	spent  map[string]int64
	limits map[string]int64
	events map[string][]BudgetEvent
}

func NewMemoryBudgetStore() *MemoryBudgetStore {
	return &MemoryBudgetStore{
		spent:  make(map[string]int64),
		limits: make(map[string]int64),
		events: make(map[string][]BudgetEvent),
	}
}

func (s *MemoryBudgetStore) Current(_ context.Context, key string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spent[key], nil
}

func (s *MemoryBudgetStore) Record(_ context.Context, event UsageEvent) error {
	if event.BudgetKey == "" || event.CostMicros <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spent[event.BudgetKey] += event.CostMicros
	return nil
}

func (s *MemoryBudgetStore) Reset(_ context.Context, key string) error {
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

func (s *MemoryBudgetStore) AppendEvent(_ context.Context, event BudgetEvent) error {
	if event.Key == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	events := append(s.events[event.Key], event)
	if len(events) > 200 {
		events = append([]BudgetEvent(nil), events[len(events)-200:]...)
	}
	s.events[event.Key] = events
	return nil
}

func (s *MemoryBudgetStore) ListEvents(_ context.Context, key string, limit int) ([]BudgetEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := s.events[key]
	if limit <= 0 || limit > len(events) {
		limit = len(events)
	}

	out := make([]BudgetEvent, 0, limit)
	for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, events[i])
	}
	return out, nil
}

func (s *MemoryBudgetStore) PruneEvents(_ context.Context, maxAge time.Duration, maxCount int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	deleted := 0
	for key, events := range s.events {
		kept := events[:0]
		for _, event := range events {
			if maxAge > 0 && !event.OccurredAt.IsZero() && event.OccurredAt.Before(now.Add(-maxAge)) {
				deleted++
				continue
			}
			kept = append(kept, event)
		}
		if maxCount > 0 && len(kept) > maxCount {
			deleted += len(kept) - maxCount
			kept = append([]BudgetEvent(nil), kept[len(kept)-maxCount:]...)
		}
		s.events[key] = append([]BudgetEvent(nil), kept...)
	}
	return deleted, nil
}

type RedisBudgetStore struct {
	client *storage.RedisClient
	prefix string
}

func NewRedisBudgetStore(client *storage.RedisClient, prefix string) *RedisBudgetStore {
	return &RedisBudgetStore{client: client, prefix: prefix}
}

func (s *RedisBudgetStore) Current(ctx context.Context, key string) (int64, error) {
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

func (s *RedisBudgetStore) Record(ctx context.Context, event UsageEvent) error {
	if event.BudgetKey == "" || event.CostMicros <= 0 {
		return nil
	}
	_, err := s.client.IncrBy(ctx, s.spentKey(event.BudgetKey), event.CostMicros)
	return err
}

func (s *RedisBudgetStore) Reset(ctx context.Context, key string) error {
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

func (s *RedisBudgetStore) AppendEvent(ctx context.Context, event BudgetEvent) error {
	if event.Key == "" {
		return nil
	}

	events, err := s.readEvents(ctx, event.Key)
	if err != nil {
		return err
	}
	events = append(events, event)
	if len(events) > 200 {
		events = append([]BudgetEvent(nil), events[len(events)-200:]...)
	}

	payload, err := json.Marshal(events)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.historyKey(event.Key), payload)
}

func (s *RedisBudgetStore) ListEvents(ctx context.Context, key string, limit int) ([]BudgetEvent, error) {
	events, err := s.readEvents(ctx, key)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > len(events) {
		limit = len(events)
	}

	out := make([]BudgetEvent, 0, limit)
	for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, events[i])
	}
	return out, nil
}

func (s *RedisBudgetStore) PruneEvents(ctx context.Context, maxAge time.Duration, maxCount int) (int, error) {
	keys, err := s.client.Keys(ctx, s.redisKey("*:history"))
	if err != nil {
		return 0, err
	}
	now := time.Now()
	deleted := 0
	for _, redisKey := range keys {
		payload, err := s.client.Get(ctx, redisKey)
		if err != nil {
			continue
		}
		var events []BudgetEvent
		if err := json.Unmarshal(payload, &events); err != nil {
			continue
		}
		kept := events[:0]
		for _, event := range events {
			if maxAge > 0 && !event.OccurredAt.IsZero() && event.OccurredAt.Before(now.Add(-maxAge)) {
				deleted++
				continue
			}
			kept = append(kept, event)
		}
		if maxCount > 0 && len(kept) > maxCount {
			deleted += len(kept) - maxCount
			kept = append([]BudgetEvent(nil), kept[len(kept)-maxCount:]...)
		}
		next, err := json.Marshal(kept)
		if err != nil {
			return deleted, err
		}
		if err := s.client.Set(ctx, redisKey, next); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
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

func (s *RedisBudgetStore) historyKey(key string) string {
	return s.redisKey(key) + ":history"
}

func (s *RedisBudgetStore) readEvents(ctx context.Context, key string) ([]BudgetEvent, error) {
	payload, err := s.client.Get(ctx, s.historyKey(key))
	if err != nil {
		if err == storage.ErrNil {
			return nil, nil
		}
		return nil, err
	}

	var events []BudgetEvent
	if err := json.Unmarshal(payload, &events); err != nil {
		return nil, err
	}
	return events, nil
}
