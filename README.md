# Hecate

Hecate is a Go-based LLM gateway for AI-agent workloads.

Today, Hecate sits at the model-call layer: it exposes an OpenAI-compatible API, routes requests across cloud and local OpenAI-compatible providers, applies budget and policy checks, supports exact and semantic cache paths, records traces and structured logs, and provides a small operator UI.

Hecate can already be used by agent systems as the gateway in front of their model traffic. It is not yet a full agent runtime with built-in sandboxed tool execution. That is a future direction, not current functionality.

Current runtime capabilities:

- OpenAI-compatible provider layer with configurable base URLs
- cloud and local provider support
- rule-based routing
- provider health tracking with cooldown-based recovery states
- retry and failover for transient upstream errors
- exact cache
- semantic cache
- static cost estimation via a local pricebook
- budget enforcement
- budget limit top-ups, resets, warning thresholds, and history
- tenant-aware auth and restrictions
- request tracing and structured logs
- optional OTLP HTTP export for traces, metrics, and logs
- React operator UI

Storage backends currently used in different subsystems:

- memory
- Redis
- Postgres

## Architecture

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

## Quick Start

1. Create a local env file:

```bash
cp .env.example .env
```

2. Configure at least one provider in `.env`.

Example with one cloud provider and one local provider:

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

## Providers

The provider layer is vendor-neutral at the gateway boundary. Any upstream exposing an OpenAI-compatible API can be integrated through configuration.

This includes local runtimes such as:

- Ollama
- LM Studio
- LocalAI
- llama.cpp-style servers

## Auth And Control Plane

Auth supports:

- admin bearer token
- env-defined API keys
- persisted API keys managed through the control plane

The control plane currently supports:

- tenant management
- API key management
- enable/disable and rotation flows
- audit history
- file, Redis, and Postgres backends

## Observability

Implemented observability features:

- request IDs
- trace IDs and span IDs in responses
- structured logs
- in-memory trace snapshots over HTTP
- OTLP HTTP export for traces
- OTLP HTTP export for metrics
- OTLP HTTP export for logs

## UI

The operator UI currently includes:

- provider and model visibility
- playground
- runtime metadata inspection
- trace inspection
- budget admin flows
- tenant and API key management
- control-plane activity view

## Commands

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
- [x] Rule-based routing with retry, failover, and provider health tracking
- [x] Exact cache
- [x] Semantic cache
- [x] Static pricebook and cost estimation
- [x] Budget enforcement with top-ups, resets, warning thresholds, and history
- [x] Tenant-aware auth and persisted control-plane state
- [x] Structured logs, traces, metrics, and OTLP export support
- [x] React operator UI

Next:

- [ ] Richer circuit-breaker behavior beyond cooldown-based health recovery
- [ ] More advanced routing and policy decisions
- [ ] A real pricebook ingestion/update path instead of only seeded static defaults
- [ ] Better semantic-cache debugging and trace visibility in the UI
- [ ] Better budget UX and trend visibility in the UI
- [ ] Background retention and pruning workers
- [ ] More provider presets and discovery paths
- [ ] Sandbox runtime work in `cmd/sandboxd` and `internal/sandbox`
- [ ] Deployment examples for local and production-style environments
