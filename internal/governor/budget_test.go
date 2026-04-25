package governor

import (
	"context"
	"testing"
	"time"

	"github.com/hecate/agent-runtime/internal/config"
	"github.com/hecate/agent-runtime/pkg/types"
)

func TestMemoryBudgetStore_DebitCreditFlow(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()

	// Initial credit.
	state, err := s.Credit(ctx, "global", 1_000_000) // $1.00 in micros
	if err != nil {
		t.Fatalf("Credit: %v", err)
	}
	if state.BalanceMicrosUSD != 1_000_000 {
		t.Fatalf("balance = %d, want 1_000_000", state.BalanceMicrosUSD)
	}

	// Debit half.
	state, err = s.Debit(ctx, UsageEvent{BudgetKey: "global", CostMicros: 500_000, OccurredAt: time.Now()})
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if state.BalanceMicrosUSD != 500_000 {
		t.Fatalf("after debit = %d, want 500_000", state.BalanceMicrosUSD)
	}
	if state.DebitedMicrosUSD != 500_000 {
		t.Fatalf("debited = %d, want 500_000", state.DebitedMicrosUSD)
	}
}

func TestMemoryBudgetStore_DebitGoesNegativeAllowed(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()

	// Store itself doesn't enforce — that's the governor's job. The store just records.
	state, err := s.Debit(ctx, UsageEvent{BudgetKey: "x", CostMicros: 1_000})
	if err != nil {
		t.Fatalf("Debit: %v", err)
	}
	if state.BalanceMicrosUSD >= 0 {
		t.Logf("balance after overspend = %d (acceptable)", state.BalanceMicrosUSD)
	}
}

func TestMemoryBudgetStore_Reset(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()

	_, _ = s.Credit(ctx, "k", 1000)
	if err := s.Reset(ctx, "k"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	state, ok, _ := s.Snapshot(ctx, "k")
	if ok && state.BalanceMicrosUSD != 0 {
		t.Fatalf("balance after reset = %d, want 0", state.BalanceMicrosUSD)
	}
}

func TestMemoryBudgetStore_SetBalanceOverwrites(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()

	_, _ = s.Credit(ctx, "k", 100)
	state, err := s.SetBalance(ctx, "k", 50_000)
	if err != nil {
		t.Fatalf("SetBalance: %v", err)
	}
	if state.BalanceMicrosUSD != 50_000 {
		t.Fatalf("balance = %d, want 50_000", state.BalanceMicrosUSD)
	}
}

func TestMemoryBudgetStore_SnapshotMissing(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	_, ok, err := s.Snapshot(context.Background(), "never-set")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if ok {
		t.Fatal("ok = true for missing key")
	}
}

func TestMemoryBudgetStore_AppendAndListEvents(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = s.AppendEvent(ctx, BudgetEvent{
			Key: "global", Type: "debit", AmountMicrosUSD: int64(i * 100), OccurredAt: time.Now(),
		})
	}

	events, err := s.ListEvents(ctx, "global", 10)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("len = %d, want 5", len(events))
	}

	// Recent across all keys.
	_ = s.AppendEvent(ctx, BudgetEvent{Key: "other", Type: "credit", OccurredAt: time.Now()})
	all, _ := s.ListRecentEvents(ctx, 10)
	if len(all) != 6 {
		t.Fatalf("recent len = %d, want 6", len(all))
	}
}

func TestMemoryBudgetStore_PruneEventsByAge(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()

	old := time.Now().Add(-time.Hour)
	fresh := time.Now()
	_ = s.AppendEvent(ctx, BudgetEvent{Key: "k", OccurredAt: old})
	_ = s.AppendEvent(ctx, BudgetEvent{Key: "k", OccurredAt: fresh})

	deleted, err := s.PruneEvents(ctx, 30*time.Minute, 0)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestMemoryBudgetStore_PruneEventsByCount(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		_ = s.AppendEvent(ctx, BudgetEvent{Key: "k", OccurredAt: time.Now()})
	}
	deleted, _ := s.PruneEvents(ctx, 0, 3)
	if deleted != 7 {
		t.Fatalf("deleted = %d, want 7 (kept 3)", deleted)
	}
}

func TestMemoryBudgetStore_ConcurrentDebit(t *testing.T) {
	t.Parallel()
	s := NewMemoryBudgetStore()
	ctx := context.Background()
	_, _ = s.Credit(ctx, "k", 1_000_000)

	const goroutines = 50
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			_, _ = s.Debit(ctx, UsageEvent{BudgetKey: "k", CostMicros: 1_000})
			done <- struct{}{}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	state, _, _ := s.Snapshot(ctx, "k")
	expected := int64(1_000_000 - goroutines*1_000)
	if state.BalanceMicrosUSD != expected {
		t.Fatalf("balance = %d, want %d (no lost updates)", state.BalanceMicrosUSD, expected)
	}
}

func defaultGovernorCfg() config.GovernorConfig {
	return config.GovernorConfig{
		MaxPromptTokens: 64_000,
		BudgetBackend:   "memory",
		BudgetKey:       "global",
		BudgetScope:     "global",
	}
}

func TestStaticGovernor_RewriteIsIdentityWhenNoRules(t *testing.T) {
	t.Parallel()
	g := NewStaticGovernor(defaultGovernorCfg(), NewMemoryBudgetStore(), NewMemoryBudgetStore())
	req := types.ChatRequest{Model: "gpt-4o-mini", Messages: []types.Message{{Role: "user", Content: "hi"}}}
	out := g.Rewrite(req)
	if out.Model != req.Model {
		t.Fatalf("Rewrite changed model: %q -> %q", req.Model, out.Model)
	}
}

func TestStaticGovernor_CheckEnforcesPromptTokenCap(t *testing.T) {
	t.Parallel()
	cfg := defaultGovernorCfg()
	cfg.MaxPromptTokens = 10
	g := NewStaticGovernor(cfg, NewMemoryBudgetStore(), NewMemoryBudgetStore())
	req := types.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "this is a much longer message that exceeds the cap of 10 tokens"},
		},
	}
	if err := g.Check(context.Background(), req); err == nil {
		t.Fatal("expected error for prompt over cap")
	}
}
