# Agent guide for Hecate

Scannable map for AI agents in this repo. Loaded by `agent_loop`
as the workspace prompt layer (`internal/api/system_prompt.go`,
8 KiB cap). Canonical docs live in `docs/`.

## Agent-context surfaces

Several files inform agent work in this repo. The shapes differ:

| File | Use for |
|---|---|
| `AGENTS.md` (this) | Backend map, conventions, recipes, gotchas — auto-loaded by `agent_loop` |
| `SKILL.md` | Backend product lens (Claude Code skill) |
| `ui/AGENTS.md` + `ui/SKILL.md` | UI map / design lens |
| `internal/providers/AGENTS.md` | Provider adapter patterns, capital/lowercase struct rule |
| `.claude/commands/*.md` | Slash commands: `/race`, `/typecheck`, `/diff-stat`, `/commit-msg`, `/test-affected` |
| `docs/*` | Long-form references — architecture, agent-runtime, events, telemetry |

**When in doubt:** read this file + run `/diff-stat`.

## Codebase map

```
cmd/hecate/             gateway binary entry
cmd/sandboxd/           out-of-process sandbox executor

pkg/types/              public types (ChatRequest, Message, ContentBlock, ...)
                          — no internal/ imports

internal/api/           inbound HTTP shapes + handlers
                          OpenAIChatMessage, OpenAIMessageContent (uppercase)
internal/providers/     outbound HTTP per provider (openai, anthropic)
                          openAIChatMessage, openAIMessageContent (lowercase)
                          — same JSON shape as api/, deliberate duplication
                          — keeps providers free of api-package imports
internal/orchestrator/  task runtime (queue, runner, agent_loop, sandbox)
internal/sandbox/       policy + sandboxd boundary
internal/taskstate/     task / run / step / artifact / approval persistence
internal/storage/       postgres + sqlite client wrappers
internal/retention/     retention worker (subsystems: traces, budget, audit, cache, turn_events)
internal/mcp/           stdio MCP server (read tools + write tools)
```

**Storage tier rule**: every backend-bound package mirrors three tiers — `memory` (in-process, default), `sqlite` (modernc.org, no CGO), `postgres` (pgx). When adding a new persisted thing, mirror all three.

## Conventions

- **Comments explain *why*, not *what***. Dense, contextual. State the trade-off in the code so the next reader doesn't have to git-blame for context.
- **Pointers vs values for optional fields**:
  - Pointer when zero is a valid distinct value: `Seed *int`, `ParallelToolCalls *bool`.
  - Value with `omitempty` when zero == API default: `PresencePenalty float64`, `Logprobs bool`.
- **`json.RawMessage`** for forward-compat passthrough (response_format, logit_bias, stream_options). Decode lazily where the gateway needs to inspect; stay out of the way otherwise.
- **Test naming**: `TestPackage_Behavior`. Table-driven where the variant set is obvious.
- **Commits**: Conventional Commits; `/commit-msg` proposes one. Pure `*.md` skips CI via `paths-ignore`; for inert source changes append `[skip ci]`.

## Test helper cheat-sheet

| Helper | File | Use for |
|---|---|---|
| `testRoundTripperFunc` | `internal/providers/provider_test_helpers_test.go` | Stub HTTP transport for provider tests |
| `newAnthropicTestProvider` | `internal/providers/tooluse_test.go` | Anthropic provider with cached caps (skips discovery) |
| `newTestHTTPHandler` / `*WithConfig` / `*ForProviders` | `internal/api/server_test.go` | In-process gateway handler |
| `fakeUpstreamCapturing` | `e2e/gateway_test.go` | E2E: capture what gateway forwarded to upstream |
| `hecateServer` | `e2e/gateway_test.go` | E2E: spawn the real binary on a free port |

**Capability-cache seeding** is required in provider tests, otherwise the discovery path will try to call `/v1/models` against your stub transport with a nil request body and panic. Pattern:

```go
provider.cachedCaps = Capabilities{Name: "openai", Kind: KindCloud, DefaultModel: "...", Models: []string{"..."}}
provider.capsExpiry = time.Now().Add(time.Minute)
```

**E2E build tags**: `//go:build e2e` (always required), plus optional `ollama` and `docker` sub-tags. Run with `go test -tags e2e ./e2e/...`. Use `PROVIDER_FAKE_KIND=local` to skip the pricebook preflight on synthetic test models.

