package retention

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hecate/agent-runtime/internal/config"
)

func TestShouldRunEmptySelectionMeansAll(t *testing.T) {
	if !shouldRun(nil, "anything") {
		t.Error("nil selected should mean all subsystems run")
	}
	if !shouldRun([]string{}, "anything") {
		t.Error("empty selected should mean all subsystems run")
	}
}

func TestShouldRunFiltersBySubsystemName(t *testing.T) {
	selected := []string{SubsystemTraces, SubsystemBudgetEvents}
	if !shouldRun(selected, SubsystemTraces) {
		t.Error("traces should match")
	}
	if shouldRun(selected, SubsystemSemanticCache) {
		t.Error("semantic cache not in selection should be skipped")
	}
}

func TestManagerEnabledHandlesNil(t *testing.T) {
	var m *Manager
	if m.Enabled() {
		t.Error("nil manager should not report enabled")
	}

	m = &Manager{cfg: config.RetentionConfig{Enabled: false}}
	if m.Enabled() {
		t.Error("manager with cfg.Enabled=false should not report enabled")
	}

	m = &Manager{cfg: config.RetentionConfig{Enabled: true}}
	if !m.Enabled() {
		t.Error("manager with cfg.Enabled=true should report enabled")
	}
}

func TestCloneHistoryRecordsIsolatesNestedSlices(t *testing.T) {
	originals := []HistoryRecord{
		{
			Trigger: "manual",
			Results: []SubsystemResult{{Name: "traces", Deleted: 5}, {Name: "budget", Deleted: 2}},
		},
	}
	clones := cloneHistoryRecords(originals)
	if len(clones) != 1 {
		t.Fatalf("len(clones) = %d, want 1", len(clones))
	}

	// Mutate the original results slice; clone must not change.
	originals[0].Results[0].Deleted = 9999
	if clones[0].Results[0].Deleted != 5 {
		t.Errorf("clone shared backing slice with original: got Deleted = %d, want 5", clones[0].Results[0].Deleted)
	}
}

func TestCloneHistoryRecordsEmptyReturnsNil(t *testing.T) {
	if got := cloneHistoryRecords(nil); got != nil {
		t.Errorf("nil input → got %v, want nil", got)
	}
	if got := cloneHistoryRecords([]HistoryRecord{}); got != nil {
		t.Errorf("empty input → got %v, want nil", got)
	}
}

func TestCloneSubsystemResultsEmptyReturnsNil(t *testing.T) {
	if got := cloneSubsystemResults(nil); got != nil {
		t.Errorf("nil input → got %v, want nil", got)
	}
	if got := cloneSubsystemResults([]SubsystemResult{}); got != nil {
		t.Errorf("empty input → got %v, want nil", got)
	}
}

func TestLimitHistoryRecords(t *testing.T) {
	records := []HistoryRecord{{Trigger: "a"}, {Trigger: "b"}, {Trigger: "c"}}

	cases := []struct {
		limit, wantLen int
	}{
		{0, 3},  // 0 means no limit
		{-1, 3}, // negative also no limit
		{2, 2},
		{99, 3}, // larger than slice → all
		{3, 3},
	}
	for _, tc := range cases {
		got := limitHistoryRecords(records, tc.limit)
		if len(got) != tc.wantLen {
			t.Errorf("limitHistoryRecords(limit=%d) len = %d, want %d", tc.limit, len(got), tc.wantLen)
		}
	}
}

func TestNewFileHistoryStoreRequiresPath(t *testing.T) {
	for _, p := range []string{"", "   "} {
		if _, err := NewFileHistoryStore(p); err == nil {
			t.Errorf("NewFileHistoryStore(%q) → err = nil, want error", p)
		}
	}
}

func TestFileHistoryStoreLoadsExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.json")

	// Pre-populate with a recognizable record so load() must reconstruct it.
	state := historyState{Runs: []HistoryRecord{{Trigger: "preexisting", Actor: "alice"}}}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	store, err := NewFileHistoryStore(path)
	if err != nil {
		t.Fatalf("NewFileHistoryStore: %v", err)
	}
	got, err := store.ListRuns(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(got) != 1 || got[0].Trigger != "preexisting" {
		t.Errorf("ListRuns = %+v, want preexisting record", got)
	}
}

func TestFileHistoryStoreRejectsCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := NewFileHistoryStore(path); err == nil {
		t.Error("NewFileHistoryStore on corrupt JSON → err = nil, want error")
	}
}

func TestFileHistoryStoreCreatesFileOnAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new", "history.json") // intentionally nested

	store, err := NewFileHistoryStore(path)
	if err != nil {
		t.Fatalf("NewFileHistoryStore: %v", err)
	}
	// Now append; that must create the parent dir and file.
	if err := store.AppendRun(context.Background(), HistoryRecord{Trigger: "manual"}); err != nil {
		t.Fatalf("AppendRun: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s, got %v", path, err)
	}
}

// fakeRedisClient satisfies historyRedisClient without needing a real Redis.
type fakeRedisClient struct {
	data    []byte
	getErr  error
	setErr  error
	gets    int
	setKeys []string
}

func (f *fakeRedisClient) Get(_ context.Context, _ string) ([]byte, error) {
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.data, nil
}

func (f *fakeRedisClient) Set(_ context.Context, key string, value []byte) error {
	f.setKeys = append(f.setKeys, key)
	if f.setErr != nil {
		return f.setErr
	}
	f.data = append([]byte(nil), value...)
	return nil
}

// errKeyMissing matches storage.ErrNil shape so RedisHistoryStore.readState
// treats a missing key as empty state rather than a fatal error.
var errKeyMissing = errors.New("redis: nil")

func TestNewRedisHistoryStoreRequiresClient(t *testing.T) {
	if _, err := NewRedisHistoryStoreFromClient(nil, "", "history"); err == nil {
		t.Error("nil client → err = nil, want error")
	}
}

func TestRedisHistoryStoreAppendAndList(t *testing.T) {
	// Start with no data so readState returns an error that is not ErrNil —
	// the store will surface it. That isn't what we want for this test, so
	// pre-seed with valid empty state.
	state := historyState{Runs: nil}
	encoded, _ := json.Marshal(state)
	client := &fakeRedisClient{data: encoded}

	store, err := NewRedisHistoryStoreFromClient(client, "tenant", "retention")
	if err != nil {
		t.Fatalf("NewRedisHistoryStoreFromClient: %v", err)
	}

	ctx := context.Background()
	if err := store.AppendRun(ctx, HistoryRecord{Trigger: "manual", Actor: "alice"}); err != nil {
		t.Fatalf("AppendRun: %v", err)
	}
	if err := store.AppendRun(ctx, HistoryRecord{Trigger: "scheduled"}); err != nil {
		t.Fatalf("AppendRun: %v", err)
	}

	got, err := store.ListRuns(ctx, 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListRuns len = %d, want 2", len(got))
	}

	// Key prefix should be applied.
	if len(client.setKeys) == 0 || client.setKeys[0] != "tenant:retention" {
		t.Errorf("Set called with %v, want first key tenant:retention", client.setKeys)
	}
}
