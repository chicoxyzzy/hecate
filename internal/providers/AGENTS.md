# Agent guide for `internal/providers/`

Provider adapters: outbound HTTP from the gateway to LLM upstreams. Sibling to root [`AGENTS.md`](../../AGENTS.md). Read that first for cross-cutting conventions.

## Layout

```
provider.go             Provider / Streamer / Capabilities interfaces
openai.go               OpenAI-compat adapter (real OpenAI, Together, Groq, Ollama, vLLM)
anthropic.go            Native Anthropic Messages API adapter
runtime_manager.go      provider catalog + protocol → adapter dispatch
capabilities_cache.go   /v1/models discovery + TTL cache
discovery_policy.go     when to discover, when to use static caps
health.go               provider health probe + circuit
mutable_registry.go     control-plane mutation surface
```

Test helpers live in `provider_test_helpers_test.go` and `tooluse_test.go` (the `newAnthropicTestProvider` helper is there, not in `anthropic_test.go`).

## The capital/lowercase parallel-struct rule

`internal/api/openai.go` defines `OpenAIChatMessage`, `OpenAIMessageContent`, `OpenAIContentBlock` (capital).
This package defines `openAIChatMessage`, `openAIMessageContent`, `openAIContentBlock` (lowercase).

**Same JSON shape, two packages, intentional.** Keeps `internal/providers/` free of `internal/api/` imports — the wire shapes evolve independently. When you add a field on one side, mirror it on the other.

The polymorphic content type (`UnmarshalJSON` / `MarshalJSON` for string-or-array-or-null) is duplicated for the same reason. Don't try to share — the duplication is the contract.

## Capability cache + tests

Provider tests **must** seed `cachedCaps` or the discovery path will try to call `/v1/models` against your test transport with a nil request body and panic on JSON decode:

```go
provider.cachedCaps = Capabilities{
    Name: "openai", Kind: KindCloud,
    DefaultModel: "gpt-4o-mini",
    Models:       []string{"gpt-4o-mini"},
}
provider.capsExpiry = time.Now().Add(time.Minute)
```

Alternatively, the test transport can return an empty 200 for any request that isn't `/v1/chat/completions` — see `TestOpenAIProviderForwardsResponseFormat` for that pattern. Use whichever fits.

## Cross-provider translation

When a caller's request hits a provider whose protocol doesn't natively support a field, the convention is:

- **Translatable** (semantic equivalent exists): translate. Examples: OpenAI `tool_choice: "required"` ↔ Anthropic `{"type":"any"}`. OpenAI `image_url` block ↔ Anthropic `image` block with `source`.
- **Not translatable**: log-and-drop with a per-field warning hint, never silently discard.

The Anthropic adapter centralizes warn-and-drop in `warnUnsupportedFieldsDropped` (anthropic.go). Each entry names the field, includes the value, and points the operator at the right Anthropic-side equivalent (or notes there is none). Add new dropped fields here, not as scattered per-call warnings.

## When adding a new wire field

The 7-step chain (also in root AGENTS.md, repeated here because it's the most-redone provider task):

1. `pkg/types/chat.go` — field on `ChatRequest` with comment explaining pointer-vs-value choice
2. `internal/api/openai.go` — field on `OpenAIChatCompletionRequest` with `json:"x,omitempty"`
3. `internal/api/handler_chat.go` — copy in `normalizeChatRequest` return value
4. `openai.go` — field on `openAIChatCompletionRequest` (this package)
5. Same file: plumb in **both** `Chat` (`chatUpstream`) AND `ChatStream` `wireReq` constructions. Forgetting one is the most common bug — streaming silently drops the field.
6. `anthropic.go` — add a case to `warnUnsupportedFieldsDropped` with a hint
7. Tests:
   - `openai_test.go`: passthrough + omitempty (table-driven; see `TestOpenAIProviderForwardsTier2Passthroughs`)
   - `anthropic_test.go`: drop-not-leaked (single test asserting field absent on Anthropic wire)

## Streaming-specific gotchas

- **`translateAnthropicSSE` (anthropic.go)** consumes Anthropic SSE and emits OpenAI-format chunks. The `usageSnapshot` accumulator captures input + cache tokens at `message_start` and updates output_tokens at every `message_delta`. The final usage chunk uses `anthropicUsageToTypes` to map to OpenAI's flat shape with `prompt_tokens_details.cached_tokens` for cache hits.
- **`translateOpenAIToAnthropicSSE` (in `internal/api/handler_messages.go`)** is the reverse direction. Both directions need to stay in sync when streaming-related fields are added.
- **Tool-call streaming**: OpenAI streams `function.arguments` as partial JSON in `delta.tool_calls`; Anthropic streams the same as `input_json_delta`. Both translators handle this — pin tests when modifying.

## Prompt caching (Anthropic-specific)

`anthropicUsage` captures three buckets:
- `input_tokens` — fresh tokens
- `cache_read_input_tokens` → `Usage.CachedPromptTokens` (priced via `CachedInputMicrosUSDPerMillionTokens`)
- `cache_creation_input_tokens` → folded into `Usage.PromptTokens` (priced at fresh rate; under-charges by ~20% per Anthropic's actual 1.25x rate, but at least counts them — the prior adapter dropped them entirely)

When the pricebook gains a dedicated `cache_creation` rate, split `cache_creation_input_tokens` back into its own `Usage` field. Comment in `anthropicUsage` documents the trade-off.

## Common bugs I've actually hit here

- **Forgot to plumb a field into the streaming `wireReq`** — request works in non-stream tests, drops the field in production for any client using `stream: true`. The streaming wireReq is around line ~500 of openai.go; not the same as the non-stream one.
- **Capital/lowercase struct mix-up** — wrote a test against `openAIChatMessage` but built the request using `OpenAIChatMessage`. Compiles in their respective packages; doesn't catch the actual JSON-shape drift.
- **Silently passing through unknown content blocks to Anthropic** — sending `{"type":"image_url"}` to Anthropic 400s the upstream because Anthropic only knows `image`. Always translate or drop, never pass through unknown types.
- **CodeQL CWE-190**: `make([]T, 0, len(x)+N)` is flagged as integer-overflow risk. Use plain `len(x)` and let `append` grow.