**Runtime/backend verification**: if a change touches runtime behavior (`internal/gateway`, `internal/router`, `internal/providers`, `internal/orchestrator`, `internal/sandbox`, retention/state wiring, or other request execution paths), finish by running the race suite, not just the focused package tests. Default command: `GOCACHE=/Users/chicoxyzzy/dev/hecate/.gocache go test -race -timeout 10m ./...`.

## Recipes

### Add a passthrough field end-to-end (OpenAI-only knob)

The chain that bit me three times before I stopped re-discovering it:

1. `pkg/types/chat.go` → field on `ChatRequest`
2. `internal/api/openai.go` → field on `OpenAIChatCompletionRequest` with `json:"x,omitempty"`
3. `internal/api/handler_chat.go` → copy in `normalizeChatRequest` return value
4. `internal/providers/openai.go` → field on `openAIChatCompletionRequest`
5. Same file: plumb in **both** `Chat` and `ChatStream` `wireReq` constructions (forgetting one is the most common mistake)
6. `internal/providers/anthropic.go` → add a case to `warnUnsupportedFieldsDropped`
7. Tests at each layer — see `TestOpenAIProviderForwardsTier2Passthroughs` for the template

### Add an MCP tool

`internal/mcp/tools.go`:

1. Append a `s.RegisterTool(...)` call in `RegisterDefaultTools` with `Annotations` set (ReadOnlyHint / DestructiveHint / IdempotentHint as appropriate)
2. Add a `<name>Handler` returning `ToolHandler` further down
3. Update `docs/mcp.md` tool table
4. Tests in `internal/mcp/tools_test.go` using `fakeGateway` helper

### Add a persisted run-event type

1. `internal/orchestrator/runner.go` → call `r.emitRunEvent(ctx, taskID, runID, "your.event.type", ..., extraDataMap)` at the right life-cycle moment
2. Document the event + payload in `docs/events.md`
3. If high-cardinality, wire into `internal/retention/retention.go` as a new subsystem (see `turn_events` for the pattern)

## Gotchas

- **modernc/sqlite TIME-as-text format**: the driver writes `time.Time` as Go's default `time.Time.String()` format (`2026-04-28 02:37:38.4524 +0000 UTC`). That doesn't lex-compare with RFC3339Nano cutoffs and broke the retention sweep silently. Always write timestamps as `t.UTC().Format(time.RFC3339Nano)` explicitly when the column is TEXT (see `internal/taskstate/sqlite.go` `AppendRunEvent`).
- **mermaid `loop` is a reserved keyword**: don't use it as a sequence-diagram participant name (collides with the `loop ... end` block syntax). Use `Agent` or similar.
- **OpenAI/openAI parallel structs are intentional**: `internal/api/OpenAIChatMessage` (capital O) and `internal/providers/openAIChatMessage` (lowercase o) duplicate the JSON shape on purpose. Keeps the providers package free of api-package imports. Don't unify them.
- **Capabilities cache seeding** (above): provider test transports must handle `/v1/models` OR the test must seed `cachedCaps` to skip discovery.
- **Pricebook preflight**: cloud-kind providers in tests trigger a pricebook lookup. `PROVIDER_FAKE_KIND=local` bypasses it for synthetic models in e2e.
- **CodeQL CWE-190**: don't compute `make([]T, 0, len(x)+N)` with arithmetic — flagged as overflow risk. Use plain `len(x)` and let `append` grow once if needed.
## Canonical docs

| Doc | Covers |
|---|---|
| `docs/architecture.md` | Request flow, lease semantics, storage tier matrix |
| `docs/agent-runtime.md` | `agent_loop` tools, four-layer system prompt, cost model, retry-from-turn |
| `docs/runtime-api.md` | Task / run / step / approval endpoints, queue + lease |
| `docs/events.md` | Every event type at `/v1/events` with payload shapes |
| `docs/telemetry.md` | OTel spans + metrics, OTLP wiring, status & gaps |
| `docs/providers.md` | Provider catalog, configuration |
| `docs/client-integration.md` | Codex / Claude Code setup, multi-modal, OpenAI cross-provider behavior |
| `docs/mcp.md` | MCP server: tools, transport, configure |
| `docs/deployment.md` | Compose profiles, image pinning, lost-token recovery |
| `docs/development.md` | Local build, testing, screenshot tooling, `[skip ci]` convention |

## Commit etiquette

Don't auto-commit. After a change, propose a Conventional Commits (or run `/commit-msg`); the user merges.
