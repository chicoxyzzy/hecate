package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type memoryClaim struct {
	job        QueueJob
	leaseUntil time.Time
}

type MemoryRunQueue struct {
	ch       chan QueueJob
	mu       sync.Mutex
	inflight map[string]memoryClaim
	counter  uint64
	lease    time.Duration
}

func NewMemoryRunQueue(capacity int, lease time.Duration) *MemoryRunQueue {
	if capacity <= 0 {
		capacity = 128
	}
	if lease <= 0 {
		lease = 30 * time.Second
	}
	return &MemoryRunQueue{
		ch:       make(chan QueueJob, capacity),
		inflight: make(map[string]memoryClaim),
		lease:    lease,
	}
}

func (q *MemoryRunQueue) Backend() string { return "memory" }

func (q *MemoryRunQueue) Enqueue(ctx context.Context, job QueueJob) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case q.ch <- job:
		return nil
	default:
		return fmt.Errorf("run queue is full")
	}
}

func (q *MemoryRunQueue) Claim(ctx context.Context, _ string, waitFor time.Duration) (QueueClaim, bool, error) {
	if waitFor <= 0 {
		waitFor = 200 * time.Millisecond
	}
	q.reclaimExpired(time.Now().UTC())
	timer := time.NewTimer(waitFor)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return QueueClaim{}, false, ctx.Err()
	case <-timer.C:
		return QueueClaim{}, false, nil
	case job := <-q.ch:
		claimID := strconv.FormatUint(atomic.AddUint64(&q.counter, 1), 10)
		leaseUntil := time.Now().UTC().Add(q.lease)
		q.mu.Lock()
		q.inflight[claimID] = memoryClaim{job: job, leaseUntil: leaseUntil}
		q.mu.Unlock()
		return QueueClaim{
			ClaimID:    claimID,
			Job:        job,
			LeaseUntil: leaseUntil,
		}, true, nil
	}
}

func (q *MemoryRunQueue) reclaimExpired(now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for claimID, claim := range q.inflight {
		if now.Before(claim.leaseUntil) {
			continue
		}
		select {
		case q.ch <- claim.job:
			delete(q.inflight, claimID)
		default:
			// The in-memory queue is the dev/test backend. If the pending
			// channel is full, keep the expired claim in place and retry
			// reclamation on the next Claim instead of dropping work.
		}
	}
}

func (q *MemoryRunQueue) Ack(_ context.Context, claimID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inflight, claimID)
	return nil
}

func (q *MemoryRunQueue) Nack(ctx context.Context, claimID, _ string) error {
	q.mu.Lock()
	claim, ok := q.inflight[claimID]
	if ok {
		delete(q.inflight, claimID)
	}
	q.mu.Unlock()
	if !ok {
		return nil
	}
	return q.Enqueue(ctx, claim.job)
}

func (q *MemoryRunQueue) ExtendLease(_ context.Context, claimID string, leaseFor time.Duration) error {
	if leaseFor <= 0 {
		leaseFor = q.lease
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	claim, ok := q.inflight[claimID]
	if !ok {
		return nil
	}
	claim.leaseUntil = time.Now().UTC().Add(leaseFor)
	q.inflight[claimID] = claim
	return nil
}

func (q *MemoryRunQueue) Depth(_ context.Context) (int, error) {
	return len(q.ch), nil
}

func (q *MemoryRunQueue) Capacity() int {
	return cap(q.ch)
}
