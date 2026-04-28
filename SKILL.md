---
name: hecate-backend
description: Use when working on the Hecate Go backend — gateway, agent runtime, providers, sandbox, storage. Keeps backend work aligned with Hecate's "single binary, operator-grade control plane, runtime-aware" thesis.
---

# Hecate Backend Skill

Use this skill for any work outside `ui/` (the React UI has its own [SKILL.md](ui/SKILL.md)).

This is not a generic LLM proxy. It is an operator-grade control plane for AI gateways and agent runtimes — the kind of thing you bind to `:8080`, hand a bearer token to an SDK, and trust to mediate cost, policy, retries, and runtime visibility for everything an organization sends to an LLM.

## Product Lens

The backend should feel like:

- a single-binary control plane
- a deny-by-default policy enforcer
- a runtime-aware proxy that explains its decisions
- a debugging surface — every request leaves a trace, every cost is itemized, every approval is logged

It should not feel like:

- a thin pass-through with marketing on top
- a configurable framework where you bring your own everything
- a research demo that works in one provider's happy path

Default to operator confidence: clear status, clear errors, deterministic state, no surprises on restart.

## Engineering Thesis

Calm, durable, and explicit. Code should age well — the runtime is supposed to live for years, not iterations.

Prefer:

- single binary, single port, embedded UI (`//go:embed ui/dist`)
- deterministic startup: env-driven config, no surprise file fetches
- backend tier choice surfaced as a config knob, never inferred
- explicit error wrapping with cause chains
- comments that explain *why*, not *what* — and the trade-off being made
- standard library first, well-known third party second, novel deps last

Avoid:

- magic auto-discovery that breaks "what did the gateway just do?"
- silent fallbacks that mask misconfiguration
- packages that import everything they touch
- ORM-style abstractions over what's a thin SQL layer
- generic frameworks where direct code would do

## Operator Priorities

Every endpoint, every config knob, every error message should answer:

1. What did the gateway just decide?
2. Why did it decide that?
3. What did it cost / how long did it take?
4. What happens if it fails next time — retry, fallback, fail?
5. How do I find the trace for this in OTel?

When choosing between "elegant" and "operationally explicit," choose explicit.

## Architecture Boundaries

The codebase has three concentric rings; cross-ring imports go inward only:

- **`pkg/types/`** — public types, no internal/ imports. The wire-shape contract.
- **`internal/api/`** — inbound HTTP shapes + handlers. Translates HTTP requests into internal types, never touches providers directly.
- **`internal/providers/`** — outbound HTTP per provider (OpenAI-compat, Anthropic). Translates internal types to provider wire shapes. Never imports `internal/api/`.
- **`internal/orchestrator/`** — task runtime (queue, runner, agent_loop, sandbox boundary). Sits above providers, called by api.
- **`internal/<feature>/`** — gateway services (governor, router, cache, retention, taskstate, mcp, …). Each owns one concern.

When tempted to share a struct between api/ and providers/: **don't**. Duplicate the JSON shape (see `OpenAIChatMessage` ↔ `openAIChatMessage`). The boundary is intentional — keeps providers free of api-package imports and lets the wire shapes evolve independently.

## Storage Tier Rule

Every backend-bound concern (cache, taskstate, chatstate, governor, retention history) ships with three tiers, **mirrored exactly**:

- `memory` — in-process, default, perfect for `go test` and `make dev`
- `sqlite` — single-file persistence via `modernc.org/sqlite` (no CGO)
- `postgres` — production scale via `pgx`

When adding a new persisted thing, mirror all three. Add a `<thing>_test.go` that runs against memory and sqlite (postgres is structurally identical SQL — covered transitively).

## Hecate-Specific Backend Rules

- **Auth is a path-level decision.** `/v1/chat/completions` accepts tenant API keys; `/admin/*` requires admin bearer. `/v1/tasks/*` accepts both. Don't blur these.
- **Tenant scoping is automatic.** Once a request has a tenant principal, every subsequent store query gets `WHERE tenant = ?` injected. New endpoints must respect this — never bypass via the admin path.
- **Sandbox is out-of-process.** Shell, file, git execution runs inside `cmd/sandboxd`, invoked over an exec boundary. A buggy tool can't crash the gateway. New tools follow the same pattern.
- **Approvals are blocking.** Pre-execution and mid-loop approvals halt the run; the run record persists in `awaiting_approval` until resolved. New gates use the same `TaskApproval` shape.
- **Events are appended, not mutated.** Every state transition writes a `run_event` with a monotonic sequence. The SSE stream replays from `after_sequence`. New event types go in `docs/events.md`.
- **Cost is in micro-USD.** All money is `int64` in micro-USD (`1_000_000` = $1). Never use `float64` for money — pricebook lookups, budgets, ledger entries all stay integer.
- **OTel is first-class.** Every request gets a trace ID surfaced in the response header (`X-Trace-Id`) and persisted on the run record. New code paths add spans, not just log lines.

