# Hecate

Hecate is a production-oriented Go runtime and gateway for AI-agent workloads.

It sits between agents and model providers and gives you a single OpenAI-compatible surface for routing, caching, budgets, and operator control. The current codebase is an MVP, but it already supports a working vertical slice across cloud and local models.

## Overview

Hecate currently provides:

- an OpenAI-compatible `POST /v1/chat/completions` endpoint
- a unified `GET /v1/models` catalog across configured providers
- exact caching with memory or Redis backends
- static cost estimation with zero-cost defaults for local models
- rule-based routing with `explicit_or_default` and `local_first`
- shared budget enforcement with scoped limits
- request IDs, structured logs, and lightweight tracing
- admin endpoints for providers, budgets, and control-plane state
- a React + TypeScript operator console

Supported provider style:

- any provider exposing an OpenAI-compatible chat API
- cloud providers such as OpenAI-compatible hosted APIs
- local providers such as Ollama, LM Studio, LocalAI, and llama.cpp bridges

## How Requests Flow

Requests pass through a consistent runtime pipeline:

```text
auth -> governor -> cache -> router -> provider -> usage normalization -> cost calculation -> telemetry
```

The gateway returns normal OpenAI-style JSON plus runtime headers such as:

- `X-Runtime-Provider`
- `X-Runtime-Provider-Kind`
- `X-Runtime-Requested-Model`
- `X-Runtime-Model`
- `X-Runtime-Cache`
- `X-Runtime-Cost-USD`
- `X-Request-Id`

## Quick Start

1. Copy the environment template:

```bash
cp .env.example .env
```

2. Pick a setup in `.env`.

Cloud-only example:

```bash
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini
GATEWAY_ROUTER_STRATEGY=explicit_or_default

OPENAI_PROVIDER_NAME=openai
OPENAI_PROVIDER_KIND=cloud
OPENAI_STUB_MODE=false
OPENAI_API_KEY=your_api_key_here
OPENAI_BASE_URL=https://api.openai.com
OPENAI_DEFAULT_MODEL=gpt-4o-mini
```

Local-first example with cloud fallback:

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

3. Run the backend:

```bash
make dev
```

4. In another shell, run the UI:

```bash
make ui-install
make ui-dev
```

Defaults:

- gateway: `http://127.0.0.1:8080`
- UI: `http://127.0.0.1:5173`

Useful commands:

- `make test`
- `make dev`
- `make run`
- `make ui-install`
- `make ui-dev`
- `make ui-build`

The UI uses the Volta pinning declared in [ui/package.json](/Users/chicoxyzzy/dev/hecate/ui/package.json).

## API

### Chat Completions

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

If you repeat the exact same request, `X-Runtime-Cache` should move from `false` to `true`.

### Models

```bash
curl -s http://127.0.0.1:8080/v1/models | jq
```

Each model entry includes:

- `id`
- `owned_by`
- `metadata.provider`
- `metadata.provider_kind`
- `metadata.default`
- `metadata.discovery_source`

## Authentication

Hecate supports multiple auth sources behind the same `Authorization: Bearer ...` header.

Auth sources:

- admin token from `GATEWAY_AUTH_TOKEN`
- tenant API keys from `GATEWAY_API_KEYS_JSON`
- persisted tenant and API-key state from the optional control-plane file backend

Admin token behavior:

- can call `/v1/*`
- can call admin endpoints such as `/admin/budget`, `/admin/providers`, and `/admin/control-plane`

Tenant API key behavior:

- can call `/v1/chat/completions`
- can call `/v1/models`
- cannot call admin endpoints
- can be bound to a tenant
- can restrict allowed providers and models

Example env-defined tenant key:

```bash
GATEWAY_API_KEYS_JSON='[
  {
    "name": "team-a-dev",
    "key": "hecate-team-a-dev",
    "tenant": "team-a",
    "role": "tenant",
    "allowed_providers": ["openai", "ollama"],
    "allowed_models": ["gpt-4o-mini", "llama3.1:8b"]
  }
]'
```

If a key is bound to a tenant, the backend treats that binding as the source of truth and rejects attempts to impersonate another tenant through the OpenAI-compatible `user` field.

## Budgets

Budget state supports memory or Redis backends.

Supported scopes:

- `global`
- `provider`
- `tenant`
- `tenant_provider`

Budget semantics:

- `reset` clears tracked spend for a scope
- `topup` increases the stored limit for a scope
- `limit` sets the stored limit for a scope to an exact value
- if no stored limit exists, the governor falls back to `GATEWAY_MAX_BUDGET_MICROS_USD`

