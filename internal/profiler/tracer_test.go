package profiler

import "testing"

func TestTraceRecordCreatesEvent(t *testing.T) {
	t.Parallel()

	trace := NewTrace("req-123")
	trace.Record("cache.miss", map[string]any{"key": "abc"})

	events := trace.Events()
	if len(events) != 1 {
		t.Fatalf("Events() len = %d, want 1", len(events))
	}
	if events[0].Name != "cache.miss" {
		t.Fatalf("event name = %q, want %q", events[0].Name, "cache.miss")
	}
	if events[0].Attributes["key"] != "abc" {
		t.Fatalf("event attribute = %#v, want abc", events[0].Attributes["key"])
	}
}
