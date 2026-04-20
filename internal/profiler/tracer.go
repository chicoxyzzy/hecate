package profiler

import (
	"sync"
	"time"

	"github.com/hecate/agent-runtime/pkg/types"
)

type Tracer interface {
	Start(requestID string) *Trace
}

type Trace struct {
	RequestID string
	StartedAt time.Time

	mu     sync.Mutex
	events []types.TraceEvent
}

func NewTrace(requestID string) *Trace {
	return &Trace{
		RequestID: requestID,
		StartedAt: time.Now().UTC(),
		events:    make([]types.TraceEvent, 0, 8),
	}
}

func (t *Trace) Record(name string, attrs map[string]any) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.events = append(t.events, types.TraceEvent{
		Name:       name,
		Timestamp:  time.Now().UTC(),
		Attributes: attrs,
	})
}

func (t *Trace) Events() []types.TraceEvent {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]types.TraceEvent, len(t.events))
	copy(out, t.events)
	return out
}

type InMemoryTracer struct {
	mu     sync.Mutex
	traces []*Trace
}

func NewInMemoryTracer() *InMemoryTracer {
	return &InMemoryTracer{
		traces: make([]*Trace, 0, 16),
	}
}

func (t *InMemoryTracer) Start(requestID string) *Trace {
	trace := NewTrace(requestID)

	t.mu.Lock()
	t.traces = append(t.traces, trace)
	t.mu.Unlock()

	return trace
}
