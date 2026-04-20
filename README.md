# Hecate

Hecate is an open-source AI agent runtime and LLM gateway written in Go.

It sits between agents and model providers and gives you a single OpenAI-compatible surface for routing, caching, budget enforcement, provider normalization, and control-plane management across cloud and local models.

## Current Scope

- OpenAI-compatible API surface:
  - `POST /v1/chat/completions`
  - `GET /v1/models`
- Vendor-neutral provider layer for OpenAI-compatible cloud and local endpoints
- Rule-based routing with `explicit_or_default` and `local_first`
- Exact cache with memory, Redis, and Postgres backends
- Semantic cache with memory and Postgres backends
- Local embedder path, OpenAI-compatible embeddings path, and optional `pgvector` search for Postgres
- Static pricebook and request cost estimation
- Budgets across global, provider, tenant, and tenant-provider scopes
- Admin auth, tenant API keys, and persisted control-plane state
- React + TypeScript operator UI

## Request Flow

```text
auth -> governor -> exact cache -> router -> semantic cache -> provider -> usage normalization -> cost calculation -> telemetry
```

Useful response headers:

- `X-Runtime-Provider`
- `X-Runtime-Provider-Kind`
- `X-Runtime-Requested-Model`
- `X-Runtime-Model`
- `X-Runtime-Cache`
- `X-Runtime-Cache-Type`
- `X-Runtime-Cost-USD`
- `X-Request-Id`

## Quick Start

1. Create a local env file:

```bash
cp .env.example .env
```

2. Configure at least one provider.

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

## Providers And Routing

The provider layer is vendor-neutral. Any upstream exposing an OpenAI-compatible API can be configured without changing gateway logic.

Current support:

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

## Semantic Cache

Embedding modes:

- `local_simple`: in-process hashed embeddings for a zero-dependency path
- `openai_compatible`: calls `/v1/embeddings` on a local or remote OpenAI-compatible endpoint

Example local embeddings setup:

```bash
GATEWAY_SEMANTIC_CACHE_ENABLED=true
GATEWAY_SEMANTIC_CACHE_EMBEDDER=openai_compatible
GATEWAY_SEMANTIC_CACHE_EMBEDDER_PROVIDER=ollama
GATEWAY_SEMANTIC_CACHE_EMBEDDER_MODEL=nomic-embed-text
```

You can also override the embedder endpoint directly with `GATEWAY_SEMANTIC_CACHE_EMBEDDER_BASE_URL` and `GATEWAY_SEMANTIC_CACHE_EMBEDDER_API_KEY`.

For Postgres-backed semantic cache, Hecate can optionally use `pgvector` for database-side cosine similarity:

```bash
GATEWAY_SEMANTIC_CACHE_BACKEND=postgres
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_MODE=auto
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_CANDIDATES=200
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_MODE=auto
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_TYPE=hnsw
```

`GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_MODE` supports:

- `auto`: try `pgvector`, fall back to JSON-stored embeddings if the extension is unavailable
- `required`: fail startup unless `pgvector` can be enabled
- `off`: always use the JSON-stored fallback path

ANN tuning knobs:

- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_MODE=auto|required|off`
- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_TYPE=hnsw|ivfflat`
- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_HNSW_M`
- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_HNSW_EF_CONSTRUCTION`
- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_IVFFLAT_LISTS`
- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_SEARCH_EF`
- `GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_SEARCH_PROBES`

## Auth And Control Plane

Auth:

- admin bearer token via `GATEWAY_AUTH_TOKEN`
- env-defined API keys via `GATEWAY_API_KEYS_JSON`

Tenant API keys can:

- access `/v1/chat/completions`
- access `/v1/models`
- be bound to a tenant
- restrict providers and models

Control plane:

- persisted tenants and API keys
- admin mutation APIs
- lightweight audit history
- `file`, `redis`, and `postgres` backends

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

## Budgets

Budget enforcement currently supports:

- `global`
- `provider`
- `tenant`
- `tenant_provider`

Backends:

- `memory`
- `redis`
- `postgres`

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

Most important settings:

```bash
GATEWAY_ADDRESS=:8080
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini
GATEWAY_ROUTER_STRATEGY=explicit_or_default
GATEWAY_ROUTER_FALLBACK_PROVIDER=

GATEWAY_AUTH_TOKEN=
GATEWAY_API_KEYS_JSON=

GATEWAY_CACHE_BACKEND=memory
GATEWAY_SEMANTIC_CACHE_ENABLED=false
GATEWAY_SEMANTIC_CACHE_BACKEND=memory
GATEWAY_SEMANTIC_CACHE_EMBEDDER=local_simple
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_MODE=auto
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_MODE=auto
GATEWAY_SEMANTIC_CACHE_POSTGRES_VECTOR_INDEX_TYPE=hnsw

GATEWAY_BUDGET_BACKEND=memory
GATEWAY_MAX_BUDGET_MICROS_USD=5000000

GATEWAY_CONTROL_PLANE_BACKEND=none

POSTGRES_DSN=
POSTGRES_SCHEMA=public
POSTGRES_TABLE_PREFIX=hecate
```

For the full list, use `.env.example` as the source of truth.

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
internal/profiler     Lightweight tracing
internal/providers    OpenAI-compatible provider implementations
internal/router       Routing logic
internal/storage      Storage helpers such as Redis and Postgres primitives
pkg/types             Vendor-neutral runtime types
ui                    Operator console
```

## Checklist

Implemented:

- [x] OpenAI-compatible `POST /v1/chat/completions`
- [x] Unified `GET /v1/models` across configured providers
- [x] Vendor-neutral OpenAI-compatible provider layer
- [x] Cloud and local provider support
- [x] Rule-based routing with explicit and local-first strategies
- [x] Exact cache with memory, Redis, and Postgres backends
- [x] Semantic cache with memory and Postgres backends
- [x] OpenAI-compatible embedding backends for semantic cache
- [x] Optional `pgvector` similarity search for Postgres semantic cache
- [x] ANN index creation and query tuning for Postgres `pgvector` semantic cache
- [x] Static pricebook and cost estimation
- [x] Shared budgets with memory, Redis, and Postgres storage backends
- [x] Budget admin mutation endpoints
- [x] Structured logs, request IDs, and lightweight in-process tracing
- [x] Admin auth, tenant API keys, and tenant-aware restrictions
- [x] File-, Redis-, and Postgres-backed control plane
- [x] Control-plane lifecycle operations and audit history
- [x] React operator UI for playground and admin operations

Next:

- [ ] Expand routing beyond simple rules to include richer policy inputs
- [ ] Add runtime metrics and explain/debug visibility for semantic-cache retrieval strategy
- [ ] Add persistent tracing and telemetry export, starting with OpenTelemetry
- [ ] Add more provider presets and discovery paths on top of the existing generic provider layer
- [ ] Add background pruning/retention workers for persistent caches
- [ ] Add budget history, threshold warnings, and better operator UX
- [ ] Start the sandbox runtime path in `cmd/sandboxd` and `internal/sandbox`
- [ ] Add deployment examples for local dev and production-style environments