## Code Organization

Mental model:

- `cmd/<name>/` — binary entry points, CLI flags, dependency wiring
- `pkg/types/` — public types shared with external Go code
- `internal/<feature>/` — one Go package per concern; flat by default
- `internal/<feature>/<feature>_test.go` — table-driven tests next to the code
- `e2e/` — binary-startup tests (build tag `e2e`); sub-tags for `ollama`, `docker`

When a file gets crowded, split by responsibility, not by line count:

- request handlers vs response renderers
- inbound JSON shapes vs outbound wire shapes
- runtime-driven flows vs admin endpoints
- typed-store impl vs interface contract

## Field Shape Rules

For optional config or request fields, the choice between `T` and `*T`:

- **Pointer when zero is a valid distinct value.** `Seed *int` (0 is a real seed), `ParallelToolCalls *bool` (false means "disable"), `Strict *bool` (default depends on provider).
- **Value with `omitempty`** when zero == API default. `PresencePenalty float64` (0.0 = no penalty), `Logprobs bool` (false = default off).
- **`json.RawMessage`** for forward-compat passthrough fields (response_format, logit_bias, stream_options, tool_choice). Decode lazily where the gateway needs to inspect; pass through verbatim otherwise.

State the choice in a comment so the next reader understands the constraint.

## Testing Expectations

**Always add unit tests for new behavior.** Not "when practical" — as a default. A change without tests is incomplete. The bar is: a future contributor should be able to refactor the implementation and have the tests catch a regression in behavior. If you can't write a test for what you just added, the design is probably wrong (untestable side effects, hidden globals, etc.).

**Add e2e tests where they make sense.** The `e2e/` directory (build tag `e2e`, optional sub-tags `ollama` / `docker`) covers binary-startup behavior — the things that only break when the whole gateway is up. Reach for an e2e test when the change:

- spans the api → orchestrator → providers / sandbox / mcp chain end-to-end
- depends on real subprocess lifecycle (sandbox, mcp stdio host)
- changes startup or config-loading semantics
- adds a new SSE event sequence operators rely on
- mutates a public HTTP contract that downstream SDKs consume

Unit tests prove the seams. E2e tests prove they fit together.

Prioritize unit tests for:

- request → wire shape passthrough (especially across the api/providers boundary)
- error classification (which errors map to which HTTP status)
- tenant scoping (cross-tenant data must never leak)
- retry/failover decisions
- streaming wire shape (per-event SSE translation, usage accumulation)
- new tool dispatch (agent_loop tools, MCP tools)

Pin behavior, not implementation. The unit tests should still pass when someone refactors the implementation.

## Workflow Before Editing

Before making a substantial backend change, confirm:

- **What's the wire-shape impact?** Inbound HTTP types? Outbound provider wire? Both? Don't add a field on one side without the other.
- **Does this cross the api/providers boundary?** If so, plan the duplication carefully — and write tests on both sides of the boundary.
- **What's the error path?** New code that can fail needs an error class — `IsClientError`, `IsBudgetExceeded`, `IsRateLimited`, `IsDenied`, or a new one if the existing ones don't fit.
- **What's the OTel surface?** New requests = new spans. New events = new event types in `docs/events.md`.

If a change starts spanning many files, return to those four questions and split.

## Done Criteria

A backend change is in good shape when:

- the build passes (`go build ./...`)
- the race suite passes (`go test ./... -race -count=1`)
- inbound + outbound wire shapes are tested independently
- new env knobs are documented in `.env.example` and the relevant `docs/<feature>.md`
- error paths return the right HTTP status with a useful message
- OTel attributes are populated for new spans
- if a new event type was added, `docs/events.md` has its payload table

When in doubt, lean on the existing patterns. The codebase is internally consistent — match what's there before inventing something new.
