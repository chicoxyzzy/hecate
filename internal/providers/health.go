package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type HealthTracker interface {
	Observe(provider string, observation HealthObservation)
	RecordSuccess(provider string)
	RecordFailure(provider string, err error)
	State(provider string) HealthState
}

type HealthStatus string

const (
	HealthStatusHealthy  HealthStatus = "healthy"
	HealthStatusDegraded HealthStatus = "degraded"
	HealthStatusOpen     HealthStatus = "open"
	HealthStatusHalfOpen HealthStatus = "half_open"
)

type HealthObservation struct {
	Duration time.Duration
	Error    error
}

type HealthState struct {
	Available           bool
	Status              HealthStatus
	ConsecutiveFailures int
	TotalSuccesses      int64
	TotalFailures       int64
	Timeouts            int64
	ServerErrors        int64
	RateLimits          int64
	LastLatency         time.Duration
	LastSuccessAt       time.Time
	LastFailureAt       time.Time
	OpenUntil           time.Time
	LastError           string
	LastErrorClass      string
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
	t.Observe(provider, HealthObservation{})
}

func (t *MemoryHealthTracker) Observe(provider string, observation HealthObservation) {
	if provider == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.providers[provider]
	now := t.now()
	state.LastLatency = observation.Duration
	if observation.Error == nil {
		state.Available = true
		state.Status = HealthStatusHealthy
		state.ConsecutiveFailures = 0
		state.OpenUntil = time.Time{}
		state.LastError = ""
		state.LastErrorClass = ""
		state.LastSuccessAt = now
		state.TotalSuccesses++
		t.providers[provider] = state
		return
	}

	state.TotalFailures++
	state.ConsecutiveFailures++
	state.Available = true
	state.Status = HealthStatusDegraded
	state.LastFailureAt = now
	state.LastError = observation.Error.Error()
	errorClass := classifyHealthError(observation.Error)
	state.LastErrorClass = errorClass
	switch errorClass {
	case "timeout":
		state.Timeouts++
	case "rate_limit":
		state.RateLimits++
	case "server_error":
		state.ServerErrors++
	}
	if errorClass == "rate_limit" {
		// Upstream 429s are a signal that the provider-side quota window is
		// the current bottleneck, not that we should keep probing until a
		// generic consecutive-failure threshold trips. Cool the provider
		// down immediately so later requests can route elsewhere.
		state.Available = false
		state.Status = HealthStatusOpen
		state.OpenUntil = now.Add(t.cooldown)
		t.providers[provider] = state
		return
	}
	if state.ConsecutiveFailures >= t.failureThreshold {
		state.Available = false
		state.Status = HealthStatusOpen
		state.OpenUntil = now.Add(t.cooldown)
	}
	t.providers[provider] = state
}

func HealthStateReason(state HealthState) string {
	return state.LastErrorClass
}

func (t *MemoryHealthTracker) RecordFailure(provider string, err error) {
	t.Observe(provider, HealthObservation{Error: err})
}

func (t *MemoryHealthTracker) State(provider string) HealthState {
	if provider == "" {
		return HealthState{Available: true, Status: HealthStatusHealthy}
	}

	t.mu.RLock()
	state, ok := t.providers[provider]
	now := t.now()
	t.mu.RUnlock()
	if !ok {
		return HealthState{Available: true, Status: HealthStatusHealthy}
	}

	if state.OpenUntil.IsZero() || !now.Before(state.OpenUntil) {
		if !state.Available || !state.OpenUntil.IsZero() {
			t.mu.Lock()
			updated := t.providers[provider]
			if !updated.OpenUntil.IsZero() && !t.now().Before(updated.OpenUntil) {
				updated.Available = true
				updated.OpenUntil = time.Time{}
				updated.Status = HealthStatusHalfOpen
				t.providers[provider] = updated
				state = updated
			}
			t.mu.Unlock()
		}
		state.Available = true
		if state.Status == "" {
			state.Status = HealthStatusHealthy
		}
		return state
	}

	state.Available = false
	state.Status = HealthStatusOpen
	return state
}

func FormatHealthStateError(provider string, state HealthState) string {
	if state.LastError == "" && state.OpenUntil.IsZero() {
		return ""
	}
	if state.OpenUntil.IsZero() {
		return fmt.Sprintf("provider health memory indicates recent transient failures for %s (%s): %s", provider, state.Status, state.LastError)
	}
	if state.LastError == "" {
		return fmt.Sprintf("provider %s is cooling down until %s (%s)", provider, state.OpenUntil.UTC().Format(time.RFC3339), state.Status)
	}
	return fmt.Sprintf("provider %s is cooling down until %s after transient failures (%s): %s", provider, state.OpenUntil.UTC().Format(time.RFC3339), state.Status, state.LastError)
}

func classifyHealthError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	var upstreamErr *UpstreamError
	if errors.As(err, &upstreamErr) {
		switch upstreamErr.StatusCode {
		case http.StatusTooManyRequests:
			return "rate_limit"
		case http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return "server_error"
		}
	}

	return "other"
}
