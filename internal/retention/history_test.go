package retention

import (
	"context"
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
