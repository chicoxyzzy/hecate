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

For coding-runtime operations, `GET /admin/runtime/stats` is the primary live health snapshot. It includes queue depth/capacity, worker count, in-flight jobs, backend type (`queue_backend` / `store_backend`), and run-state counters.

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

Orchestrator-specific attributes include:

- `hecate.task.id`
- `hecate.task.status`
- `hecate.task.repo`
- `hecate.task.base_branch`
- `hecate.run.id`
- `hecate.run.number`
- `hecate.run.status`
- `hecate.run.duration_ms`
- `hecate.execution.kind`
- `hecate.step.id`
- `hecate.step.kind`
- `hecate.step.index`
- `hecate.step.tool_name`
- `hecate.step.duration_ms`
- `hecate.artifact.id`
- `hecate.artifact.kind`
- `hecate.artifact.size_bytes`
- `hecate.approval.id`
- `hecate.approval.kind`
- `hecate.approval.status`
- `hecate.approval.decision`
- `hecate.approval.wait_ms`
- `hecate.queue.backend`
- `hecate.queue.wait_ms`
- `hecate.worker.id`

Normalized results are:

- `success`
- `denied`
- `error`

## Traces

### Gateway Spans

Gateway traces are centered around a small set of runtime stages. Each stage maps to a child span under the root `gateway.request` span:

| Span name | Phase |
|---|---|
| `gateway.request` | Root span, present on every request |
| `gateway.request.parse` | Request parsing and validation |
| `gateway.governor` | Governor and policy decisions |
| `gateway.router` | Route selection |
| `gateway.cache.exact` | Exact cache lookup |
| `gateway.cache.semantic` | Semantic cache lookup and writeback |
| `gateway.provider` | Provider execution, retry, and failover |
| `gateway.usage` | Usage normalization |
| `gateway.cost` | Cost calculation |
| `gateway.response` | Response return |
| `gateway.runtime` | Catch-all for unclassified events |

When `GATEWAY_TRACE_BODIES=true`, the gateway also records redacted, size-capped trace events named:

- `request.body.captured`
- `response.body.captured`

These events contain truncated message or choice snapshots and are intended for local debugging and carefully controlled observability setups, not blanket production payload capture.

### Orchestrator Spans

Coding-runtime operations emit their own spans, grouped by lifecycle stage:

| Span name | Events |
|---|---|
| `orchestrator.task` | `orchestrator.task.started`, `orchestrator.task.finished` |
| `orchestrator.run` | `orchestrator.run.started`, `orchestrator.run.finished`, `orchestrator.run.failed` |
| `orchestrator.step` | `orchestrator.step.completed`, `orchestrator.step.failed` |
| `orchestrator.artifact` | `orchestrator.artifact.created`, `orchestrator.artifact.failed` |
| `orchestrator.approval` | `orchestrator.approval.requested`, `orchestrator.approval.resolved`, `orchestrator.approval.failed` |
| `orchestrator.queue` | `queue.enqueued`, `queue.claimed`, `queue.acked`, `queue.nacked`, `queue.lease_extended`, `queue.lease_extend_failed` |

Steps carry `hecate.step.duration_ms`. Runs carry `hecate.run.duration_ms`. Queue claim events carry `hecate.queue.wait_ms` — the time the run spent in the queue between enqueue and claim.

### Retention Spans

Retention manager runs emit events under the `retention.run` span:

| Event | When |
|---|---|
| `retention.run.started` | A retention pass begins |
| `retention.subsystem.finished` | One subsystem pruned successfully |
| `retention.subsystem.failed` | One subsystem pruning failed |
| `retention.run.finished` | All subsystems processed |
| `retention.history.persisted` | Run record written to history store |
| `retention.history.failed` | History write failed |

## Metrics

### Gateway Metrics

| Instrument | Type | Unit | Description |
|---|---|---|---|
| `hecate.gateway.requests` | Counter | `{request}` | Total gateway requests grouped by result |
| `hecate.gateway.request.duration` | Histogram | `ms` | Gateway request duration |
| `gen_ai.gateway.chat.requests` | Counter | `{request}` | Chat completion responses finalized |
| `gen_ai.gateway.cost` | Counter | `1` | Accumulated estimated cost in micros USD |
| `gen_ai.client.tokens.input` | Counter | `{token}` | Accumulated prompt tokens |
| `gen_ai.client.tokens.output` | Counter | `{token}` | Accumulated completion tokens |
| `gen_ai.client.tokens.total` | Counter | `{token}` | Accumulated total tokens |
| `hecate.gateway.retries` | Counter | `{retry}` | Provider retry attempts beyond the first |
| `hecate.gateway.failovers` | Counter | `{failover}` | Provider failover events |

### Orchestrator Metrics

| Instrument | Type | Unit | Description |
|---|---|---|---|
| `hecate.orchestrator.runs` | Counter | `{run}` | Total runs grouped by status and execution kind |
| `hecate.orchestrator.run.duration` | Histogram | `ms` | Run wall-clock duration |
| `hecate.orchestrator.queue.wait_duration` | Histogram | `ms` | Time a run spent in the queue before being claimed |
| `hecate.orchestrator.steps` | Counter | `{step}` | Total steps grouped by kind and result |
| `hecate.orchestrator.step.duration` | Histogram | `ms` | Step wall-clock duration |
| `hecate.orchestrator.approvals` | Counter | `{approval}` | Approval gates resolved, grouped by kind and decision |
| `hecate.orchestrator.approval.wait_duration` | Histogram | `ms` | Time a run spent waiting for an approval gate |
| `hecate.orchestrator.queue.lease_extend_failures` | Counter | `{failure}` | Queue lease extension failures |

Metric attributes reuse the same vocabulary as traces — provider, model, cache, failover, result, step kind, approval decision, queue backend, and run status fields.

## Error And Limit Signals

Two operational response classes are worth calling out:

- budget exhaustion is returned as HTTP `402` with a `payment_required` error shape
- rate limiting is returned as HTTP `429` with a `rate_limit_error` error shape

When rate limiting is enabled, the token-bucket limiter also exposes reset and remaining-budget information through the `X-RateLimit-*` headers above.

The `hecate.error.kind` attribute on error events is clamped to a closed set of known values. Any value outside this set is normalized to `other` to prevent high-cardinality label explosions in metric exporters and trace backends.

## Local Debugging Workflow

For request-level debugging:

1. Send a request through `/v1/chat/completions`.
2. Capture `X-Request-Id` and `X-Trace-Id` from the response.
3. Call `GET /v1/traces?request_id=<request-id>`.
4. Inspect route candidates, failovers, cache decisions, provider latency, final route reason, and span attributes.

That local HTTP path is usually faster than jumping straight into an OTLP backend while developing.

For task/run debugging, use `GET /v1/tasks/{task_id}/runs/{run_id}` to retrieve the run record with its `trace_id`, then look up the trace with `GET /v1/traces?request_id=<request_id>`. The queue wait and step durations are recorded as span attributes on the relevant spans.
