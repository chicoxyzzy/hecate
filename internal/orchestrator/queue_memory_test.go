package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestMemoryRunQueueClaimAck(t *testing.T) {
	t.Parallel()
	q := NewMemoryRunQueue(8, 2*time.Second)
	if err := q.Enqueue(context.Background(), QueueJob{TaskID: "task_1", RunID: "run_1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claim, ok, err := q.Claim(context.Background(), "worker_a", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok {
		t.Fatal("expected claimed job")
	}
	if claim.Job.RunID != "run_1" {
		t.Fatalf("unexpected run id: %s", claim.Job.RunID)
	}
	if err := q.Ack(context.Background(), claim.ClaimID); err != nil {
		t.Fatalf("ack: %v", err)
	}
}

func TestMemoryRunQueueNackRequeues(t *testing.T) {
	t.Parallel()
	q := NewMemoryRunQueue(8, time.Second)
	if err := q.Enqueue(context.Background(), QueueJob{TaskID: "task_2", RunID: "run_2"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claim, ok, err := q.Claim(context.Background(), "worker_a", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !ok {
		t.Fatal("expected claimed job")
	}
	if err := q.Nack(context.Background(), claim.ClaimID, "retry"); err != nil {
		t.Fatalf("nack: %v", err)
	}
	claimedAgain, ok, err := q.Claim(context.Background(), "worker_b", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("claim again: %v", err)
	}
	if !ok {
		t.Fatal("expected requeued job to be claimed")
	}
	if claimedAgain.Job.RunID != "run_2" {
		t.Fatalf("unexpected run id: %s", claimedAgain.Job.RunID)
	}
}
