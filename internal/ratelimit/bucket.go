// Package ratelimit implements a per-key token bucket for HTTP request
// rate limiting.  Each key gets its own bucket; the bucket refills
// continuously at a constant rate up to its capacity.
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// ExceededError is returned when a key has exhausted its token bucket.
// Handlers should translate this to HTTP 429 and set X-RateLimit-* headers.
type ExceededError struct {
	Limit     int64
	Remaining int64
	ResetAt   time.Time
}

func (e *ExceededError) Error() string {
	return fmt.Sprintf("rate limit exceeded (limit: %d/min, resets at: %s)",
		e.Limit, e.ResetAt.UTC().Format(time.RFC3339))
}

// bucket is a single token bucket for one key.
type bucket struct {
	mu       sync.Mutex
	tokens   float64
	capacity float64
	// refillRate is tokens per nanosecond
	refillRate float64
	lastRefill int64 // UnixNano
}

func newBucket(capacity int64, refillPerMinute int64) *bucket {
	ratePerNs := float64(refillPerMinute) / float64(time.Minute)
	return &bucket{
		tokens:     float64(capacity),
		capacity:   float64(capacity),
		refillRate: ratePerNs,
		lastRefill: time.Now().UnixNano(),
	}
}

// Allow tries to consume one token.  Returns (limit, remaining, resetAt, ok).
// resetAt is the time when the bucket will be full again.
func (b *bucket) Allow() (limit int64, remaining int64, resetAt time.Time, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	nowNano := now.UnixNano()

	// Refill tokens for elapsed time.
	elapsed := float64(nowNano - b.lastRefill)
	if elapsed > 0 {
		b.tokens = min(b.capacity, b.tokens+elapsed*b.refillRate)
		b.lastRefill = nowNano
	}

	limit = int64(b.capacity)

	// Compute when the bucket will be full (for Retry-After / X-RateLimit-Reset).
	deficit := b.capacity - b.tokens
	if deficit <= 0 {
		resetAt = now
	} else {
		nsUntilFull := int64(deficit / b.refillRate)
		resetAt = now.Add(time.Duration(nsUntilFull))
	}

	if b.tokens < 1 {
		remaining = 0
		return limit, remaining, resetAt, false
	}

	b.tokens--
	remaining = int64(b.tokens)
	return limit, remaining, resetAt, true
}

// Store is a thread-safe collection of per-key token buckets.
type Store struct {
	mu              sync.Mutex
	buckets         map[string]*bucket
	capacity        int64 // default capacity
	refillPerMinute int64 // default refill rate
}

// NewStore creates a Store with the given default capacity and refill rate.
// capacity is the maximum burst size; refillPerMinute is the steady-state
// rate (e.g. 60 means one token per second).
func NewStore(capacity, refillPerMinute int64) *Store {
	if capacity <= 0 {
		capacity = 60
	}
	if refillPerMinute <= 0 {
		refillPerMinute = 60
	}
	return &Store{
		buckets:         make(map[string]*bucket),
		capacity:        capacity,
		refillPerMinute: refillPerMinute,
	}
}

// Allow consumes one token for key and returns limit/remaining/resetAt, or
// an *ExceededError if the bucket is empty.
func (s *Store) Allow(key string) (limit, remaining int64, resetAt time.Time, err error) {
	b := s.getBucket(key)
	limit, remaining, resetAt, ok := b.Allow()
	if !ok {
		return limit, 0, resetAt, &ExceededError{
			Limit:     limit,
			Remaining: 0,
			ResetAt:   resetAt,
		}
	}
	return limit, remaining, resetAt, nil
}

func (s *Store) getBucket(key string) *bucket {
	s.mu.Lock()
	defer s.mu.Unlock()

	if b, ok := s.buckets[key]; ok {
		return b
	}
	b := newBucket(s.capacity, s.refillPerMinute)
	s.buckets[key] = b
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
