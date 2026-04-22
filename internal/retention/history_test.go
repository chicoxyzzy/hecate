package retention

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMemoryHistoryStoreReturnsNewestFirst(t *testing.T) {
	t.Parallel()

	store := NewMemoryHistoryStore()
	if err := store.AppendRun(context.Background(), HistoryRecord{FinishedAt: "2026-04-22T10:00:00Z", Trigger: "manual"}); err != nil {
		t.Fatalf("AppendRun(first) error = %v", err)
	}
	if err := store.AppendRun(context.Background(), HistoryRecord{FinishedAt: "2026-04-22T11:00:00Z", Trigger: "scheduled"}); err != nil {
		t.Fatalf("AppendRun(second) error = %v", err)
	}

	runs, err := store.ListRuns(context.Background(), 1)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].Trigger != "scheduled" {
		t.Fatalf("trigger = %q, want scheduled", runs[0].Trigger)
	}
}

func TestFileHistoryStorePersistsRuns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "retention-history.json")
	store, err := NewFileHistoryStore(path)
	if err != nil {
		t.Fatalf("NewFileHistoryStore() error = %v", err)
	}

	record := HistoryRecord{
		StartedAt:  "2026-04-22T10:00:00Z",
		FinishedAt: "2026-04-22T10:00:05Z",
		Trigger:    "manual",
		Actor:      "admin:req-1",
		RequestID:  "req-1",
		Results: []SubsystemResult{
			{Name: SubsystemTraces, Deleted: 12, MaxCount: 2000},
		},
	}
	if err := store.AppendRun(context.Background(), record); err != nil {
		t.Fatalf("AppendRun() error = %v", err)
	}

	reloaded, err := NewFileHistoryStore(path)
	if err != nil {
		t.Fatalf("NewFileHistoryStore(reload) error = %v", err)
	}
	runs, err := reloaded.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0].RequestID != "req-1" {
		t.Fatalf("request_id = %q, want req-1", runs[0].RequestID)
	}
	if len(runs[0].Results) != 1 || runs[0].Results[0].Name != SubsystemTraces {
		t.Fatalf("results = %#v, want trace_snapshots record", runs[0].Results)
	}
}
