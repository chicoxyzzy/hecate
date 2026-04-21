package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otmetric "go.opentelemetry.io/otel/metric"
)

type ChatMetricsRecord struct {
	Provider             string
	ProviderKind         string
	RequestedModel       string
	ResponseModel        string
	CacheHit             bool
	CacheType            string
	SemanticStrategy     string
	SemanticIndexType    string
	CostMicrosUSD        int64
	PromptTokens         int64
	CompletionTokens     int64
	TotalTokens          int64
	RetryCount           int
	FallbackFromProvider string
}

type Metrics struct {
	requestsTotal         otmetric.Int64Counter
	requestDuration       otmetric.Int64Histogram
	chatRequestsTotal     otmetric.Int64Counter
	costMicrosTotal       otmetric.Int64Counter
	promptTokensTotal     otmetric.Int64Counter
	completionTokensTotal otmetric.Int64Counter
	totalTokensTotal      otmetric.Int64Counter
	retriesTotal          otmetric.Int64Counter
	failoversTotal        otmetric.Int64Counter
}

func NewMetrics() *Metrics {
	metrics, err := NewMetricsWithMeterProvider(otel.GetMeterProvider())
	if err != nil {
		return &Metrics{}
	}
	return metrics
}

func NewMetricsWithMeterProvider(provider otmetric.MeterProvider) (*Metrics, error) {
	if provider == nil {
		provider = otel.GetMeterProvider()
	}

	meter := provider.Meter("github.com/hecate/agent-runtime/internal/telemetry")

	requestsTotal, err := meter.Int64Counter(
		"hecate.gateway.requests",
		otmetric.WithDescription("Total gateway requests grouped by result."),
		otmetric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	requestDuration, err := meter.Int64Histogram(
		"hecate.gateway.request.duration",
		otmetric.WithDescription("Gateway request duration."),
		otmetric.WithUnit("ms"),
	)
	if err != nil {
		return nil, err
	}

	chatRequestsTotal, err := meter.Int64Counter(
		"gen_ai.gateway.chat.requests",
		otmetric.WithDescription("Total chat completion responses finalized by the gateway."),
		otmetric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	costMicrosTotal, err := meter.Int64Counter(
		"gen_ai.gateway.cost",
		otmetric.WithDescription("Accumulated estimated cost for chat responses."),
		otmetric.WithUnit("1"),
	)
	if err != nil {
		return nil, err
	}

	promptTokensTotal, err := meter.Int64Counter(
		"gen_ai.client.tokens.input",
		otmetric.WithDescription("Accumulated prompt tokens."),
		otmetric.WithUnit("{token}"),
	)
	if err != nil {
		return nil, err
	}

	completionTokensTotal, err := meter.Int64Counter(
		"gen_ai.client.tokens.output",
		otmetric.WithDescription("Accumulated completion tokens."),
		otmetric.WithUnit("{token}"),
	)
	if err != nil {
		return nil, err
	}

	totalTokensTotal, err := meter.Int64Counter(
		"gen_ai.client.tokens.total",
		otmetric.WithDescription("Accumulated total tokens."),
		otmetric.WithUnit("{token}"),
	)
	if err != nil {
		return nil, err
	}

	retriesTotal, err := meter.Int64Counter(
		"hecate.gateway.retries",
		otmetric.WithDescription("Total provider retry attempts beyond the first request attempt."),
		otmetric.WithUnit("{retry}"),
	)
	if err != nil {
		return nil, err
	}

	failoversTotal, err := meter.Int64Counter(
		"hecate.gateway.failovers",
		otmetric.WithDescription("Total provider failover events."),
		otmetric.WithUnit("{failover}"),
	)
	if err != nil {
		return nil, err
	}

	return &Metrics{
		requestsTotal:         requestsTotal,
		requestDuration:       requestDuration,
		chatRequestsTotal:     chatRequestsTotal,
		costMicrosTotal:       costMicrosTotal,
		promptTokensTotal:     promptTokensTotal,
		completionTokensTotal: completionTokensTotal,
		totalTokensTotal:      totalTokensTotal,
		retriesTotal:          retriesTotal,
		failoversTotal:        failoversTotal,
	}, nil
}

func (m *Metrics) RecordRequestOutcome(ctx context.Context, result string, duration time.Duration) {
	if m == nil || result == "" {
		return
	}

	attrs := otmetric.WithAttributes(attribute.String(AttrHecateResult, result))
	m.requestsTotal.Add(ctx, 1, attrs)
	m.requestDuration.Record(ctx, duration.Milliseconds(), attrs)
}

func (m *Metrics) RecordChat(ctx context.Context, record ChatMetricsRecord) {
	if m == nil {
		return
	}

	attrs := make([]attribute.KeyValue, 0, 9)
	if record.Provider != "" {
		attrs = append(attrs, attribute.String(AttrGenAIProviderName, record.Provider))
	}
	if record.ProviderKind != "" {
		attrs = append(attrs, attribute.String(AttrHecateProviderKind, record.ProviderKind))
	}
	if record.RequestedModel != "" {
		attrs = append(attrs, attribute.String(AttrGenAIRequestModel, record.RequestedModel))
	}
	if record.ResponseModel != "" {
		attrs = append(attrs, attribute.String(AttrGenAIResponseModel, record.ResponseModel))
	}
	attrs = append(attrs, attribute.Bool(AttrHecateCacheHit, record.CacheHit))
	if record.CacheType != "" {
		attrs = append(attrs, attribute.String(AttrHecateCacheType, record.CacheType))
	}
	if record.SemanticStrategy != "" {
		attrs = append(attrs, attribute.String(AttrHecateSemanticStrategy, record.SemanticStrategy))
	}
	if record.SemanticIndexType != "" {
		attrs = append(attrs, attribute.String(AttrHecateSemanticIndexType, record.SemanticIndexType))
	}

	options := otmetric.WithAttributes(attrs...)
	m.chatRequestsTotal.Add(ctx, 1, options)

	if record.CostMicrosUSD > 0 {
		m.costMicrosTotal.Add(ctx, record.CostMicrosUSD, options)
	}
	if record.PromptTokens > 0 {
		m.promptTokensTotal.Add(ctx, record.PromptTokens, options)
	}
	if record.CompletionTokens > 0 {
		m.completionTokensTotal.Add(ctx, record.CompletionTokens, options)
	}
	if record.TotalTokens > 0 {
		m.totalTokensTotal.Add(ctx, record.TotalTokens, options)
	}
	if record.RetryCount > 0 {
		m.retriesTotal.Add(ctx, int64(record.RetryCount), options)
	}
	if record.FallbackFromProvider != "" {
		m.failoversTotal.Add(ctx, 1, otmetric.WithAttributes(append(attrs,
			attribute.String(AttrHecateFailoverFromProvider, record.FallbackFromProvider),
		)...))
	}
}
