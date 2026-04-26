package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func registerW3CPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// TestTraceContextMiddlewareExtractsTraceparent verifies that an inbound
// W3C traceparent header is parsed into the request context. Without this,
// distributed traces from upstream services lose their parent link the moment
// they enter the gateway.
func TestTraceContextMiddlewareExtractsTraceparent(t *testing.T) {
	registerW3CPropagator()

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

// TestTraceContextMiddlewareNoHeaderPassesThrough verifies that requests
// without trace context don't trigger errors and yield an invalid (zero)
// span context downstream — the signal handlers use to start a fresh trace
// rather than parent off something fabricated.
func TestTraceContextMiddlewareNoHeaderPassesThrough(t *testing.T) {
	registerW3CPropagator()

	var captured oteltrace.SpanContext
	handler := TraceContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = oteltrace.SpanContextFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if captured.IsValid() {
		t.Errorf("expected invalid span context when no traceparent header, got valid: %v", captured)
	}
}

// TestTraceContextMiddlewareExtractsBaggage verifies that W3C baggage entries
// flow into request context. Baggage carries cross-cutting key-value pairs
// like tenant id or experiment flags that downstream spans annotate themselves
// with, and dropping them at the edge would break that contract.
func TestTraceContextMiddlewareExtractsBaggage(t *testing.T) {
	registerW3CPropagator()

	var captured baggage.Baggage
	handler := TraceContextMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = baggage.FromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("baggage", "tenant=acme,env=staging")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := captured.Member("tenant").Value(); got != "acme" {
		t.Errorf("baggage tenant = %q, want %q", got, "acme")
	}
	if got := captured.Member("env").Value(); got != "staging" {
		t.Errorf("baggage env = %q, want %q", got, "staging")
	}
}
