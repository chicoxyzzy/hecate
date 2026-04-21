package telemetry

import (
	"context"
	"testing"
)

func TestContextAttrsIncludeRequestTraceAndPrincipal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-123")
	ctx = WithTraceIDs(ctx, "trace-123", "span-123")
	ctx = WithPrincipal(ctx, Principal{
		Name:     "alice",
		Role:     "tenant",
		TenantID: "acme",
		Source:   "control_plane_api_key",
		KeyID:    "key-1",
	})

	attrs := ContextAttrs(ctx)
	got := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		if attr.Value.Kind() == 0 {
			continue
		}
		got[attr.Key] = attr.Value.String()
	}

	want := map[string]string{
		"request.id":         "req-123",
		"trace_id":           "trace-123",
		"span_id":            "span-123",
		"enduser.id":         "alice",
		"tenant.id":          "acme",
		"hecate.auth.role":   "tenant",
		"hecate.auth.source": "control_plane_api_key",
		"hecate.auth.key_id": "key-1",
	}

	for key, value := range want {
		if got[key] != value {
			t.Fatalf("ContextAttrs()[%q] = %q, want %q", key, got[key], value)
		}
	}
}
