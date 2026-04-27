package billing

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/internal/controlplane"
)

// fakeStore is a tiny in-memory PricebookImportStore. We don't reuse
// the real memory store from controlplane because the importer only
// needs Snapshot + UpsertPricebookEntry — a fake exposes the seam.
type fakeStore struct {
	mu      sync.Mutex
	rows    []config.ModelPriceConfig
	upserts int
	failOn  string // "provider/model" key — UpsertPricebookEntry returns error for this row
}

func (f *fakeStore) Snapshot(ctx context.Context) (controlplane.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]config.ModelPriceConfig, len(f.rows))
	copy(out, f.rows)
	return controlplane.State{Pricebook: out}, nil
}

func (f *fakeStore) UpsertPricebookEntry(ctx context.Context, entry config.ModelPriceConfig) (config.ModelPriceConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn != "" && PricebookKey(entry.Provider, entry.Model) == f.failOn {
		return config.ModelPriceConfig{}, errors.New("simulated upsert failure")
	}
	f.upserts++
	for i, r := range f.rows {
		if r.Provider == entry.Provider && r.Model == entry.Model {
			f.rows[i] = entry
			return entry, nil
		}
	}
	f.rows = append(f.rows, entry)
	return entry, nil
}

func TestPricebookImporter_PreviewClassifiesRows(t *testing.T) {
	store := &fakeStore{
		rows: []config.ModelPriceConfig{
			// imported row that LiteLLM has at a different price → Updated
			{Provider: "openai", Model: "gpt-4o", InputMicrosUSDPerMillionTokens: 100, Source: config.PricebookSourceImported},
			// manual row LiteLLM also has at a different price → Skipped (operator-protected)
			{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 50, Source: config.PricebookSourceManual},
			// imported row that already matches → Unchanged
			{Provider: "anthropic", Model: "claude-sonnet", InputMicrosUSDPerMillionTokens: 200, Source: config.PricebookSourceImported},
		},
	}
	fixture := []config.ModelPriceConfig{
		{Provider: "openai", Model: "gpt-4o", InputMicrosUSDPerMillionTokens: 150},           // updated
		{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 60},       // skipped (manual)
		{Provider: "anthropic", Model: "claude-sonnet", InputMicrosUSDPerMillionTokens: 200}, // unchanged
		{Provider: "anthropic", Model: "claude-opus", InputMicrosUSDPerMillionTokens: 300},   // added
	}
	imp := NewPricebookImporterWithFetcher(store, func(ctx context.Context) ([]config.ModelPriceConfig, error) {
		return fixture, nil
	})

	summary, err := imp.Run(context.Background(), PricebookImportOptions{Apply: false})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := len(summary.Added); got != 1 {
		t.Errorf("Added: got %d, want 1", got)
	}
	if got := len(summary.Updated); got != 1 {
		t.Errorf("Updated: got %d, want 1", got)
	}
	if got := len(summary.Skipped); got != 1 {
		t.Errorf("Skipped: got %d, want 1", got)
	}
	if summary.Unchanged != 1 {
		t.Errorf("Unchanged: got %d, want 1", summary.Unchanged)
	}
	// Preview must NOT mutate state.
	if store.upserts != 0 {
		t.Errorf("preview wrote %d rows; expected 0", store.upserts)
	}
}

func TestPricebookImporter_BlanketApplyPreservesManual(t *testing.T) {
	// The operator-protection contract: a blanket apply (empty Keys)
	// never overwrites manual rows. A regression here would silently
	// stomp on operator-set prices on every scheduler tick.
	store := &fakeStore{
		rows: []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 50, Source: config.PricebookSourceManual},
		},
	}
	imp := NewPricebookImporterWithFetcher(store, func(ctx context.Context) ([]config.ModelPriceConfig, error) {
		return []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 60},
		}, nil
	})

	summary, err := imp.Run(context.Background(), PricebookImportOptions{Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.Applied) != 0 {
		t.Errorf("manual row was overwritten; Applied=%d, want 0", len(summary.Applied))
	}
	if got := store.rows[0].InputMicrosUSDPerMillionTokens; got != 50 {
		t.Errorf("manual row mutated; price=%d, want 50", got)
	}
}

