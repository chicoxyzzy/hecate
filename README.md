# Hecate

Hecate is an open-source AI agent runtime and LLM gateway for teams running agents across cloud and local models, including coding assistants.

It exposes an OpenAI-compatible gateway API while supporting both OpenAI-compatible upstreams and Anthropic's native Messages API behind a vendor-neutral runtime layer.

The goal is not to build another thin proxy. Hecate is meant to become a runtime control plane for AI-agent workloads: one place to understand which models were used, what each request cost, why routing decisions happened, and how agent execution can eventually be made safer.

Today, Hecate is production-shaped at the model gateway layer. It supports OpenAI-compatible upstreams, Anthropic's native Messages API, local runtimes, provider routing, health-aware failover, exact and semantic cache paths, tenant-aware auth, persisted control-plane state, tracing, OTLP export, and an operator UI. That already makes it useful as the gateway and control plane behind coding assistants that know how to execute tools themselves. It is not yet a full coding-agent runtime with sandboxed tool execution and workspace orchestration; that remains the next major track.

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
- live task and run monitoring with approvals, cancellation, and streamed stdout/stderr
- task creation and task start controls in the runs workspace
- runtime metadata inspection
- trace inspection
- budget admin flows
- tenant and API key management
- control-plane activity view

## Using Hecate For Coding

Hecate can already sit behind a coding assistant as the model gateway and runtime control plane. In that role it is useful today for:

- provider routing across cloud and local models
- model and provider policy enforcement
- tenant-aware API keys and access restrictions
- request tracing, route debugging, and structured logs
- exact and semantic cache paths
- static and persisted pricebook-backed cost estimation
- budget enforcement and admin controls

That means a coding client can point its LLM traffic at Hecate today and get central routing, observability, and spend controls.

The first coding-runtime slice is also now in place:

- task, run, step, artifact, and approval objects with HTTP read/write APIs
- bounded shell, file, and git executor paths
- an out-of-process `cmd/sandboxd` worker for sandbox execution
- per-run workspace provisioning before execution begins
- sandbox policy enforcement for allowed roots, read-only mode, timeouts, and basic network denial
- shell approval gating with explicit approve or reject flows before execution
- queued run execution with explicit run cancellation
- live stdout/stderr artifact streaming plus SSE run updates via `/v1/tasks/{id}/runs/{run_id}/stream`

What is still missing is the part that makes Hecate itself a stronger daily-driver coding runtime. The main gaps are:

- stronger workspace isolation beyond the current per-run sandbox worker and workspace model
- resumable execution for interrupted or restarted coding runs
- richer tool execution APIs beyond the current run and artifact streaming surface
- policy-driven approval flows for broader sensitive actions like network access or git push
- richer coding-oriented operator views for task traces, repo activity, and broader run management

The practical roadmap for coding use breaks into three phases:

### Phase 1: Coding Gateway

Make Hecate excellent as the gateway and control plane under an existing coding product:

- tighten compatibility for coding-style chat, streaming, and tool-call workloads
- improve route reason visibility and failure debugging
- add coding-oriented policies, budgets, and per-session cost views
- ship local and small-team deployment examples

### Phase 2: Minimal Coding Runtime

Add the first runtime slice that can execute bounded coding tasks:

- add resume semantics on top of the new sandbox worker boundary
- extend the task/job model with resumable state across process restarts
- expose richer tool execution APIs on top of the current streamed run snapshots and artifacts
- enforce policy-driven safety controls like approval classes, allowed paths, timeouts, and network policy

### Phase 3: Team-Ready Coding Platform

Expand the runtime into something teams can trust for daily coding work:

- approval flows for sensitive actions such as network access, destructive filesystem writes, or git push
- stronger workspace isolation and resumable task state
- richer UI for task timelines, tool runs, traces, and cost by session or repo
- smarter model selection policies for different coding task classes
- stronger reliability features around health, failover, pricing sync, and debug tooling

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
internal/sandbox      Local sandbox executor and policy enforcement
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
- [x] Budget enforcement with top-ups, resets, warning thresholds, and history
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
