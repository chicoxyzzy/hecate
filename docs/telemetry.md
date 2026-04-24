# Hecate Telemetry

Hecate uses OpenTelemetry-style traces, metrics, and logs, but the important thing for operators is simpler than that:

- every request gets stable runtime identifiers
- chat responses expose routing and cache metadata in headers
- traces are inspectable locally over HTTP
- OTLP export is available for traces, metrics, and logs

The runtime keeps standard OpenTelemetry keys where they already fit and uses `hecate.*` only for product-specific fields.

## What You Can Inspect Today

Telemetry currently shows up in three places:

- response headers
- `GET /v1/traces?request_id=...`
- OTLP HTTP export when enabled

For request responses, the most useful headers are:

- `X-Request-Id`
- `X-Trace-Id`
- `X-Span-Id`
- `X-Runtime-Provider`
- `X-Runtime-Provider-Kind`
- `X-Runtime-Route-Reason`
- `X-Runtime-Requested-Model`
- `X-Runtime-Model`
- `X-Runtime-Cache`
- `X-Runtime-Cache-Type`
- `X-Runtime-Cost-USD`
- `X-RateLimit-Limit`
- `X-RateLimit-Remaining`
- `X-RateLimit-Reset`

The runtime metadata headers are most relevant on `/v1/chat/completions` and `/v1/messages`.

Task and run lifecycle endpoints also return `X-Trace-Id` and `X-Span-Id` on key execution actions such as run start and approval resolution.

The trace endpoint returns:

- the request id and trace id
- ordered spans with timestamps and attributes
- route candidates
- failover history
- the final provider, model, and route reason

## OTLP Configuration

All OTLP export is over HTTP. Each signal is enabled independently.

Shared identity:

- `GATEWAY_OTEL_SERVICE_NAME`

Traces:

- `GATEWAY_OTEL_TRACES_ENABLED`
- `GATEWAY_OTEL_TRACES_ENDPOINT`
- `GATEWAY_OTEL_TRACES_HEADERS`
- `GATEWAY_OTEL_TRACES_TIMEOUT`

Metrics:

- `GATEWAY_OTEL_METRICS_ENABLED`
- `GATEWAY_OTEL_METRICS_ENDPOINT`
- `GATEWAY_OTEL_METRICS_HEADERS`
- `GATEWAY_OTEL_METRICS_TIMEOUT`
- `GATEWAY_OTEL_METRICS_INTERVAL`

Logs:

- `GATEWAY_OTEL_LOGS_ENABLED`
- `GATEWAY_OTEL_LOGS_ENDPOINT`
- `GATEWAY_OTEL_LOGS_HEADERS`
- `GATEWAY_OTEL_LOGS_TIMEOUT`

Behavior to know:

- traces export only when `GATEWAY_OTEL_TRACES_ENABLED=true`
- metrics export only when `GATEWAY_OTEL_METRICS_ENABLED=true`
- logs export only when `GATEWAY_OTEL_LOGS_ENABLED=true`
- if log endpoint, headers, or timeout are omitted, log export falls back to the trace signal settings

Trace body capture is configured separately from OTLP export:

- `GATEWAY_TRACE_BODIES`
- `GATEWAY_TRACE_BODY_MAX_BYTES`

## Core Vocabulary

Common standard or standard-shaped attributes include:

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

Common Hecate-specific attributes include:

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

Normalized results are:

- `success`
- `denied`
- `error`

## Traces

Gateway traces are centered around a small set of runtime stages:

- request parsing and validation
- governor and policy decisions
- routing
- exact cache lookup
- semantic cache lookup and writeback
- provider execution, retry, and failover
- usage normalization
- cost calculation
- response return

Important span names include:

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

These spans back both local trace inspection and OTLP trace export.

When `GATEWAY_TRACE_BODIES=true`, the gateway also records redacted, size-capped trace events named:

- `request.body.captured`
- `response.body.captured`

These events contain truncated message or choice snapshots and are intended for local debugging and carefully controlled observability setups, not blanket production payload capture.

## Metrics

The current metric set is intentionally small and request-focused. Exported instruments include:

- `hecate.gateway.requests`
- `hecate.gateway.request.duration`
- `gen_ai.gateway.chat.requests`
- `gen_ai.gateway.cost`
- `gen_ai.client.tokens.input`
- `gen_ai.client.tokens.output`
- `gen_ai.client.tokens.total`
- `hecate.gateway.retries`
- `hecate.gateway.failovers`

Metric attributes reuse the same vocabulary as traces and logs, especially provider, model, cache, failover, and result fields.

## Error And Limit Signals

Two operational response classes are worth calling out:

- budget exhaustion is returned as HTTP `402` with a `payment_required` error shape
- rate limiting is returned as HTTP `429` with a `rate_limit_error` error shape

When rate limiting is enabled, the token-bucket limiter also exposes reset and remaining-budget information through the `X-RateLimit-*` headers above.

## Local Debugging Workflow

For request-level debugging:

1. Send a request through `/v1/chat/completions`.
2. Capture `X-Request-Id` and `X-Trace-Id` from the response.
3. Call `GET /v1/traces?request_id=<request-id>`.
4. Inspect route candidates, failovers, cache decisions, provider latency, final route reason, and span attributes.

That local HTTP path is usually faster than jumping straight into an OTLP backend while developing.
