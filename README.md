# Hecate

Hecate is an open-source AI agent runtime and LLM gateway written in Go.

It sits between agents and model providers and gives you a single OpenAI-compatible surface for routing, caching, budget enforcement, provider normalization, and control-plane management across cloud and local models.

## What It Does

- exposes `POST /v1/chat/completions`
- exposes `GET /v1/models`
- supports OpenAI-compatible cloud and local providers
- routes requests with `explicit_or_default` and `local_first`
- applies exact caching with memory or Redis
- estimates cost with a static pricebook
- enforces budgets across global, provider, tenant, and tenant-provider scopes
- supports admin auth, tenant API keys, and a persisted control plane
- records lightweight tracing and runtime metadata
- includes a React + TypeScript operator UI

## Request Flow

```text
auth -> governor -> cache -> router -> provider -> usage normalization -> cost calculation -> telemetry
```

Useful response headers:

- `X-Runtime-Provider`
- `X-Runtime-Provider-Kind`
- `X-Runtime-Requested-Model`
- `X-Runtime-Model`
- `X-Runtime-Cache`
- `X-Runtime-Cost-USD`
- `X-Request-Id`

## Quick Start

1. Create a local env file:

```bash
cp .env.example .env
```

2. Adjust `.env` for your providers and routing strategy.

Example mixed setup with both cloud and local providers enabled:

```bash
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini
GATEWAY_ROUTER_STRATEGY=local_first
GATEWAY_ROUTER_FALLBACK_PROVIDER=openai

OPENAI_PROVIDER_NAME=openai
OPENAI_PROVIDER_KIND=cloud
OPENAI_STUB_MODE=false
OPENAI_API_KEY=your_api_key_here
OPENAI_BASE_URL=https://api.openai.com
OPENAI_DEFAULT_MODEL=gpt-4o-mini

LOCAL_PROVIDER_ENABLED=true
LOCAL_PROVIDER_NAME=ollama
LOCAL_PROVIDER_KIND=local
LOCAL_PROVIDER_BASE_URL=http://127.0.0.1:11434
LOCAL_PROVIDER_DEFAULT_MODEL=llama3.1:8b
LOCAL_PROVIDER_MODELS=llama3.1:8b,llama3.2:3b
LOCAL_PROVIDER_ALLOW_ANY_MODEL=false
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

Common commands:

- `make dev`
- `make test`
- `make ui-install`
- `make ui-dev`
- `make ui-build`

## API

Chat completion example:

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

Models example:

```bash
curl -s http://127.0.0.1:8080/v1/models | jq
```

## Providers

The provider layer is vendor-neutral. Any upstream exposing an OpenAI-compatible API can be configured without changing gateway logic.

Current model supports:

- configurable base URL per provider
- provider kind metadata: `cloud` or `local`
- default model and discovered or configured model lists
- explicit model/provider routing and local-first routing with cloud fallback
- zero-cost or custom pricing for local models

Examples of local providers that fit this model:

- Ollama
- LM Studio
- LocalAI
- llama.cpp-compatible bridges

## Auth And Control Plane

Hecate supports:

- admin bearer token via `GATEWAY_AUTH_TOKEN`
- env-defined API keys via `GATEWAY_API_KEYS_JSON`
- persisted tenants and API keys via the control plane

Tenant API keys can:

- access `/v1/chat/completions`
- access `/v1/models`
- be bound to a tenant
- restrict providers and models

Admin APIs include:

- `GET /admin/providers`
- `GET /admin/budget`
- `POST /admin/budget/reset`
- `POST /admin/budget/topup`
- `POST /admin/budget/limit`
- `GET /admin/control-plane`
- `POST /admin/control-plane/tenants`
- `POST /admin/control-plane/api-keys`
- `POST /admin/control-plane/tenants/enabled`
- `POST /admin/control-plane/tenants/delete`
- `POST /admin/control-plane/api-keys/enabled`
- `POST /admin/control-plane/api-keys/rotate`
- `POST /admin/control-plane/api-keys/delete`

Control-plane backends:

- `file`
- `redis`

The control plane also keeps lightweight audit history for admin mutations, which is visible in the admin API and UI.

## Budgets

Budget enforcement currently supports:

- `global`
- `provider`
- `tenant`
- `tenant_provider`

Backends:

- `memory`
- `redis`

If no stored scoped limit exists, the gateway falls back to `GATEWAY_MAX_BUDGET_MICROS_USD`.

## UI

The operator console is built with React, TypeScript, Vite, Tailwind CSS, and `pnpm`.

Current UI coverage:

- health and provider status
- model catalog with Cloud and Local grouping
- provider-aware playground
- runtime header inspection
- budget inspection and admin mutations
- tenant and API key management
- recent control-plane activity

## Key Configuration

Core gateway settings:

```bash
GATEWAY_ADDRESS=:8080
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini
GATEWAY_ROUTER_STRATEGY=explicit_or_default
GATEWAY_ROUTER_FALLBACK_PROVIDER=

