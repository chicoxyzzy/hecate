# Hecate

Hecate is an open-source AI gateway and agent runtime for teams that want one control plane across cloud and local models, with operator-grade policy, spend, and observability.

## Table Of Contents

- [What Hecate Is Today](#what-hecate-is-today)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Provider Model](#provider-model)
- [Auth, Policy, And Spend](#auth-policy-and-spend)
- [Observability](#observability)
- [Operator UI](#operator-ui)
- [Using Hecate For Coding](#using-hecate-for-coding)
- [Using Hecate With Codex And Claude Code](#using-hecate-with-codex-and-claude-code)
- [Durable Queue Execution Flow](#durable-queue-execution-flow)
- [Config Highlights](#config-highlights)
- [Docs](#docs)
- [Commands](#commands)
- [Status And Roadmap](#status-and-roadmap)

## What Hecate Is Today

Hecate currently has two strong layers:

- a mature gateway/control-plane path for multi-provider model traffic
- a production-leaning coding runtime foundation for task/run execution

What already works well:

- OpenAI-compatible chat completions and Anthropic-native `/v1/messages`
- OpenAI tool-call and Anthropic tool-use compatibility behind one runtime boundary
- cloud and local provider routing with retries, failover, and health tracking
- exact and semantic cache paths
- tenant-aware auth, policies, budgets, and persisted control-plane state
- budget exhaustion (`402`) and per-API-key token-bucket rate limiting
- structured logs, trace inspection, OTLP export, and optional trace body capture
- task/run orchestration with approvals, sandboxed execution, persisted run events, and stream resume cursors
- durable leased queue backend for distributed workers via Postgres

Storage backends used across the system include `file`, `memory`, `Redis`, and `Postgres`.

## Architecture

Gateway client flow:

```mermaid
flowchart TD
    GatewayClient["Gateway client"] --> Auth["Auth"];
    Auth --> Governor["Governor and policy"];
    Governor --> Router["Router"];
    Router --> Preflight["Route preflight"];
    Preflight --> ExactCache["Exact cache"];
    Preflight --> SemanticCache["Semantic cache"];
    ExactCache --> Provider["Provider call"];
    SemanticCache --> Provider;
    Provider --> Usage["Usage normalization"];
    Usage --> Cost["Cost calculation"];
    Cost --> Telemetry["Telemetry headers and logs"];
    Telemetry --> ClientResponse["Client response"];
```

Task client flow:

```mermaid
flowchart TD
    TaskClient["Task client"] --> TaskApi["Task API"];
    TaskApi --> Orchestrator["Orchestrator runner"];
    Orchestrator --> LeaseQueue["Lease queue (memory or postgres)"];
    LeaseQueue --> WorkerA["Worker A claim"];
    LeaseQueue --> WorkerB["Worker B claim"];
    WorkerA --> Sandboxd["sandboxd"];
    WorkerB --> Sandboxd;
    Sandboxd --> TaskState["Task state"];
    Sandboxd --> Events["Run events"];
    TaskState --> Stream["SSE stream and replay"];
    Events --> Stream;
```

## Quick Start

1. Copy env defaults:

```bash
cp .env.example .env
```

2. Configure at least one provider in `.env`.

`GATEWAY_PROVIDERS` is optional. Hecate can infer enabled providers from core
provider envs like `PROVIDER_<NAME>_API_KEY` and `PROVIDER_<NAME>_BASE_URL`.

Example cloud + local:

```bash
GATEWAY_PROVIDERS=openai,ollama
GATEWAY_DEFAULT_MODEL=gpt-5.4-mini
PROVIDER_OPENAI_API_KEY=your_api_key_here
```

Example cloud-only:

```bash
GATEWAY_DEFAULT_MODEL=gpt-5.4-mini
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

## Provider Model

Hecate uses a vendor-neutral provider layer at the runtime boundary. It treats
OpenAI-compatible upstreams and Anthropic Messages API as first-class paths.

Core provider env knobs:

- `PROVIDER_<NAME>_API_KEY`
- `PROVIDER_<NAME>_BASE_URL`
- `PROVIDER_<NAME>_DEFAULT_MODEL`

Advanced overrides such as protocol, API version, and timeout are also
available when needed.

Built-in cloud presets:

- `openai`
- `anthropic`
- `groq`
- `gemini`

Built-in local presets:

- `ollama`
- `lmstudio`
- `localai`
- `llamacpp`

Default local base URLs:

- `ollama`: `http://127.0.0.1:11434/v1`
- `lmstudio`: `http://127.0.0.1:1234/v1`
- `localai`: `http://127.0.0.1:8080/v1`
- `llamacpp`: `http://127.0.0.1:8080/v1`

## Auth, Policy, And Spend

Auth supports:

- admin bearer token
- persisted API keys from the control plane
- optional single-user admin mode (`GATEWAY_SINGLE_USER_ADMIN_MODE=true`) that treats requests as admin for tokenless local development

Control plane supports:

- tenant and API key management
- persisted provider management with encrypted secrets
- provider enable/disable and rotation flows
- policy and pricebook CRUD
- audit history

Spend/governor supports:

- budget accounting and enforcement
- warning thresholds, top-ups, resets, and history
- request denial as `402` on budget exhaustion
- per-key rate limiting with `X-RateLimit-*` headers

## Observability

Observability includes:

- request IDs, trace IDs, and span IDs in response headers
- structured logs
- local trace inspection over HTTP
- OTLP HTTP export for traces, metrics, and logs
- optional request/response trace body capture (`GATEWAY_TRACE_BODIES=true`)
- runtime telemetry health and SLO snapshots via `/admin/runtime/stats`

For full telemetry details, see [`docs/telemetry.md`](docs/telemetry.md).

## Operator UI

The operator UI includes:

- provider/model visibility and setup presets
- managed provider lifecycle flows (enable/disable/delete/rotate)
- playground and runtime metadata inspection
- task creation, run starts, approvals, cancellation, and live stdout/stderr
- telemetry health panel with signal status and run SLO cards
- trace inspection
- budget admin flows
- tenant/API key management and control-plane activity views

The app shell lives in `ui/src/app`, shared console primitives live in
`ui/src/features/shared`, and feature-owned styles live beside feature views.

## Using Hecate For Coding

Hecate is already useful behind coding assistants even when orchestration logic
still lives in the client.

Current coding-runtime foundation:

- task/run/step/artifact/approval APIs
- shell, file, and git executors
- out-of-process `cmd/sandboxd`
- per-run workspace provisioning
- sandbox policy controls (roots, read-only mode, timeout, network denial)
- policy-driven approvals (`shell_exec`, `git_exec`, `file_write`, `network_egress`)
- queueing, cancellation, retry/resume APIs
- persisted run events and SSE stream resume (`after_sequence`, `Last-Event-ID`)
- durable distributed queue semantics via Postgres lease claims

## Using Hecate With Codex And Claude Code

Hecate supports both OpenAI-compatible clients and Anthropic Messages clients, so you can point Codex and Claude Code at one gateway.

Use:

- OpenAI-compatible path: `POST /v1/chat/completions`
- Anthropic path: `POST /v1/messages`
- Discovery: `GET /v1/models`

For copy-paste setup and auth/header examples, see [`docs/client-integration.md`](docs/client-integration.md).

## Durable Queue Execution Flow

```mermaid
flowchart TD
    Client["Task client or UI"] --> TasksApi["/v1/tasks"];
    TasksApi --> Runner["Orchestrator runner"];
    Runner --> Queue["Run queue"];
    Queue -->|"Claim lease"| WorkerA["Worker A"];
    Queue -->|"Claim lease"| WorkerB["Worker B"];
    WorkerA -->|"Heartbeat and extend lease"| Queue;
    WorkerB -->|"Ack or nack"| Queue;
    WorkerA --> Sandboxd["sandboxd"];
    WorkerB --> Sandboxd;
    Sandboxd --> State["Task state store"];
    Sandboxd --> RunEvents["Run events"];
    Queue --> Stats["Runtime stats"];
    State --> Stats;
    RunEvents --> Stats;
    State --> Snapshot["Run snapshot"];
    RunEvents --> Snapshot;
    Snapshot --> Stream["SSE stream with replay cursor"];
```

## Config Highlights

Runtime and queue knobs commonly adjusted for coding workflows:

- `GATEWAY_TASKS_BACKEND=memory|postgres`
- `GATEWAY_TASK_QUEUE_BACKEND=memory|postgres`
- `GATEWAY_TASK_QUEUE_WORKERS=<int>`
- `GATEWAY_TASK_QUEUE_BUFFER=<int>`
- `GATEWAY_TASK_QUEUE_LEASE_SECONDS=<int>`
- `GATEWAY_TASK_APPROVAL_POLICIES=shell_exec,git_exec,file_write,network_egress`
- `GATEWAY_TASK_MAX_CONCURRENT_PER_TENANT=<int>`
- `GATEWAY_SINGLE_USER_ADMIN_MODE=true|false`

Use `.env.example` as the baseline. For the full env surface, see
`internal/config/config.go`.

## Docs

- [Client Integration (Codex And Claude Code)](docs/client-integration.md)
- [Runtime API Notes](docs/runtime-api.md)
- [Telemetry And OTLP Notes](docs/telemetry.md)
- [OTLP Collector Recipes And Runbooks](docs/telemetry.md#known-good-otlp-recipes)

## Commands

```bash
make dev
make test
make ui-install
make ui-dev
make ui-build
```

## Status And Roadmap

Delivered:

- gateway runtime with OpenAI + Anthropic API compatibility
- control plane with persisted policy, pricebook, and provider management
- spend governance and key-level rate limiting
- operator UI for day-to-day runtime operations
- coding runtime foundation with sandboxd, approvals, run events, stream resume
- durable leased queue backend for distributed workers
- continuation-style run resume with workspace reuse
- checkpoint context propagation for resumed runs across executor boundaries

Next focus areas:

- richer checkpoint controls for partial replay and selective step continuation
- broader policy-driven approval classes
- richer coding-focused UI views and aggregate run operations
- improved route-reason taxonomy and debug ergonomics
- automated provider pricebook ingestion and sync
- deployment examples for local and production environments

## License

MIT. See [`LICENSE`](LICENSE).
