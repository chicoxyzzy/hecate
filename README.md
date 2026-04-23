# Hecate

Hecate is an open-source AI agent runtime and LLM gateway for teams running agents across cloud and local models.

It exposes an OpenAI-compatible gateway API while supporting both OpenAI-compatible upstreams and Anthropic's native Messages API behind a vendor-neutral runtime layer.

The goal is not to build another thin proxy. Hecate is meant to become a runtime control plane for AI-agent workloads: one place to understand which models were used, what each request cost, why routing decisions happened, and how agent execution can eventually be made safer.

Today, Hecate is production-shaped at the model gateway layer. It supports OpenAI-compatible upstreams, Anthropic's native Messages API, local runtimes, provider routing, health-aware failover, exact and semantic cache paths, tenant-aware auth, persisted control-plane state, tracing, OTLP export, and an operator UI. It is not yet a full agent runtime with sandboxed tool execution; that remains a future track.

Current runtime capabilities:

- OpenAI-compatible and Anthropic provider paths
- configurable base URLs for OpenAI-compatible upstreams
- cloud and local provider support
- persisted provider configs with encrypted control-plane secret storage
- live provider catalog discovery from upstream model endpoints
- deterministic routing across configured healthy providers
- provider health tracking with cooldown-based recovery states
- retry and failover for transient upstream errors
- exact cache
- semantic cache
- static and persisted pricebook-backed cost estimation
- budget enforcement
- persisted policy rules with deny/rewrite decisions by tenant, provider, model, and cost
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
  -> router
  -> route preflight
  -> exact cache
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

`GATEWAY_PROVIDERS` is optional. Hecate can infer enabled providers from core
bootstrap envs such as `PROVIDER_<NAME>_API_KEY` or `PROVIDER_<NAME>_BASE_URL`.
Set `GATEWAY_PROVIDERS` when you want to enable built-in presets using their
default settings.

Example with one cloud provider and one local provider:

```bash
GATEWAY_PROVIDERS=openai,ollama
GATEWAY_DEFAULT_MODEL=gpt-5.4-mini

PROVIDER_OPENAI_API_KEY=your_api_key_here
```

If you want cloud-only startup, a smaller config is enough:

```bash
GATEWAY_DEFAULT_MODEL=gpt-5.4-mini

PROVIDER_OPENAI_API_KEY=your_api_key_here
```

By default, Hecate considers all available providers. Explicit provider requests
still pin the route; otherwise healthy providers are considered in alphabetical
order.

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

The provider layer is vendor-neutral at the runtime boundary. Hecate supports OpenAI-compatible upstreams and Anthropic's native Messages API as first-class provider paths.

Bootstrap env configuration uses optional `GATEWAY_PROVIDERS` together with
`PROVIDER_<NAME>_*` overrides such as `PROVIDER_OPENAI_API_KEY` or
`PROVIDER_OLLAMA_BASE_URL`. When `GATEWAY_PROVIDERS` is omitted, Hecate derives
enabled providers from the core provider envs it finds.

The documented core provider knobs are:

- `PROVIDER_<NAME>_API_KEY`
- `PROVIDER_<NAME>_BASE_URL`
- `PROVIDER_<NAME>_DEFAULT_MODEL`

Advanced overrides like `PROTOCOL`, `API_VERSION`, and `TIMEOUT` are available
when needed.


Built-in cloud provider presets:

- `openai` - OpenAI-compatible provider path
- `anthropic` - Anthropic native Messages API provider path
- `groq` - OpenAI-compatible provider path
- `gemini` - OpenAI-compatible provider path

Built-in local provider presets:

- `ollama` - Ollama OpenAI-compatible endpoint
- `lmstudio` - LM Studio OpenAI-compatible server
- `localai` - LocalAI OpenAI-compatible API
- `llamacpp` - llama.cpp-style OpenAI-compatible servers

Local presets have default OpenAI-compatible base URLs:

- `ollama`: `http://127.0.0.1:11434/v1`
- `lmstudio`: `http://127.0.0.1:1234/v1`
- `localai`: `http://127.0.0.1:8080/v1`
- `llamacpp`: `http://127.0.0.1:8080/v1`

LocalAI and llama.cpp share the same default URL because both commonly run an
OpenAI-compatible server on port `8080`. Hecate cannot reliably infer which
runtime is behind a generic OpenAI-compatible URL, so the provider identity is
the configured provider name. Enable only the matching preset, or override
`PROVIDER_<NAME>_BASE_URL` so each configured provider points to a unique
endpoint.

## Auth And Control Plane

Auth supports:

- admin bearer token
- persisted API keys managed through the control plane

The control plane currently supports:

- tenant management
- API key management
- persisted provider management
- built-in provider default hydration by provider name
- encrypted provider credential storage
- enable/disable and rotation flows
- audit history
- file, Redis, and Postgres backends

## Observability

Implemented observability features:

- request IDs
- trace IDs and span IDs in response headers
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
cmd/sandboxd          Sandbox daemon placeholder
internal/api          HTTP handlers and middleware
internal/auth         Auth and principal resolution
internal/billing      Static pricebook and cost estimation
internal/cache        Exact and semantic cache backends
internal/catalog      Provider/model catalog views
internal/chatstate    Persisted chat session state
internal/config       Environment-based configuration
internal/controlplane Tenant, API-key, and audit-history persistence
internal/gateway      Core runtime pipeline
internal/governor     Policy and budget enforcement
internal/models       Canonical model identity helpers
internal/policy       Policy matching helpers
internal/profiler     Tracing and trace snapshots
internal/providers    Provider transports, discovery, and health tracking
internal/requestscope Tenant/provider request scoping
internal/retention    Background pruning and retention runs
internal/router       Routing logic
internal/sandbox      Sandbox runtime placeholder
internal/secrets      Secret encryption helpers
internal/storage      Redis and Postgres helpers
internal/telemetry    Metrics and OTLP export wiring
pkg/types             Vendor-neutral runtime types
ui                    Operator console
```

## Checklist

Implemented:

- [x] OpenAI-compatible chat completions endpoint
- [x] Anthropic native Messages API provider path
- [x] Unified model catalog across configured providers
- [x] Cloud and local provider support behind a vendor-neutral provider layer
- [x] Deterministic routing across configured healthy providers
- [x] Retry, failover, and provider health tracking
- [x] Exact cache
- [x] Semantic cache
- [x] Static and persisted pricebook-backed cost estimation
- [x] Budget enforcement with top-ups, resets, warning thresholds, and history
- [x] Background retention and pruning for traces, cache, budget history, and audit events
- [x] Tenant-aware auth and persisted control-plane state
- [x] Persisted provider config with encrypted secret storage and runtime reload
- [x] Persisted policy and pricebook control-plane CRUD
- [x] Structured logs, traces, metrics, and OTLP export support
- [x] React operator UI
- [x] Provider setup preset catalog for common cloud and local runtimes

Next:

- [ ] Richer circuit-breaker behavior beyond cooldown-based health recovery
- [ ] Cleaner route reason taxonomy and debug views after routing simplification
- [ ] Richer policy lifecycle UI, history, and validation helpers
- [ ] Automated pricebook ingestion/sync from provider pricing sources
- [ ] Better semantic-cache debugging and trace visibility in the UI
- [ ] Better budget UX and trend visibility in the UI
- [ ] Provider setup UX that keeps presets separate from runtime routing truth
- [ ] More provider discovery paths
- [ ] Sandbox runtime work in `cmd/sandboxd` and `internal/sandbox`
- [ ] Deployment examples for local and production-style environments
