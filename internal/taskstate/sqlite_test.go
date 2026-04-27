package taskstate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/storage"
	"github.com/hecate/agent-runtime/pkg/types"
)

func newSQLiteTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	client, err := storage.NewSQLiteClient(context.Background(), storage.SQLiteConfig{
		Path:        filepath.Join(dir, "taskstate.db"),
		TablePrefix: "test",
	})
	if err != nil {
		t.Fatalf("NewSQLiteClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	store, err := NewSQLiteStore(context.Background(), client)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return store
}

func TestSQLiteStore_RejectsNilClient(t *testing.T) {
	_, err := NewSQLiteStore(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestSQLiteStore_BackendName(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	if got := store.Backend(); got != "sqlite" {
		t.Fatalf("Backend() = %q, want %q", got, "sqlite")
	}
}

func TestSQLiteStore_TaskRunStepRoundTrip(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	task := types.Task{
		ID:     "task-1",
		Title:  "demo",
		Tenant: "tenant-a",
		Status: "queued",
	}
	saved, err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if saved.CreatedAt.IsZero() {
		t.Fatal("CreateTask did not stamp CreatedAt")
	}

	got, ok, err := store.GetTask(ctx, "task-1")
	if err != nil || !ok {
		t.Fatalf("GetTask: ok=%v err=%v", ok, err)
	}
	if got.Title != "demo" || got.Tenant != "tenant-a" {
		t.Fatalf("GetTask round-trip mismatch: %+v", got)
	}

	run := types.TaskRun{
		ID:        "run-1",
		TaskID:    "task-1",
		Number:    1,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}
	if _, err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	gotRun, ok, err := store.GetRun(ctx, "task-1", "run-1")
	if err != nil || !ok {
		t.Fatalf("GetRun: ok=%v err=%v", ok, err)
	}
	if gotRun.Status != "running" || gotRun.Number != 1 {
		t.Fatalf("GetRun round-trip mismatch: %+v", gotRun)
	}

	for i, status := range []string{"running", "completed"} {
		step := types.TaskStep{
			ID:        "step-" + status,
			TaskID:    "task-1",
			RunID:     "run-1",
			Index:     i,
			Status:    status,
			StartedAt: time.Now().UTC(),
		}
		if _, err := store.AppendStep(ctx, step); err != nil {
			t.Fatalf("AppendStep(%s): %v", status, err)
		}
	}
	steps, err := store.ListSteps(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListSteps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("ListSteps len = %d, want 2", len(steps))
	}
	// step_index ASC ordering: index 0 first.
	if steps[0].Index != 0 || steps[1].Index != 1 {
		t.Fatalf("ListSteps ordering: %+v", steps)
	}
}

func TestSQLiteStore_ListTasksFilterAndLimit(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	// Three tasks, two tenants. updated_at staggered so ordering is
	// deterministic.
	now := time.Now().UTC()
	for i, spec := range []struct {
		id     string
		tenant string
		status string
		ts     time.Time
	}{
		{"t-a1", "alpha", "queued", now.Add(-3 * time.Minute)},
		{"t-a2", "alpha", "running", now.Add(-2 * time.Minute)},
		{"t-b1", "beta", "queued", now.Add(-1 * time.Minute)},
	} {
		_, err := store.CreateTask(ctx, types.Task{
			ID:        spec.id,
			Tenant:    spec.tenant,
			Status:    spec.status,
			CreatedAt: spec.ts,
			UpdatedAt: spec.ts,
		})
		if err != nil {
			t.Fatalf("CreateTask[%d]: %v", i, err)
		}
	}

	all, err := store.ListTasks(ctx, TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("ListTasks all len = %d, want 3", len(all))
	}
	// updated_at DESC: t-b1 first.
	if all[0].ID != "t-b1" {
		t.Fatalf("ListTasks ordering: got first %q, want t-b1", all[0].ID)
	}

	tenanted, err := store.ListTasks(ctx, TaskFilter{Tenant: "alpha"})
	if err != nil {
		t.Fatalf("ListTasks(tenant): %v", err)
	}
	if len(tenanted) != 2 {
		t.Fatalf("ListTasks tenant len = %d, want 2", len(tenanted))
	}

	limited, err := store.ListTasks(ctx, TaskFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListTasks(limit): %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("ListTasks limit len = %d, want 2", len(limited))
	}

	statused, err := store.ListTasks(ctx, TaskFilter{Status: "queued"})
	if err != nil {
		t.Fatalf("ListTasks(status): %v", err)
	}
	if len(statused) != 2 {
		t.Fatalf("ListTasks status len = %d, want 2", len(statused))
	}
}

func TestSQLiteStore_ApprovalRoundTrip(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateTask(ctx, types.Task{ID: "task-ap", Status: "running"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	approval := types.TaskApproval{
		ID:          "ap-1",
		TaskID:      "task-ap",
		RunID:       "run-ap",
		Kind:        "shell",
		Status:      "pending",
		RequestedBy: "agent",
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := store.CreateApproval(ctx, approval); err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}

	got, ok, err := store.GetApproval(ctx, "task-ap", "ap-1")
	if err != nil || !ok {
		t.Fatalf("GetApproval: ok=%v err=%v", ok, err)
	}
	if got.Status != "pending" || got.Kind != "shell" {
		t.Fatalf("GetApproval round-trip mismatch: %+v", got)
	}

	// Resolve.
	got.Status = "approved"
	got.ResolvedBy = "operator"
	got.ResolvedAt = time.Now().UTC()
	got.ResolutionNote = "looks fine"
	if _, err := store.UpdateApproval(ctx, got); err != nil {
		t.Fatalf("UpdateApproval: %v", err)
	}

	resolved, ok, err := store.GetApproval(ctx, "task-ap", "ap-1")
	if err != nil || !ok {
		t.Fatalf("GetApproval after resolve: ok=%v err=%v", ok, err)
	}
	if resolved.Status != "approved" || resolved.ResolvedBy != "operator" || resolved.ResolutionNote != "looks fine" {
		t.Fatalf("resolution not persisted: %+v", resolved)
	}

	approvals, err := store.ListApprovals(ctx, "task-ap")
	if err != nil {
		t.Fatalf("ListApprovals: %v", err)
	}
	if len(approvals) != 1 || approvals[0].Status != "approved" {
		t.Fatalf("ListApprovals: %+v", approvals)
	}
}

func TestSQLiteStore_ArtifactRoundTrip(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	artifact := types.TaskArtifact{
		ID:          "art-1",
		TaskID:      "task-art",
		RunID:       "run-art",
		StepID:      "step-art",
		Kind:        "log",
		Name:        "build.log",
		MimeType:    "text/plain",
		StorageKind: "inline",
		ContentText: "hello world",
		SizeBytes:   11,
		Status:      "ready",
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := store.CreateArtifact(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifact: %v", err)
	}

	got, ok, err := store.GetArtifact(ctx, "task-art", "art-1")
	if err != nil || !ok {
		t.Fatalf("GetArtifact: ok=%v err=%v", ok, err)
	}
	if got.ContentText != "hello world" || got.MimeType != "text/plain" {
		t.Fatalf("GetArtifact round-trip mismatch: %+v", got)
	}

	listed, err := store.ListArtifacts(ctx, ArtifactFilter{TaskID: "task-art"})
	if err != nil {
		t.Fatalf("ListArtifacts: %v", err)
	}
	if len(listed) != 1 || listed[0].ContentText != "hello world" {
		t.Fatalf("ListArtifacts: %+v", listed)
	}

	// Filter by kind that doesn't match — should be empty.
	missing, err := store.ListArtifacts(ctx, ArtifactFilter{TaskID: "task-art", Kind: "trace"})
	if err != nil {
		t.Fatalf("ListArtifacts(kind=trace): %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("ListArtifacts(kind=trace) len = %d, want 0", len(missing))
	}
}

func TestSQLiteStore_RunEventsAppendAndList(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.AppendRunEvent(ctx, types.TaskRunEvent{
			TaskID:    "task-evt",
			RunID:     "run-evt",
			EventType: "step.completed",
			Data:      map[string]any{"i": i},
			RequestID: "req-evt",
		})
		if err != nil {
			t.Fatalf("AppendRunEvent[%d]: %v", i, err)
		}
	}

	events, err := store.ListRunEvents(ctx, "task-evt", "run-evt", 0, 100)
	if err != nil {
		t.Fatalf("ListRunEvents: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("ListRunEvents len = %d, want 3", len(events))
	}
	// sequence ASC, so the first event has the smallest sequence.
	if events[0].Sequence >= events[2].Sequence {
		t.Fatalf("sequence ordering: %+v", events)
	}
	// Cursor: afterSequence skips earlier rows.
	tail, err := store.ListRunEvents(ctx, "task-evt", "run-evt", events[0].Sequence, 100)
	if err != nil {
		t.Fatalf("ListRunEvents(cursor): %v", err)
	}
	if len(tail) != 2 {
		t.Fatalf("ListRunEvents(cursor) len = %d, want 2", len(tail))
	}
}

func TestSQLiteStore_ListRunsByFilterStatusSet(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i, status := range []string{"queued", "running", "completed", "failed"} {
		_, err := store.CreateRun(ctx, types.TaskRun{
			ID:        "run-" + status,
			TaskID:    "task-rfilter",
			Number:    i + 1,
			Status:    status,
			StartedAt: now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("CreateRun(%s): %v", status, err)
		}
	}

	got, err := store.ListRunsByFilter(ctx, RunFilter{
		TaskID:   "task-rfilter",
		Statuses: []string{"running", "completed"},
	})
	if err != nil {
		t.Fatalf("ListRunsByFilter: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListRunsByFilter len = %d, want 2", len(got))
	}
	for _, run := range got {
		if run.Status != "running" && run.Status != "completed" {
			t.Fatalf("unexpected status in filtered set: %q", run.Status)
		}
	}

	// Limit clamps the result.
	limited, err := store.ListRunsByFilter(ctx, RunFilter{TaskID: "task-rfilter", Limit: 2})
	if err != nil {
		t.Fatalf("ListRunsByFilter(limit): %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("ListRunsByFilter(limit) len = %d, want 2", len(limited))
	}
}

func TestSQLiteStore_DeleteTaskCascades(t *testing.T) {
	t.Parallel()
	store := newSQLiteTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateTask(ctx, types.Task{ID: "task-del", Status: "queued"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := store.CreateRun(ctx, types.TaskRun{ID: "run-del", TaskID: "task-del", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := store.AppendStep(ctx, types.TaskStep{ID: "step-del", TaskID: "task-del", RunID: "run-del", Status: "running"}); err != nil {
		t.Fatalf("AppendStep: %v", err)
	}

	if err := store.DeleteTask(ctx, "task-del"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	if _, ok, _ := store.GetTask(ctx, "task-del"); ok {
		t.Fatal("task still present after delete")
	}
	if _, ok, _ := store.GetRun(ctx, "task-del", "run-del"); ok {
		t.Fatal("run still present after delete")
	}
	steps, err := store.ListSteps(ctx, "run-del")
	if err != nil {
		t.Fatalf("ListSteps: %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps still present after delete: %d", len(steps))
	}
}
