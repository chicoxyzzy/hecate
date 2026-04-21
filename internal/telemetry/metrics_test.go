package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricsRecordRequestOutcomeProducesOTelMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := NewMetricsWithMeterProvider(provider)
	if err != nil {
		t.Fatalf("NewMetricsWithMeterProvider() error = %v", err)
	}

	metrics.RecordRequestOutcome(context.Background(), ResultSuccess, 125*time.Millisecond)

	collected := collectMetrics(t, reader)
	requests := findMetric[metricdata.Sum[int64]](t, collected, "hecate.gateway.requests")
	if len(requests.DataPoints) != 1 {
		t.Fatalf("request data points = %d, want 1", len(requests.DataPoints))
	}
	if requests.DataPoints[0].Value != 1 {
		t.Fatalf("request count = %d, want 1", requests.DataPoints[0].Value)
	}
	if got := attrValue(requests.DataPoints[0].Attributes, MetricLabelResult); got != ResultSuccess {
		t.Fatalf("result attribute = %q, want %q", got, ResultSuccess)
	}

	duration := findMetric[metricdata.Histogram[int64]](t, collected, "hecate.gateway.request.duration")
	if len(duration.DataPoints) != 1 {
		t.Fatalf("duration data points = %d, want 1", len(duration.DataPoints))
	}
	if duration.DataPoints[0].Count != 1 {
		t.Fatalf("duration count = %d, want 1", duration.DataPoints[0].Count)
	}
	if duration.DataPoints[0].Sum != 125 {
		t.Fatalf("duration sum = %d, want 125", duration.DataPoints[0].Sum)
	}
}

func TestMetricsRecordChatTracksSemanticAndRetryDetails(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := NewMetricsWithMeterProvider(provider)
	if err != nil {
		t.Fatalf("NewMetricsWithMeterProvider() error = %v", err)
	}

	metrics.RecordChat(context.Background(), ChatMetricsRecord{
		Provider:             "ollama",
		ProviderKind:         "local",
		RequestedModel:       "llama3.1:8b",
		ResponseModel:        "llama3.1:8b",
		CacheHit:             true,
		CacheType:            "semantic",
		SemanticStrategy:     "postgres_pgvector",
		SemanticIndexType:    "hnsw",
		CostMicrosUSD:        123,
		PromptTokens:         12,
		CompletionTokens:     5,
		TotalTokens:          17,
		RetryCount:           2,
		FallbackFromProvider: "local-primary",
	})

	collected := collectMetrics(t, reader)

	chatRequests := findMetric[metricdata.Sum[int64]](t, collected, "gen_ai.gateway.chat.requests")
	if len(chatRequests.DataPoints) != 1 {
		t.Fatalf("chat request data points = %d, want 1", len(chatRequests.DataPoints))
	}
	if chatRequests.DataPoints[0].Value != 1 {
		t.Fatalf("chat request count = %d, want 1", chatRequests.DataPoints[0].Value)
	}
	if got := attrValue(chatRequests.DataPoints[0].Attributes, AttrHecateCacheType); got != "semantic" {
		t.Fatalf("cache_type attribute = %q, want semantic", got)
	}
	if got := attrValue(chatRequests.DataPoints[0].Attributes, AttrHecateSemanticStrategy); got != "postgres_pgvector" {
		t.Fatalf("semantic strategy attribute = %q, want postgres_pgvector", got)
	}

	cost := findMetric[metricdata.Sum[int64]](t, collected, "gen_ai.gateway.cost")
	if cost.DataPoints[0].Value != 123 {
		t.Fatalf("cost total = %d, want 123", cost.DataPoints[0].Value)
	}

	retries := findMetric[metricdata.Sum[int64]](t, collected, "hecate.gateway.retries")
	if retries.DataPoints[0].Value != 2 {
		t.Fatalf("retry total = %d, want 2", retries.DataPoints[0].Value)
	}

	failovers := findMetric[metricdata.Sum[int64]](t, collected, "hecate.gateway.failovers")
	if failovers.DataPoints[0].Value != 1 {
		t.Fatalf("failover total = %d, want 1", failovers.DataPoints[0].Value)
	}
	if got := attrValue(failovers.DataPoints[0].Attributes, AttrHecateFailoverFromProvider); got != "local-primary" {
		t.Fatalf("failover attribute = %q, want local-primary", got)
	}
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return collected
}

func findMetric[T any](t *testing.T, collected metricdata.ResourceMetrics, name string) T {
	t.Helper()

	for _, scope := range collected.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != name {
				continue
			}
			data, ok := metric.Data.(T)
			if !ok {
				t.Fatalf("metric %q type = %T, want requested type", name, metric.Data)
			}
			return data
		}
	}

	t.Fatalf("metric %q not found", name)
	var zero T
	return zero
}

func attrValue(set attribute.Set, key string) string {
	value, ok := set.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	if value.Type() != attribute.STRING {
		return ""
	}
	return value.AsString()
}
