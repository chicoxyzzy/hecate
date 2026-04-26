package profiler

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestNewTracerProviderDisabled covers the common production case where OTLP
// export is off (e.g. local dev or environments without a collector). The
// provider must still be usable so callers don't have to nil-check it.
func TestNewTracerProviderDisabled(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerProviderOptions{
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Error("span context should be valid even when export is disabled")
	}
}

// TestNewTracerProviderEnabledWithoutEndpoint guards against a regression
// where an enabled flag with no endpoint was attempted to construct an OTLP
// exporter against an empty URL. Treating empty endpoint as "disabled" keeps
// half-configured deployments from crashing at boot.
func TestNewTracerProviderEnabledWithoutEndpoint(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerProviderOptions{
		Enabled:  true,
		Endpoint: "",
	})
	if err != nil {
		t.Fatalf("NewTracerProvider with empty endpoint: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil tracer provider")
	}
}

// TestNewTracerProviderHonorsResource verifies that resource attributes the
// caller supplies are visible on spans the provider produces. Without this
// guarantee, signal correlation across traces/metrics/logs in the backend
// would silently break since each signal would carry a different identity.
func TestNewTracerProviderHonorsResource(t *testing.T) {
	res := sdkresource.NewSchemaless(
		attribute.String("service.name", "hecate-test"),
		attribute.String("service.version", "9.9.9"),
	)
	tp, err := NewTracerProvider(context.Background(), TracerProviderOptions{
		Resource: res,
	})
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}
	exporter := tracetest.NewInMemoryExporter()
	tp.RegisterSpanProcessor(sdktrace.NewSimpleSpanProcessor(exporter))

	_, span := tp.Tracer("test").Start(context.Background(), "probe")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(spans))
	}
	got := map[string]string{}
	for _, kv := range spans[0].Resource.Attributes() {
		got[string(kv.Key)] = kv.Value.AsString()
	}
	if got["service.name"] != "hecate-test" {
		t.Errorf("span resource service.name = %q, want %q", got["service.name"], "hecate-test")
	}
	if got["service.version"] != "9.9.9" {
		t.Errorf("span resource service.version = %q, want %q", got["service.version"], "9.9.9")
	}
}

// TestNewTracerProviderHonorsSampler is the strongest check that the sampler
// plumbing is wired through. We force NeverSample and assert the resulting
// span carries no sampling flag — anything else means the option was dropped
// somewhere between the caller and sdktrace.WithSampler.
func TestNewTracerProviderHonorsSampler(t *testing.T) {
	tp, err := NewTracerProvider(context.Background(), TracerProviderOptions{
		Sampler: sdktrace.NeverSample(),
	})
	if err != nil {
		t.Fatalf("NewTracerProvider: %v", err)
	}

	_, span := tp.Tracer("test").Start(context.Background(), "test-span")
	defer span.End()
	if span.SpanContext().IsSampled() {
		t.Error("span should not be sampled when NeverSample is configured")
	}
}
