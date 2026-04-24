package orchestrator

import (
	"context"
	"time"
)

type QueueJob struct {
	TaskID string
	RunID  string
}

type QueueClaim struct {
	ClaimID    string
	Job        QueueJob
	LeaseUntil time.Time
}

type RunQueue interface {
	Backend() string
	Enqueue(ctx context.Context, job QueueJob) error
	Claim(ctx context.Context, workerID string, waitFor time.Duration) (QueueClaim, bool, error)
	Ack(ctx context.Context, claimID string) error
	Nack(ctx context.Context, claimID string, reason string) error
	ExtendLease(ctx context.Context, claimID string, leaseFor time.Duration) error
	Depth(ctx context.Context) (int, error)
	Capacity() int
}
