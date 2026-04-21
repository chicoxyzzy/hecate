package providers

import (
	"fmt"
	"sync"
	"time"
)

type HealthTracker interface {
	RecordSuccess(provider string)
	RecordFailure(provider string, err error)
	State(provider string) HealthState
}

type HealthState struct {
	Available           bool
	ConsecutiveFailures int
	OpenUntil           time.Time
	LastError           string
}

type MemoryHealthTracker struct {
	mu               sync.RWMutex
	failureThreshold int
	cooldown         time.Duration
	providers        map[string]HealthState
	now              func() time.Time
}

func NewMemoryHealthTracker(failureThreshold int, cooldown time.Duration) *MemoryHealthTracker {
	if failureThreshold <= 0 {
		failureThreshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &MemoryHealthTracker{
		failureThreshold: failureThreshold,
		cooldown:         cooldown,
		providers:        make(map[string]HealthState),
		now:              time.Now,
	}
}

func (t *MemoryHealthTracker) RecordSuccess(provider string) {
	if provider == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.providers[provider]
	state.Available = true
	state.ConsecutiveFailures = 0
	state.OpenUntil = time.Time{}
	state.LastError = ""
	t.providers[provider] = state
}

func (t *MemoryHealthTracker) RecordFailure(provider string, err error) {
	if provider == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.providers[provider]
	state.ConsecutiveFailures++
	state.Available = true
	if err != nil {
		state.LastError = err.Error()
	}
	if state.ConsecutiveFailures >= t.failureThreshold {
		state.Available = false
		state.OpenUntil = t.now().Add(t.cooldown)
	}
	t.providers[provider] = state
}

func (t *MemoryHealthTracker) State(provider string) HealthState {
	if provider == "" {
		return HealthState{Available: true}
	}

	t.mu.RLock()
	state, ok := t.providers[provider]
	now := t.now()
	t.mu.RUnlock()
	if !ok {
		return HealthState{Available: true}
	}

	if state.OpenUntil.IsZero() || !now.Before(state.OpenUntil) {
		if !state.Available || !state.OpenUntil.IsZero() {
			t.mu.Lock()
			updated := t.providers[provider]
			if !updated.OpenUntil.IsZero() && !t.now().Before(updated.OpenUntil) {
				updated.Available = true
				updated.OpenUntil = time.Time{}
				t.providers[provider] = updated
				state = updated
			}
			t.mu.Unlock()
		}
		state.Available = true
		return state
	}

	state.Available = false
	return state
}

func FormatHealthStateError(provider string, state HealthState) string {
	if state.LastError == "" && state.OpenUntil.IsZero() {
		return ""
	}
	if state.OpenUntil.IsZero() {
		return fmt.Sprintf("provider health memory indicates recent transient failures for %s: %s", provider, state.LastError)
	}
	if state.LastError == "" {
		return fmt.Sprintf("provider %s is cooling down until %s", provider, state.OpenUntil.UTC().Format(time.RFC3339))
	}
	return fmt.Sprintf("provider %s is cooling down until %s after transient failures: %s", provider, state.OpenUntil.UTC().Format(time.RFC3339), state.LastError)
}