Admin endpoints:

- `GET /admin/budget`
- `POST /admin/budget/reset`
- `POST /admin/budget/topup`
- `POST /admin/budget/limit`

Examples:

```bash
curl -s http://127.0.0.1:8080/admin/budget | jq
```

```bash
curl -s "http://127.0.0.1:8080/admin/budget?scope=tenant_provider&tenant=team-a&provider=ollama" | jq
```

```bash
curl -s -X POST http://127.0.0.1:8080/admin/budget/topup \
  -H 'Content-Type: application/json' \
  -d '{"scope":"tenant_provider","tenant":"team-a","provider":"ollama","amount_micros_usd":2000000}' | jq
```

## Control Plane

Hecate can persist tenants and API keys outside environment variables.

Supported backends:

- `file`
- `redis`

File-backed example:

```bash
GATEWAY_CONTROL_PLANE_BACKEND=file
GATEWAY_CONTROL_PLANE_FILE=./data/control-plane.json
```

Redis-backed example:

```bash
GATEWAY_CONTROL_PLANE_BACKEND=redis
GATEWAY_CONTROL_PLANE_KEY=control-plane
REDIS_ADDRESS=127.0.0.1:6379
REDIS_DB=0
REDIS_PREFIX=agent-runtime
REDIS_TIMEOUT=3s
```

Admin endpoints:

- `GET /admin/control-plane`
- `POST /admin/control-plane/tenants`
- `POST /admin/control-plane/api-keys`
- `POST /admin/control-plane/tenants/enabled`
- `POST /admin/control-plane/tenants/delete`
- `POST /admin/control-plane/api-keys/enabled`
- `POST /admin/control-plane/api-keys/rotate`
- `POST /admin/control-plane/api-keys/delete`

Control-plane state now includes lightweight audit history for admin mutations. Recent events are stored alongside tenants and API keys and exposed in the admin API and UI.

The operator console includes a control-plane panel for admin users. Tenant-key users still get a usable non-admin console instead of a broken dashboard.

Example persisted state:

- [examples/control-plane/control-plane.sample.json](/Users/chicoxyzzy/dev/hecate/examples/control-plane/control-plane.sample.json)

## UI Console

The UI is built with React, TypeScript, Vite, Tailwind CSS, and `pnpm`.

Current capabilities:

- gateway health snapshot
- provider health list
- local runtime issue hints
- discovered model catalog
- grouped Cloud and Local model selection
- provider-aware chat playground
- runtime header inspection
- budget view and admin budget actions
- bearer-token input stored in browser local storage
- control-plane management for admins
- recent control-plane activity feed

## Provider Configuration

The provider layer is intentionally vendor-neutral. Any OpenAI-compatible upstream can be used without changing core gateway logic.

Current model:

- configurable base URL per provider
- provider kind metadata: `cloud` or `local`
- per-provider default model and supported-model hints
- optional fallback from local to cloud
- zero-cost local pricing by default, with optional custom pricing

Examples:

Ollama:

```bash
LOCAL_PROVIDER_ENABLED=true
LOCAL_PROVIDER_NAME=ollama
LOCAL_PROVIDER_KIND=local
LOCAL_PROVIDER_BASE_URL=http://127.0.0.1:11434
LOCAL_PROVIDER_DEFAULT_MODEL=llama3.1:8b
LOCAL_PROVIDER_MODELS=llama3.1:8b,llama3.2:3b
LOCAL_PROVIDER_ALLOW_ANY_MODEL=false
```

LM Studio:

```bash
LOCAL_PROVIDER_ENABLED=true
LOCAL_PROVIDER_NAME=lmstudio
LOCAL_PROVIDER_KIND=local
LOCAL_PROVIDER_BASE_URL=http://127.0.0.1:1234/v1
LOCAL_PROVIDER_DEFAULT_MODEL=local-model
LOCAL_PROVIDER_ALLOW_ANY_MODEL=true
```

LocalAI:

```bash
LOCAL_PROVIDER_ENABLED=true
LOCAL_PROVIDER_NAME=localai
LOCAL_PROVIDER_KIND=local
LOCAL_PROVIDER_BASE_URL=http://127.0.0.1:8080/v1
LOCAL_PROVIDER_DEFAULT_MODEL=llama3
LOCAL_PROVIDER_ALLOW_ANY_MODEL=true
```

Note:

- some local servers expose a base URL ending in `/v1`
- others expose the root and expect the gateway to append `/v1/chat/completions`
- the provider layer handles both forms

## Configuration

Core environment variables:

```bash
GATEWAY_ADDRESS=:8080
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini
GATEWAY_ROUTER_STRATEGY=explicit_or_default
GATEWAY_ROUTER_FALLBACK_PROVIDER=

GATEWAY_AUTH_TOKEN=
GATEWAY_API_KEYS_JSON=
GATEWAY_CONTROL_PLANE_BACKEND=none
GATEWAY_CONTROL_PLANE_FILE=
GATEWAY_CONTROL_PLANE_KEY=control-plane

GATEWAY_CACHE_TTL=5m
GATEWAY_CACHE_BACKEND=memory

GATEWAY_MAX_PROMPT_TOKENS=64000
GATEWAY_MAX_BUDGET_MICROS_USD=5000000
GATEWAY_BUDGET_BACKEND=memory
GATEWAY_BUDGET_KEY=global
GATEWAY_BUDGET_SCOPE=global
GATEWAY_BUDGET_TENANT_FALLBACK=anonymous

GATEWAY_ROUTE_MODE=any
GATEWAY_ALLOWED_PROVIDERS=
GATEWAY_DENIED_PROVIDERS=
GATEWAY_ALLOWED_MODELS=
GATEWAY_DENIED_MODELS=
GATEWAY_ALLOWED_PROVIDER_KINDS=
```

Redis cache example:

```bash
GATEWAY_CACHE_BACKEND=redis
REDIS_ADDRESS=127.0.0.1:6379
REDIS_DB=0
REDIS_PREFIX=agent-runtime
REDIS_TIMEOUT=3s
```

Redis-backed budget example:

```bash
GATEWAY_MAX_BUDGET_MICROS_USD=5000000
GATEWAY_BUDGET_BACKEND=redis
GATEWAY_BUDGET_KEY=global
GATEWAY_BUDGET_SCOPE=tenant_provider
GATEWAY_BUDGET_TENANT_FALLBACK=anonymous
REDIS_ADDRESS=127.0.0.1:6379
REDIS_DB=0
REDIS_PREFIX=agent-runtime
REDIS_TIMEOUT=3s
```

Policy example:

```bash
GATEWAY_ROUTE_MODE=local_only
GATEWAY_ALLOWED_PROVIDERS=ollama
GATEWAY_DENIED_MODELS=gpt-4o-mini
GATEWAY_ALLOWED_PROVIDER_KINDS=local
```

Advanced option:

- `GATEWAY_PROVIDERS_JSON` for explicit multi-provider configuration beyond the built-in cloud plus optional local setup

## Repository Layout

```text
cmd/gateway          Main HTTP server
ui                   React operator console
internal/api         HTTP handlers, middleware, wire types
internal/auth        Auth and principal resolution
internal/billing     Static pricebook and cost estimation
internal/cache       Exact cache backends
internal/config      Environment-based configuration
internal/controlplane Tenant, API-key, and audit-history persistence
internal/gateway     Core request pipeline
internal/governor    Policy and budget enforcement
internal/models      Canonical model identity helpers
internal/profiler    Lightweight tracing
internal/providers   OpenAI-compatible provider implementations
internal/router      Rule-based routing
internal/storage     Durable backend helpers such as Redis primitives
pkg/types            Vendor-neutral runtime types
```

## Status

Implemented:

- [x] OpenAI-compatible chat and models endpoints
- [x] Cloud and local OpenAI-compatible provider support
- [x] Rule-based routing with local-first support
- [x] Exact cache with memory and Redis backends
- [x] Static pricebook and cost estimation
- [x] Shared budgets with scoped limits and admin mutation endpoints
- [x] Structured logs, request IDs, and lightweight tracing
- [x] Admin bearer auth and tenant-bound API keys
- [x] Persistent control plane with file and Redis backends
- [x] Control-plane lifecycle operations and audit history
- [x] React operator console

Still open:

- [ ] Semantic cache
- [ ] Persistent trace backend
- [ ] OpenTelemetry export
- [ ] Additional provider integrations beyond the generic OpenAI-compatible path
- [ ] More expressive policy engine
- [ ] Sandbox worker runtime
- [ ] Postgres-backed control-plane storage
- [ ] Budget and control-plane audit export integrations
- [ ] Budget history and warning UX
- [ ] Richer dashboard and trace visualization
- [ ] Production deployment assets
