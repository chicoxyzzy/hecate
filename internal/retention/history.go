package retention

import (
	"context"
	"sync"
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

func NewMemoryHistoryStore() *MemoryHistoryStore {
	return &MemoryHistoryStore{
		runs: make([]HistoryRecord, 0, 16),
	}
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
