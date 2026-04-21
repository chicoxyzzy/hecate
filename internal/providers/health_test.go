package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryHealthTrackerOpensAndRecoversAfterCooldown(t *testing.T) {
	t.Parallel()

	tracker := NewMemoryHealthTracker(2, 10*time.Second)
	now := time.Date(2026, 4, 21, 1, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return now }

	tracker.RecordFailure("openai", context.DeadlineExceeded)
	if state := tracker.State("openai"); !state.Available || state.ConsecutiveFailures != 1 {
		t.Fatalf("state after first failure = %#v, want available with one failure", state)
	} else if state.Status != HealthStatusDegraded || state.Timeouts != 1 {
		t.Fatalf("state after first failure = %#v, want degraded with timeout tracked", state)
	}

	tracker.RecordFailure("openai", errors.New("temporary failure"))
	state := tracker.State("openai")
	if state.Available {
		t.Fatalf("state.Available = true, want false after threshold")
	}
	if state.Status != HealthStatusOpen {
		t.Fatalf("state.Status = %q, want %q", state.Status, HealthStatusOpen)
	}
	if state.ConsecutiveFailures != 2 {
		t.Fatalf("state.ConsecutiveFailures = %d, want 2", state.ConsecutiveFailures)
	}

	now = now.Add(11 * time.Second)
	state = tracker.State("openai")
	if !state.Available {
		t.Fatalf("state.Available = false, want true after cooldown")
	}
	if state.Status != HealthStatusHalfOpen {
		t.Fatalf("state.Status = %q, want %q after cooldown", state.Status, HealthStatusHalfOpen)
	}
	if !state.OpenUntil.IsZero() {
		t.Fatalf("state.OpenUntil = %v, want zero after cooldown", state.OpenUntil)
	}

	tracker.RecordSuccess("openai")
	state = tracker.State("openai")
	if state.ConsecutiveFailures != 0 {
		t.Fatalf("state.ConsecutiveFailures = %d, want 0 after success", state.ConsecutiveFailures)
	}
	if state.LastError != "" {
		t.Fatalf("state.LastError = %q, want empty after success", state.LastError)
	}
	if state.Status != HealthStatusHealthy {
		t.Fatalf("state.Status = %q, want %q after success", state.Status, HealthStatusHealthy)
	}
}
