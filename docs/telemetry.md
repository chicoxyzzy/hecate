# Hecate Telemetry Schema

Hecate uses an OpenTelemetry-shaped runtime model for traces, logs, and metrics.

The goal is consistency first:

- use OpenTelemetry-style keys where they already exist
- keep `hecate.*` only for product-specific fields
- keep the same vocabulary across traces, logs, and metrics

## Current Telemetry Surface

In the current gateway-first state, telemetry comes from three places:

- response headers on `/v1/chat/completions`
- in-memory trace inspection over `GET /v1/traces?request_id=...`
- OTLP HTTP export for traces, metrics, and logs when enabled

Important runtime response headers include:

- `X-Request-Id`
- `X-Trace-Id`
- `X-Span-Id`
- `X-Runtime-Provider`
- `X-Runtime-Provider-Kind`
- `X-Runtime-Route-Reason`
- `X-Runtime-Model`
- `X-Runtime-Cache`
- `X-Runtime-Cache-Type`
- `X-Runtime-Cost-USD`

The trace endpoint returns:

- request id and trace id
- ordered spans
- route candidate and failover reports
- normalized timestamps and attributes

## OTLP Configuration

Hecate exports OTLP over HTTP when the corresponding signal is enabled.

Trace export:

- `GATEWAY_OTEL_TRACES_ENABLED`
- `GATEWAY_OTEL_TRACES_ENDPOINT`
- `GATEWAY_OTEL_TRACES_HEADERS`
- `GATEWAY_OTEL_TRACES_TIMEOUT`

Metric export:

- `GATEWAY_OTEL_METRICS_ENABLED`
- `GATEWAY_OTEL_METRICS_ENDPOINT`
- `GATEWAY_OTEL_METRICS_HEADERS`
- `GATEWAY_OTEL_METRICS_TIMEOUT`
- `GATEWAY_OTEL_METRICS_INTERVAL`

Log export:

- `GATEWAY_OTEL_LOGS_ENABLED`
- `GATEWAY_OTEL_LOGS_ENDPOINT`
- `GATEWAY_OTEL_LOGS_HEADERS`
- `GATEWAY_OTEL_LOGS_TIMEOUT`

Shared service identity:

- `GATEWAY_OTEL_SERVICE_NAME`

Current runtime behavior to know:

- traces export only when `GATEWAY_OTEL_TRACES_ENABLED=true`
- metrics export only when `GATEWAY_OTEL_METRICS_ENABLED=true`
- logs export only when `GATEWAY_OTEL_LOGS_ENABLED=true`
- if `GATEWAY_OTEL_LOGS_ENDPOINT` is not set, the logger falls back to the traces endpoint
- if `GATEWAY_OTEL_LOGS_HEADERS` is not set, the logger falls back to the traces headers
- if `GATEWAY_OTEL_LOGS_TIMEOUT` is not set, the logger falls back to the traces timeout

## Core Attributes

Standard or standard-shaped attributes used across the runtime:

- `service.name`
- `request.id`
- `trace.id`
- `span.id`
- `enduser.id`
- `tenant.id`
- `error.type`
- `error.message`
- `gen_ai.provider.name`
- `gen_ai.request.model`
- `gen_ai.response.model`
- `gen_ai.usage.input_tokens`
- `gen_ai.usage.output_tokens`
- `gen_ai.usage.total_tokens`

Hecate-specific runtime attributes:

- `hecate.phase`
- `hecate.result`
- `hecate.error.kind`
- `hecate.provider.kind`
- `hecate.route.reason`
- `hecate.cache.hit`
- `hecate.cache.type`
- `hecate.semantic.strategy`
- `hecate.semantic.index_type`
- `hecate.semantic.similarity`
- `hecate.cost.total_micros_usd`
- `hecate.retry.attempt_count`
- `hecate.retry.retry_count`
- `hecate.failover.from_provider`

## Request Results

Request outcomes are normalized to:

- `success`
- `denied`
- `error`

These are used in:

- request summary logs
- request outcome metrics
- selected trace events

## Trace Event Families

Runtime traces are grouped into a small set of event families:

- request parsing and validation
- governor and policy decisions
- routing decisions
- exact cache lookup
- semantic cache lookup and writeback
- provider execution, retry, failover, and health
- usage normalization
- cost calculation
- response return

Important spans created by the profiler currently include:

- `gateway.request`
- `gateway.request.parse`
- `gateway.governor`
- `gateway.router`
- `gateway.cache.exact`
- `gateway.cache.semantic`
- `gateway.provider`
- `gateway.usage`
- `gateway.cost`
- `gateway.response`

These spans back both OTLP traces and the in-memory trace inspection endpoint.

## Error Kinds

Hecate-specific error kinds currently include:

- `invalid_request`
- `request_denied`
- `router_failed`
- `budget_estimate_failed`
- `route_denied`
- `provider_call_failed`
- `retry_backoff_failed`
- `provider_health_degraded`
- `semantic_cache_store_failed`
- `usage_record_failed`

These appear in `hecate.error.kind` and are also copied into `error.type` for normalized runtime events.

## Metrics

Hecate records metrics using OpenTelemetry instruments and exports them through OTLP when enabled.

The current metric set includes:

- request totals by `result`
- request duration histograms
- finalized chat request totals
- accumulated estimated cost in micros USD
- accumulated input, output, and total token counts
- retry totals
- failover totals

Metric attributes reuse the same vocabulary as traces and logs, especially:

- `gen_ai.provider.name`
- `gen_ai.request.model`
- `gen_ai.response.model`
- `hecate.provider.kind`
- `hecate.cache.hit`
- `hecate.cache.type`
- `hecate.semantic.strategy`
- `hecate.semantic.index_type`
- `hecate.failover.from_provider`
- `result`

## Debugging Workflow

For request-level debugging, the most useful workflow today is:

1. send a request through `/v1/chat/completions`
2. capture `X-Request-Id` and `X-Trace-Id` from the response
3. call `GET /v1/traces?request_id=<request-id>`
4. inspect:
   - route candidates
   - failovers
   - cache decisions
   - provider latency
   - final route reason
   - span and event attributes

That path is often faster than going straight to your OTLP backend while you are developing locally.
