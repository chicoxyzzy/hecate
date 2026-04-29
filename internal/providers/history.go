package providers

import (
	"context"
	"sync"
	"time"
)

const maxHealthHistoryListLimit = 1_000

type HealthHistoryRecord struct {
	Provider            string
	Event               string
	Status              string
	Available           bool
	Error               string
	ErrorClass          string
	LatencyMS           int64
	ConsecutiveFailures int
	TotalSuccesses      int64
	TotalFailures       int64
	Timeouts            int64
	ServerErrors        int64
	RateLimits          int64
	OpenUntil           string
	Timestamp           string
}

type HealthHistoryFilter struct {
	Provider string
	Limit    int
}

type HealthHistoryStore interface {
	Append(ctx context.Context, record HealthHistoryRecord) error
	List(ctx context.Context, filter HealthHistoryFilter) ([]HealthHistoryRecord, error)
}

type MemoryHealthHistoryStore struct {
	mu      sync.Mutex
	records []HealthHistoryRecord
}

func NewMemoryHealthHistoryStore() *MemoryHealthHistoryStore {
	return &MemoryHealthHistoryStore{
		records: make([]HealthHistoryRecord, 0, 32),
	}
}

func (s *MemoryHealthHistoryStore) Append(_ context.Context, record HealthHistoryRecord) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append([]HealthHistoryRecord{cloneHealthHistoryRecord(record)}, s.records...)
	return nil
}

func (s *MemoryHealthHistoryStore) List(_ context.Context, filter HealthHistoryFilter) ([]HealthHistoryRecord, error) {
	if s == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	limit := normalizeHealthHistoryLimit(filter.Limit)
	out := make([]HealthHistoryRecord, 0, limit)
	for _, record := range s.records {
		if filter.Provider != "" && record.Provider != filter.Provider {
			continue
		}
		out = append(out, cloneHealthHistoryRecord(record))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func normalizeHealthHistoryLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxHealthHistoryListLimit {
		return maxHealthHistoryListLimit
	}
	return limit
}

func cloneHealthHistoryRecord(record HealthHistoryRecord) HealthHistoryRecord {
	return record
}

func buildHealthHistoryRecord(provider, event string, state HealthState, now time.Time) HealthHistoryRecord {
	record := HealthHistoryRecord{
		Provider:            provider,
		Event:               event,
		Status:              string(state.Status),
		Available:           state.Available,
		Error:               state.LastError,
		ErrorClass:          state.LastErrorClass,
		LatencyMS:           state.LastLatency.Milliseconds(),
		ConsecutiveFailures: state.ConsecutiveFailures,
		TotalSuccesses:      state.TotalSuccesses,
		TotalFailures:       state.TotalFailures,
		Timeouts:            state.Timeouts,
		ServerErrors:        state.ServerErrors,
		RateLimits:          state.RateLimits,
		Timestamp:           now.UTC().Format(time.RFC3339Nano),
	}
	if !state.OpenUntil.IsZero() {
		record.OpenUntil = state.OpenUntil.UTC().Format(time.RFC3339Nano)
	}
	return record
}
