package providers

import (
	"context"
	"sync"
	"time"
)

const maxHealthHistoryListLimit = 1_000

type HealthHistoryRecord struct {
	Provider            string
	Model               string
	Event               string
	Status              string
	Available           bool
	Error               string
	ErrorClass          string
	Reason              string
	RequestID           string
	TraceID             string
	PeerProvider        string
	PeerModel           string
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

type HealthHistoryPruner interface {
	Prune(ctx context.Context, maxAge time.Duration, maxCount int) (int, error)
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

func (s *MemoryHealthHistoryStore) Prune(_ context.Context, maxAge time.Duration, maxCount int) (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	original := len(s.records)
	if maxAge > 0 {
		cutoff := time.Now().UTC().Add(-maxAge)
		filtered := s.records[:0]
		for _, record := range s.records {
			ts, err := time.Parse(time.RFC3339Nano, record.Timestamp)
			if err != nil || ts.Before(cutoff) {
				continue
			}
			filtered = append(filtered, record)
		}
		s.records = append([]HealthHistoryRecord(nil), filtered...)
	}
	if maxCount > 0 && len(s.records) > maxCount {
		s.records = append([]HealthHistoryRecord(nil), s.records[:maxCount]...)
	}
	return original - len(s.records), nil
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
