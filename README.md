# Hecate

Hecate is a Go-based LLM gateway and control plane for AI-agent workloads.

It sits between agents and model providers to handle routing, caching, policy enforcement, cost tracking, and observability across cloud and local runtimes. At the gateway boundary, Hecate exposes an OpenAI-compatible API; underneath, it currently supports OpenAI-compatible upstreams and Anthropic's native Messages API.

Today, Hecate is production-shaped at the model gateway layer: provider routing, health-aware failover, exact and semantic cache paths, tenant-aware auth, persisted control-plane state, tracing, OTLP export, and a small operator UI are all implemented. It is not yet a full agent runtime with sandboxed tool execution; that remains a future track.

Current runtime capabilities:

- OpenAI-compatible provider layer with configurable base URLs
- cloud and local provider support
- persisted provider configs with encrypted control-plane secret storage
- live provider catalog discovery from upstream model endpoints
- rule-based routing
- provider health tracking with cooldown-based recovery states
- retry and failover for transient upstream errors
- exact cache
- semantic cache
- static cost estimation via a local pricebook
- budget enforcement
- config-driven policy rules with deny/rewrite decisions by tenant, provider, model, and cost
- budget limit top-ups, resets, warning thresholds, and history
- tenant-aware auth and restrictions
- request tracing and structured logs
- background retention and pruning with manual admin trigger
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

`.env.example` already includes all built-in provider preset names in `GATEWAY_PROVIDERS`.
For a minimal first run, it is usually easiest to trim that list down to only the providers
you actually want to use right now.

Example with one cloud provider and one local provider:

```bash
GATEWAY_PROVIDERS=openai,ollama
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini

PROVIDER_OPENAI_API_KEY=your_api_key_here
PROVIDER_OLLAMA_BASE_URL=http://127.0.0.1:11434/v1
```

If you want cloud-only startup, a smaller config is enough:

```bash
GATEWAY_PROVIDERS=openai
GATEWAY_DEFAULT_PROVIDER=openai
GATEWAY_DEFAULT_MODEL=gpt-4o-mini

PROVIDER_OPENAI_API_KEY=your_api_key_here
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

Configured provider records tell Hecate how to connect. The model catalog itself is discovered from the provider when possible, rather than treated as hardcoded application state.

Bootstrap env configuration uses `GATEWAY_PROVIDERS=openai,anthropic,groq,gemini,ollama,lmstudio,localai,llamacpp` together with optional `PROVIDER_<NAME>_*` overrides such as `PROVIDER_OPENAI_API_KEY` or `PROVIDER_OLLAMA_BASE_URL`.

Hecate currently has two real provider implementations under the hood:

- OpenAI-compatible upstreams
- Anthropic native Messages API upstreams

The built-in provider names are presets on top of those transports.

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
- persisted provider management
- encrypted provider credential storage
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
- preset-driven provider setup
- managed provider enable/disable/delete and secret rotation
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
- [x] Background retention and pruning for traces, cache, budget history, and audit events
- [x] Tenant-aware auth and persisted control-plane state
- [x] Persisted provider config with encrypted secret storage and runtime reload
- [x] Structured logs, traces, metrics, and OTLP export support
- [x] React operator UI
- [x] Provider preset catalog for common cloud and local runtimes

Next:

- [ ] Richer circuit-breaker behavior beyond cooldown-based health recovery
- [ ] Deeper policy lifecycle beyond config-driven rules
- [ ] A real pricebook ingestion/update path instead of only seeded static defaults
- [ ] Better semantic-cache debugging and trace visibility in the UI
- [ ] Better budget UX and trend visibility in the UI
- [ ] More provider discovery paths and richer preset coverage
- [ ] Sandbox runtime work in `cmd/sandboxd` and `internal/sandbox`
- [ ] Deployment examples for local and production-style environments