GATEWAY_AUTH_TOKEN=
GATEWAY_API_KEYS_JSON=

GATEWAY_CACHE_BACKEND=memory
GATEWAY_CACHE_TTL=5m

GATEWAY_BUDGET_BACKEND=memory
GATEWAY_BUDGET_KEY=global
GATEWAY_BUDGET_SCOPE=global
GATEWAY_BUDGET_TENANT_FALLBACK=anonymous
GATEWAY_MAX_BUDGET_MICROS_USD=5000000
GATEWAY_MAX_PROMPT_TOKENS=64000

GATEWAY_CONTROL_PLANE_BACKEND=none
GATEWAY_CONTROL_PLANE_FILE=
GATEWAY_CONTROL_PLANE_KEY=control-plane
```

Redis example:

```bash
REDIS_ADDRESS=127.0.0.1:6379
REDIS_DB=0
REDIS_PREFIX=agent-runtime
REDIS_TIMEOUT=3s
```

Policy and routing example:

```bash
GATEWAY_ROUTE_MODE=local_only
GATEWAY_ALLOWED_PROVIDERS=ollama
GATEWAY_DENIED_MODELS=gpt-4o-mini
GATEWAY_ALLOWED_PROVIDER_KINDS=local
```

For more complete setup examples, use `.env.example` as the source of truth.

## Repository Layout

```text
cmd/gateway           Main HTTP server
internal/api          HTTP handlers and middleware
internal/auth         Auth and principal resolution
internal/billing      Static pricebook and cost estimation
internal/cache        Exact cache backends
internal/config       Environment-based configuration
internal/controlplane Tenant, API-key, and audit-history persistence
internal/gateway      Core runtime pipeline
internal/governor     Policy and budget enforcement
internal/models       Canonical model identity helpers
internal/profiler     Lightweight tracing
internal/providers    OpenAI-compatible provider implementations
internal/router       Routing logic
internal/storage      Storage helpers such as Redis primitives
pkg/types             Vendor-neutral runtime types
ui                    Operator console
```

## Checklist

Implemented now:

- [x] OpenAI-compatible `POST /v1/chat/completions`
- [x] Unified `GET /v1/models` across configured providers
- [x] Vendor-neutral OpenAI-compatible provider layer
- [x] Cloud and local provider support
- [x] Rule-based routing with explicit and local-first strategies
- [x] Exact cache with memory and Redis backends
- [x] Static pricebook and cost estimation
- [x] Shared budgets with scoped limits and admin mutation endpoints
- [x] Structured logs, request IDs, and lightweight in-process tracing
- [x] Admin auth, tenant API keys, and tenant-aware restrictions
- [x] File- and Redis-backed control plane
- [x] Control-plane lifecycle operations and audit history
- [x] React operator UI for playground and admin operations

Next meaningful steps:

- [ ] Add semantic cache behind the existing cache abstraction
- [ ] Expand routing beyond simple rules to include richer policy inputs
- [ ] Add persistent tracing and telemetry export, starting with OpenTelemetry
- [ ] Add more provider presets and discovery paths on top of the existing generic provider layer
- [ ] Add Postgres-backed control-plane and budget storage
- [ ] Add budget history, threshold warnings, and better operator UX
- [ ] Start the sandbox runtime path in `cmd/sandboxd` and `internal/sandbox`
- [ ] Add deployment examples for local dev and production-style environments