func TestPricebookImporter_ExplicitKeysReplaceManual(t *testing.T) {
	// When the operator explicitly names a manual row in Keys, the
	// "replace this manual" affordance triggers — Skipped becomes
	// Applied. Tests the consent-dialog path.
	store := &fakeStore{
		rows: []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 50, Source: config.PricebookSourceManual},
		},
	}
	imp := NewPricebookImporterWithFetcher(store, func(ctx context.Context) ([]config.ModelPriceConfig, error) {
		return []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o-mini", InputMicrosUSDPerMillionTokens: 60},
		}, nil
	})

	_, err := imp.Run(context.Background(), PricebookImportOptions{
		Apply: true,
		Keys:  []string{"openai/gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := store.rows[0].InputMicrosUSDPerMillionTokens; got != 60 {
		t.Errorf("manual row should have been replaced; price=%d, want 60", got)
	}
	if got := store.rows[0].Source; got != config.PricebookSourceImported {
		t.Errorf("source should flip to imported; got %q", got)
	}
}

func TestPricebookImporter_FailedRowsCollected(t *testing.T) {
	// Best-effort apply: one row's failure must not stop the others.
	store := &fakeStore{failOn: "openai/gpt-4o"}
	imp := NewPricebookImporterWithFetcher(store, func(ctx context.Context) ([]config.ModelPriceConfig, error) {
		return []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o", InputMicrosUSDPerMillionTokens: 100},
			{Provider: "anthropic", Model: "claude-opus", InputMicrosUSDPerMillionTokens: 300},
		}, nil
	})

	summary, err := imp.Run(context.Background(), PricebookImportOptions{Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(summary.Failed) != 1 || summary.Failed[0].Entry.Model != "gpt-4o" {
		t.Errorf("Failed: got %+v, want one gpt-4o failure", summary.Failed)
	}
	if len(summary.Applied) != 1 || summary.Applied[0].Model != "claude-opus" {
		t.Errorf("Applied: got %+v, want one claude-opus", summary.Applied)
	}
}

// captureLogger collects log calls for assertions.
type captureLogger struct {
	mu    sync.Mutex
	infos atomic.Int32
	warns atomic.Int32
	last  string
}

func (l *captureLogger) Info(msg string, args ...any) {
	l.infos.Add(1)
	l.mu.Lock()
	l.last = msg
	l.mu.Unlock()
}
func (l *captureLogger) Warn(msg string, args ...any) {
	l.warns.Add(1)
}

func TestRunPricebookAutoImport_DisabledReturnsImmediately(t *testing.T) {
	logger := &captureLogger{}
	// No goroutine — call directly. Must return without blocking.
	done := make(chan struct{})
	go func() {
		RunPricebookAutoImport(context.Background(), nil, PricebookAutoImportConfig{Interval: 0}, logger)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled scheduler did not return within 1s")
	}
	if logger.infos.Load() != 0 || logger.warns.Load() != 0 {
		t.Errorf("disabled scheduler logged something; infos=%d warns=%d", logger.infos.Load(), logger.warns.Load())
	}
}

func TestRunPricebookAutoImport_FiresOnStartAndOnTick(t *testing.T) {
	store := &fakeStore{}
	imp := NewPricebookImporterWithFetcher(store, func(ctx context.Context) ([]config.ModelPriceConfig, error) {
		return []config.ModelPriceConfig{
			{Provider: "openai", Model: "gpt-4o", InputMicrosUSDPerMillionTokens: 100},
		}, nil
	})
	logger := &captureLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go RunPricebookAutoImport(ctx, imp, PricebookAutoImportConfig{Interval: 50 * time.Millisecond}, logger)

	// Wait for the start-pulse + at least one tick. 200ms allows ~3-4 ticks
	// on the 50ms interval — generous for slow CI.
	deadline := time.After(500 * time.Millisecond)
	for {
		if logger.infos.Load() >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("scheduler logged %d Info events in 500ms; want >= 2 (start + ≥1 tick)", logger.infos.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
}

func TestRunPricebookAutoImport_FetchErrorDoesNotCrash(t *testing.T) {
	store := &fakeStore{}
	imp := NewPricebookImporterWithFetcher(store, func(ctx context.Context) ([]config.ModelPriceConfig, error) {
		return nil, errors.New("simulated upstream failure")
	})
	logger := &captureLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go RunPricebookAutoImport(ctx, imp, PricebookAutoImportConfig{Interval: 30 * time.Millisecond}, logger)

	// Two ticks worth — both should produce Warn entries, not crash.
	deadline := time.After(300 * time.Millisecond)
	for {
		if logger.warns.Load() >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("scheduler logged %d Warn events; want >= 2", logger.warns.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
}
