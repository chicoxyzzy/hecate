# Hecate

[![Test](https://github.com/chicoxyzzy/hecate/actions/workflows/test.yml/badge.svg)](https://github.com/chicoxyzzy/hecate/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/chicoxyzzy/hecate)](https://goreportcard.com/report/github.com/chicoxyzzy/hecate)
[![Go version](https://img.shields.io/github/go-mod/go-version/chicoxyzzy/hecate)](go.mod)
[![License](https://img.shields.io/github/license/chicoxyzzy/hecate)](LICENSE)
[![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-enabled-f5a800?logo=opentelemetry&logoColor=white)](https://opentelemetry.io/)

Hecate is an open-source **AI gateway and agent-task runtime** for teams that want one control plane for model access, cost governance, routing, caching, observability, and controlled agent execution.

It sits between AI clients and model providers. Existing OpenAI-compatible and Anthropic-compatible clients can point at Hecate, while operators get a place to manage providers, tenants, budgets, traces, cache behavior, and queued agent work.

![Chats workspace talking to a local Ollama llama3.1:8b model with sessions sidebar and inline runtime metadata](docs/screenshots/chat.png)

## Table Of Contents

- [Why Hecate](#why-hecate)
- [Quick Start](#quick-start)
- [Connect a Client](#connect-a-client)
- [Architecture](#architecture)
- [Operator UI](#operator-ui)
- [What Works Today](#what-works-today)
- [Configuration](#configuration)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)

## Why Hecate

AI workloads are moving from simple API calls to long-running agents, tool use, local/cloud routing, and budget-sensitive automation. Hecate is built for that messier runtime layer.

- **One gateway for many clients** — OpenAI Chat Completions and Anthropic Messages shapes.
- **Local and cloud providers together** — OpenAI, Anthropic, Ollama, LM Studio, LocalAI, llama.cpp-compatible servers, and other shipped presets.
- **Operator-controlled spend** — tenant keys, model/provider scoping, balances, pricebook management, rate limits, and audit history.
- **Runtime visibility** — request ledger, route reports, failover details, cost, cache path, trace IDs, and OpenTelemetry export.
- **Agent-task runtime** — queued tasks, approvals, controlled shell/file/git execution, resumable runs, and MCP integration.
- **Single binary first** — Go gateway with embedded React operator UI; no premature microservice sprawl.

## Quick Start

```bash
docker compose up
```

Open `http://127.0.0.1:8080`, paste the generated admin bearer token from the container logs, and connect your first provider in the UI.

```text
============================================================
  Hecate first-run setup — admin bearer token generated.

    7f2a91b... (truncated)

  Saved to /data/hecate.bootstrap.json (mode 0600).
============================================================
```

The first-run UI guides provider setup and token entry:

![First-run onboarding wizard](docs/screenshots/onboard-wizard.png)

For local development from source:

```bash
make dev
```

Provider API keys can be added in the UI after first boot. If you prefer environment bootstrap, start with `.env.example`. The full deployment guide is in [docs/deployment.md](docs/deployment.md), and local development details are in [docs/development.md](docs/development.md).

## Connect a Client

Hecate is designed to work with existing tools, not a custom SDK.

| Client | Configure |
|---|---|
| Codex, OpenAI SDKs, OpenAI-compatible tools | `OPENAI_BASE_URL=http://127.0.0.1:8080/v1` and `OPENAI_API_KEY=<hecate key>` |
| Claude Code, Anthropic SDKs | `ANTHROPIC_BASE_URL=http://127.0.0.1:8080` and `ANTHROPIC_API_KEY=<hecate key>` |
| curl / internal tools | Use the OpenAI-compatible `/v1/chat/completions` or Anthropic-compatible `/v1/messages` APIs |

The operator UI also includes **Admin → Integrations** with copy-paste snippets for common clients.

Custom clients are supported today: Codex, Claude Code, OpenAI/Anthropic SDKs, curl scripts, and internal tools can point at Hecate. Custom providers are different: provider management currently centers on the shipped preset catalog, not arbitrary provider creation. See [docs/client-integration.md](docs/client-integration.md) and [docs/providers.md](docs/providers.md).

## Architecture

Hecate is one Go binary with two main surfaces:

- **Gateway** — authenticates requests, applies policy/budget checks, resolves provider/model routing, handles cache paths, calls upstream model providers, and records traces.
- **Task runtime** — queues agent-style work, manages approvals, runs controlled tools through the sandbox boundary, streams run events, and records artifacts.

```mermaid
flowchart LR
    Clients["Clients<br/>Codex, Claude Code, SDKs"] --> Gateway["Gateway<br/>OpenAI + Anthropic APIs"]
    Clients --> Runtime["Task runtime<br/>queued agent work"]
    Gateway --> Providers["Cloud + local providers"]
    Gateway --> Cache["Exact + semantic cache"]
    Runtime --> Sandbox["sandboxd<br/>controlled execution"]
    Runtime --> MCP["MCP servers"]
    Gateway --> OTel["OpenTelemetry"]
    Runtime --> OTel
    UI["Operator UI"] --> Gateway
    UI --> Runtime
```

For deeper internals, read [docs/architecture.md](docs/architecture.md), [docs/runtime-api.md](docs/runtime-api.md), and [docs/events.md](docs/events.md).

## Operator UI

The embedded UI is a runtime console for operators.

- **Chats** — send requests through Hecate, choose provider/model, inspect per-turn route/cost/cache metadata.
- **Providers** — manage provider credentials, defaults, model discovery, base URLs, and health.
- **Observability** — inspect requests, route candidates, skip reasons, failover, costs, cache decisions, and trace events.
- **Tasks** — create and manage agent runs, approvals, retries, resumes, and streamed output.
- **Admin** — tenants, API keys, balances, pricebook, policy rules, retention, and client snippets.

<details>
<summary>UI screenshots</summary>

![Providers tab — every preset card with health, model count, and configured-credentials state](docs/screenshots/providers.png)

![Pricebook tab — model catalog with priced / unpriced / deprecated filters](docs/screenshots/admin-pricebook.png)

![Balances tab — tenant and provider balances with usage history](docs/screenshots/admin-budget.png)

![Tenants tab — tenant lifecycle and access controls](docs/screenshots/admin-tenants.png)

![API keys tab — scoped keys for clients and agents](docs/screenshots/admin-keys.png)

![Integrations tab — copy-paste client configuration snippets](docs/screenshots/admin-integrations.png)

![Observability view — request ledger and route-report drilldown](docs/screenshots/observe.png)

![Tasks workspace — task list with run state and approval queue](docs/screenshots/tasks.png)

</details>

## What Works Today

Hecate is public-alpha software. The core gateway path is usable; the agent runtime and sandbox are intentionally still evolving.

| Area | State | Notes |
|---|---|---|
| OpenAI-compatible gateway | Usable | Chat Completions, streaming, vision, model discovery |
| Anthropic-compatible gateway | Usable | Messages API shape, streaming translation, Claude Code support |
| Provider catalog | Usable | Built-in presets, encrypted credentials, health, routing readiness |
| Local providers | Usable | Ollama, LM Studio, LocalAI, llama.cpp-compatible servers |
| Auth, tenants, keys | Usable | Admin bearer plus tenant API keys with provider/model scoping |
| Budgets and rate limits | Usable | Balances, warning thresholds, pricebook, `429` rate-limit headers |
| Caching | Usable | Exact cache; semantic cache is available but still early |
| OpenTelemetry | Usable | OTLP traces, metrics, logs, response headers, local trace view |
| Storage tiers | Usable | Memory, SQLite, Postgres, selected per subsystem |
| Operator UI | Usable | Main workflows are present; debugging ergonomics are still improving |
| Agent task runtime | Alpha | Queues, approvals, resumable runs, `agent_loop`, MCP integration |
| Execution isolation | Alpha | `sandboxd` boundary exists; stronger OS-level isolation is future work |

Read [docs/known-limitations.md](docs/known-limitations.md) before treating Hecate as production-stable.

## Configuration

The README intentionally stays light on configuration. The source of truth is:

- [`.env.example`](.env.example) — practical first-run environment knobs.
- [docs/deployment.md](docs/deployment.md) — Docker, storage tiers, rate limits, image pinning, reset/recovery.
- [docs/providers.md](docs/providers.md) — provider presets, local runtimes, credentials, health.
- [docs/telemetry.md](docs/telemetry.md) — OTLP traces, metrics, logs, collector recipes.
- [docs/agent-runtime.md](docs/agent-runtime.md) — task runtime, approvals, tools, workspace modes.
- [docs/mcp.md](docs/mcp.md) — MCP server and MCP tool integration.

## Documentation

- [Architecture](docs/architecture.md)
- [Agent runtime](docs/agent-runtime.md)
- [Runtime API](docs/runtime-api.md)
- [Providers](docs/providers.md)
- [Client integration](docs/client-integration.md)
- [MCP integration](docs/mcp.md)
- [Telemetry](docs/telemetry.md)
- [Deployment](docs/deployment.md)
- [Development](docs/development.md)
- [Known limitations](docs/known-limitations.md)
- [Release process](docs/release.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). If you work with an AI assistant, start with [AGENTS.md](AGENTS.md); the vendor-neutral agent instruction layer lives in [ai/](ai/README.md).

## License

MIT. See [LICENSE](LICENSE).

Third-party data and software notices live in [NOTICE.md](NOTICE.md), including LiteLLM pricing-data attribution.
