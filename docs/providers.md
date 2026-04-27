# Providers

Hecate uses a vendor-neutral provider layer at the runtime boundary. It treats OpenAI-compatible upstreams and the Anthropic Messages API as first-class paths — every other supported model lives behind one of those two protocols.

![Providers tab — every preset card with health, model count, and configured-credentials state](screenshots/providers.png)

## Built-in presets

The gateway ships with twelve providers wired up by default. The Providers tab in the operator UI lists all of them; you only need to drop in an API key (cloud) or start the local runtime (local) to enable one.

### Cloud presets

| ID | Name | Default base URL |
|---|---|---|
| `anthropic` | Anthropic | `https://api.anthropic.com/v1` |
| `deepseek` | DeepSeek | `https://api.deepseek.com/v1` |
| `gemini` | Google Gemini | `https://generativelanguage.googleapis.com/v1beta/openai` |
| `groq` | Groq | `https://api.groq.com/openai/v1` |
| `mistral` | Mistral | `https://api.mistral.ai/v1` |
| `openai` | OpenAI | `https://api.openai.com/v1` |
| `together_ai` | Together AI | `https://api.together.xyz/v1` |
| `xai` | xAI | `https://api.x.ai/v1` |

### Local presets

| ID | Name | Default base URL |
|---|---|---|
| `llamacpp` | llama.cpp | `http://127.0.0.1:8080/v1` |
| `lmstudio` | LM Studio | `http://127.0.0.1:1234/v1` |
| `localai` | LocalAI | `http://127.0.0.1:8080/v1` |
| `ollama` | Ollama | `http://127.0.0.1:11434/v1` |

`llamacpp` and `localai` share the same default port — the gateway resolves the conflict automatically by enabling whichever one was configured first; the operator can flip the active one in the Providers tab.

## Configuring a provider

Three approaches, listed from least-to-most production-friendly:

### 1. Environment variables (good for first-run)

Every preset reads three env knobs by lowercased ID:

- `PROVIDER_<NAME>_API_KEY`
- `PROVIDER_<NAME>_BASE_URL` (override the default if you're using a self-hosted proxy)
- `PROVIDER_<NAME>_DEFAULT_MODEL`

Example `.env`:

```bash
PROVIDER_ANTHROPIC_API_KEY=sk-ant-...
PROVIDER_OPENAI_API_KEY=sk-...
PROVIDER_OPENAI_DEFAULT_MODEL=gpt-4o-mini
```

Advanced overrides (`PROVIDER_<NAME>_PROTOCOL`, `PROVIDER_<NAME>_API_VERSION`, `PROVIDER_<NAME>_TIMEOUT_SECONDS`) are also available — see [`internal/config/config.go`](../internal/config/config.go) for the authoritative list.

### 2. Operator UI (recommended for ongoing changes)

The Providers tab in the operator UI lists every preset. Click a card to expand its detail panel; paste the API key in. The key is encrypted at rest with `GATEWAY_CONTROL_PLANE_SECRET_KEY` and never leaves the gateway in plaintext after that point. Rotate or revoke from the same panel.

### 3. Control-plane API (for automation)

Every UI action maps to a `PUT /admin/control-plane/providers/{id}` or `PATCH /admin/control-plane/providers/{id}` call. The full surface lives in [`internal/api/handler_controlplane.go`](../internal/api/handler_controlplane.go). Useful for terraforming a fleet of gateways from a single config source of truth.

## Health and circuit breaking

Each provider has a per-process health tracker. After a configurable threshold of consecutive failures the breaker opens; the router skips that provider and falls over to the next eligible one. A half-open probe re-opens the breaker after a cooldown.

The Providers tab shows the current state on each card:

- 🟢 **Healthy** — recent successful traffic
- 🟡 **Degraded / half-open** — recent failures, probing for recovery
- 🔴 **Open** — circuit open, requests skip this provider entirely
- ⚪ **Unknown** — no traffic yet to evaluate

Health state is in-process and resets on restart by design — durable health tracking would re-include known-broken upstreams that recovered while the gateway was down.

## Adding a custom provider

If your upstream isn't a preset but speaks OpenAI-compatible JSON:

1. POST to `/admin/control-plane/providers` with `{id, name, kind: "cloud" | "local", protocol: "openai" | "anthropic", base_url, ...}`.
2. Set its API key with `PUT /admin/control-plane/providers/{id}/api-key`.
3. Enable it with `PATCH /admin/control-plane/providers/{id}` `{"enabled": true}`.

The custom provider then appears in the Providers tab alongside the presets, and `GET /v1/models` advertises its discovered models.
