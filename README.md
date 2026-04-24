# Hecate

Hecate is an open-source AI runtime for teams that want one control plane in front of cloud and local models.

Today it is strongest as an LLM gateway and operator console: it exposes an OpenAI-compatible API, supports OpenAI-compatible upstreams plus Anthropic's native Messages API, applies routing and policy decisions, tracks cost and traces, and gives operators a UI for debugging and admin workflows.

Hecate also now includes the first coding-runtime slice: task, run, step, artifact, and approval APIs; bounded shell, file, and git execution; a worker-backed sandbox; per-run workspaces; cancellation; and live stdout/stderr streaming. It is not yet a full daily-driver coding agent runtime, but the runtime boundary is now in place.

## Table Of Contents

- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Providers](#providers)
- [Auth And Control Plane](#auth-and-control-plane)
- [Observability](#observability)
- [UI](#ui)
- [Using Hecate For Coding](#using-hecate-for-coding)
- [Docs](#docs)
- [Commands](#commands)
- [Repository Layout](#repository-layout)
- [Checklist](#checklist)

What Hecate already does well:

- OpenAI-compatible gateway API with Anthropic and OpenAI-compatible provider support
- Anthropic extended thinking pass-through (thinking/redacted_thinking blocks, streaming)
- cloud and local model routing with retries, failover, health tracking, and discovery
- exact and semantic cache paths
- tenant-aware auth, policy enforcement, budgets, and control-plane persistence
- per-API-key rate limiting with token-bucket enforcement and standard rate-limit headers
- structured logs, trace inspection, and OTLP export for traces, metrics, and logs
- opt-in request/response body capture in traces (`GATEWAY_TRACE_BODIES=true`)
- operator UI for models, providers, playground, runs, access, budgets, and control plane
- basic coding-runtime execution with approvals, sandboxing, workspaces, and live run streaming

Storage backends used across the system include:

- file
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

task client
  -> task api
  -> orchestrator
  -> sandboxd
  -> artifacts and run stream
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

Advanced overrides like `PROTOCOL`, `API_VERSION`, and `TIMEOUT` are available when needed.

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

The control plane supports:

- tenant management
- API key management
- persisted provider management
- built-in provider default hydration by provider name
- encrypted provider credential storage
- enable/disable and rotation flows
- audit history
- file, Redis, and Postgres backends

## Observability

Observability currently includes:

- request IDs
- trace IDs and span IDs in response headers
- structured logs
- in-memory trace snapshots over HTTP
- OTLP HTTP export for traces
- OTLP HTTP export for metrics
- OTLP HTTP export for logs
- opt-in request/response body capture in traces (set `GATEWAY_TRACE_BODIES=true`; max size tunable via `GATEWAY_TRACE_BODY_MAX_BYTES`, default 4096)

For a practical telemetry guide, see [`docs/telemetry.md`](docs/telemetry.md).

## UI

The operator UI includes:

- provider and model visibility
- preset-driven provider setup
- managed provider enable/disable/delete and secret rotation
- playground
- live task and run monitoring with approvals, cancellation, and streamed stdout/stderr
- task creation and task start controls in the runs workspace
- runtime metadata inspection
- trace inspection
- budget admin flows
- tenant and API key management
- control-plane activity view

The app shell lives in `ui/src/app`, shared console primitives and workbench building blocks live in `ui/src/features/shared`, and feature-owned styles now sit beside the feature views that use them.

## Using Hecate For Coding

Hecate is already useful behind a coding assistant even if the client still owns planning and tool orchestration. Today you can use it for:

- routing and failover across cloud and local models
- tenant-aware auth, policies, and budgets
- request tracing, route debugging, and structured logs
- central cost accounting and provider visibility
- exact and semantic cache paths

The first native coding-runtime slice is also in place:

- task, run, step, artifact, and approval APIs
- shell, file, and git executors
- an out-of-process `cmd/sandboxd` worker
- per-run workspace provisioning
- basic sandbox policy controls for roots, read-only mode, timeouts, and network denial
- approval gating for shell execution
- run queueing, cancellation, and SSE streaming
- UI support for task creation, live runs, approvals, cancellation, and streamed stdout/stderr

The main missing pieces are resumable execution, broader approval classes, stronger workspace isolation, richer tool APIs, and more coding-oriented operator views.

## Docs

- [Telemetry And OTLP Notes](docs/telemetry.md)

## Commands

```bash
make dev
make test
make ui-install
make ui-dev
make ui-build
```

`.env.example` is the baseline configuration reference. For the full set of supported environment variables, check `internal/config/config.go`.

## Repository Layout

```text
cmd/gateway           Main HTTP server
cmd/sandboxd          Out-of-process sandbox worker
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
internal/sandbox      Local and worker-backed sandbox execution
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
- [x] Useful as the gateway and control plane behind coding assistants that execute tools themselves
- [x] Deterministic routing across configured healthy providers
- [x] Retry, failover, and provider health tracking
- [x] Exact cache
- [x] Semantic cache
- [x] Static and persisted pricebook-backed cost estimation
- [x] Budget enforcement with top-ups, resets, warning thresholds, history, and 402 on exhaustion
- [x] Per-API-key rate limiting with configurable burst and RPM (`GATEWAY_RATE_LIMIT_*`)
- [x] Anthropic extended thinking pass-through (thinking/redacted_thinking, streaming delta events)
- [x] Background retention and pruning for traces, cache, budget history, and audit events
- [x] Tenant-aware auth and persisted control-plane state
- [x] Persisted provider config with encrypted secret storage and runtime reload
- [x] Persisted policy and pricebook control-plane CRUD
- [x] Structured logs, traces, metrics, and OTLP export support
- [x] React operator UI
- [x] Provider setup preset catalog for common cloud and local runtimes

Near-term foundation work:

- [ ] Richer circuit-breaker behavior beyond cooldown-based health recovery
- [ ] Cleaner route reason taxonomy and debug views after routing simplification
- [ ] Richer policy lifecycle UI, history, and validation helpers
- [ ] Automated pricebook ingestion/sync from provider pricing sources
- [ ] Better semantic-cache debugging and trace visibility in the UI
- [ ] Better budget UX and trend visibility in the UI
- [ ] Provider setup UX that keeps presets separate from runtime routing truth
- [ ] More provider discovery paths
- [ ] Deployment examples for local and production-style environments

Coding runtime work:

- [x] Basic task, run, step, artifact, and approval lifecycle for coding runs
- [x] Basic shell, file, and git execution paths
- [x] Out-of-process sandbox runtime work in `cmd/sandboxd`
- [x] Workspace isolation and per-run workspace provisioning
- [x] Basic sandbox policy enforcement for allowed roots, read-only mode, timeouts, and network denial
- [x] Shell approval gating with approve or reject flow
- [x] Queueing and cancellation for coding runs
- [x] Streaming run updates and stdout/stderr artifact logs
- [x] Basic operator UI for creating tasks, starting runs, approvals, cancellation, and live stdout/stderr
- [ ] Resumable execution for coding runs
- [ ] Policy-driven approvals for broader sensitive actions
- [ ] Richer coding-oriented operator views for task traces, repo activity, and aggregate run operations
