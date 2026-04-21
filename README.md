# Hecate

Hecate is an open-source AI agent runtime and LLM gateway written in Go.

It sits between agents and model providers and gives you one OpenAI-compatible surface for routing, caching, budget enforcement, observability, and control-plane management across cloud and local models.

## What It Does

- Exposes an OpenAI-compatible API:
  - `POST /v1/chat/completions`
  - `GET /v1/models`
  - `GET /v1/traces?request_id=...`
- Routes requests across cloud and local OpenAI-compatible providers
- Applies budget checks and policy restrictions before upstream execution
- Supports exact cache and semantic cache paths
- Calculates request cost from a local pricebook
- Records request traces, structured logs, and runtime metadata
- Provides admin APIs and a small operator UI for providers, budgets, tenants, and API keys

## Current Product Shape

Hecate is currently a single-binary monorepo centered on the `gateway` service.

Today it includes:

- OpenAI-compatible provider integration with configurable base URLs
- Cloud and local model support
- Rule-based routing with retry and failover
- Memory, Redis, and Postgres backends for several runtime stores
- Budget enforcement across global, provider, tenant, and tenant-provider scopes
- Persisted control-plane state for tenants and API keys
- OTel-shaped tracing and logs, with optional OTLP export
- React-based operator console

## Request Flow

```text
client
  -> auth
  -> governor
  -> exact cache
  -> router
  -> semantic cache
  -> provider
  -> usage normalization
  -> cost calculation
  -> telemetry and response
```

Useful response headers include:

- `X-Runtime-Provider`
- `X-Runtime-Provider-Kind`
- `X-Runtime-Model`
- `X-Runtime-Cache`
- `X-Runtime-Cache-Type`
- `X-Runtime-Cost-USD`
- `X-Request-Id`
- `X-Trace-Id`
- `X-Span-Id`

## Quick Start

1. Create a local env file:

```bash
cp .env.example .env
```

2. Configure at least one provider in `.env`.

Minimal example with one cloud provider and one local provider:

```bash
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini

OPENAI_PROVIDER_NAME=openai
OPENAI_PROVIDER_KIND=cloud
OPENAI_BASE_URL=https://api.openai.com
OPENAI_API_KEY=your_api_key_here
OPENAI_DEFAULT_MODEL=gpt-4o-mini

LOCAL_PROVIDER_ENABLED=true
LOCAL_PROVIDER_NAME=ollama
LOCAL_PROVIDER_KIND=local
LOCAL_PROVIDER_BASE_URL=http://127.0.0.1:11434
LOCAL_PROVIDER_DEFAULT_MODEL=llama3.1:8b
LOCAL_PROVIDER_MODELS=llama3.1:8b,llama3.2:3b
```

3. Run the gateway:

```bash
make dev
```

4. Run the UI in another shell:

```bash
make ui-install
make ui-dev
```

Default addresses:

- gateway: `http://127.0.0.1:8080`
- UI: `http://127.0.0.1:5173`

## Example Request

```bash
curl -i http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "Say hello in one short sentence." }
    ]
  }'
```

## Providers

The provider layer is vendor-neutral. Any upstream exposing an OpenAI-compatible API can be integrated without changing core gateway logic.

This makes Hecate usable with:

- OpenAI-compatible cloud providers
- Ollama
- LM Studio
- LocalAI
- llama.cpp-style servers

Hecate treats local and remote providers uniformly and can route between them based on explicit selection, defaults, policies, or fallback behavior.

## Auth, Budgets, and Control Plane

Auth supports:

- admin bearer token
- env-defined API keys
- persisted API keys managed through the control plane

Budgets currently support:

- `global`
- `provider`
- `tenant`
- `tenant_provider`

Control-plane capabilities include:

- tenant management
- API key management
- enable/disable and rotation flows
- lightweight audit history
- file, Redis, and Postgres storage backends

## Observability

Hecate includes:

- request IDs
- trace IDs and span IDs in responses
- structured OTel-shaped logs
- in-memory request trace snapshots available over HTTP
- OpenTelemetry trace export over OTLP HTTP
- optional OpenTelemetry log export over OTLP HTTP

Preferred signal-specific env naming:

- shared:
  - `GATEWAY_OTEL_SERVICE_NAME`
- traces: `GATEWAY_OTEL_TRACES_*`
- logs: `GATEWAY_OTEL_LOGS_*`

Legacy `GATEWAY_OTEL_*` trace variables are still accepted as backward-compatible fallbacks.

## UI

The operator console currently covers:

- provider and model visibility
- provider-aware playground
- runtime metadata and trace inspection
- budget inspection and admin mutations
- tenant and API key management
- recent control-plane activity

## Common Commands

```bash
make dev
make test
make ui-install
make ui-dev
make ui-build
```

`.env.example` is the source of truth for configuration.

## Repository Layout

```text
cmd/gateway           Main HTTP server
internal/api          HTTP handlers and middleware
internal/auth         Auth and principal resolution
internal/billing      Static pricebook and cost estimation
internal/cache        Exact and semantic cache backends
internal/config       Environment-based configuration
internal/controlplane Tenant, API-key, and audit-history persistence
internal/gateway      Core runtime pipeline
internal/governor     Policy and budget enforcement
internal/models       Canonical model identity helpers
internal/profiler     Tracing and trace snapshots
internal/providers    OpenAI-compatible provider implementations
internal/router       Routing logic
internal/storage      Redis and Postgres helpers
pkg/types             Vendor-neutral runtime types
ui                    Operator console
```

## Checklist

Implemented:

- [x] OpenAI-compatible chat completions endpoint
- [x] Unified model catalog across configured providers
- [x] Cloud and local provider support behind a vendor-neutral provider layer
- [x] Rule-based routing with retry and failover
- [x] Exact cache and semantic cache
- [x] Static pricebook and cost estimation
- [x] Budget enforcement with memory, Redis, and Postgres-backed state
- [x] Tenant-aware auth and control-plane state
- [x] Structured logs, request tracing, and OTLP export support
- [x] React operator UI

Next:

- [ ] Richer circuit-breaker and health-recovery behavior
- [ ] More advanced routing inputs and policy decisions
- [ ] Better semantic-cache debugging and trace views in the UI
- [ ] More provider presets and discovery paths
- [ ] Background retention and pruning workers
- [ ] Better budget UX, warnings, and history
- [ ] Sandbox runtime work in `cmd/sandboxd` and `internal/sandbox`
- [ ] Deployment examples for local and production-style environments
