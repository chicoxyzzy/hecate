package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TestTraceContextMiddlewareExtractsTraceparent verifies that an inbound
// W3C traceparent header is parsed into the request context. Without this,
// distributed traces from upstream services lose their parent link the moment
// they enter the gateway.
func TestTraceContextMiddlewareExtractsTraceparent(t *testing.T) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	const (
		wantTraceID = "0af7651916cd43dd8448eb211c80319c"
		wantSpanID  = "b7ad6b7169203331"
	)

	var captured oteltrace.SpanContext
	handler := TraceContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = oteltrace.SpanContextFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("traceparent", "00-"+wantTraceID+"-"+wantSpanID+"-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !captured.IsValid() {
		t.Fatal("expected valid span context after extraction, got invalid")
	}
	if got := captured.TraceID().String(); got != wantTraceID {
		t.Errorf("trace id = %q, want %q", got, wantTraceID)
	}
	if got := captured.SpanID().String(); got != wantSpanID {
		t.Errorf("span id = %q, want %q", got, wantSpanID)
	}
	if !captured.IsRemote() {
		t.Error("extracted span context should be marked remote")
	}
}
