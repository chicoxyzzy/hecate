# Client Integration (Codex And Claude Code)

This guide explains how to point external coding clients at Hecate as the model gateway.

## Base URL and endpoints

Use your Hecate gateway URL (local default: `http://127.0.0.1:8080`).

Supported LLM-facing endpoints:

| Client style | Endpoint |
| --- | --- |
| OpenAI-compatible (Codex-style) | `POST /v1/chat/completions` |
| Anthropic Messages (Claude Code-style) | `POST /v1/messages` |
| Model discovery | `GET /v1/models` |

## Authentication options

Hecate accepts either:

- `Authorization: Bearer <token>`
- `x-api-key: <token>`

Token sources:

- `GATEWAY_AUTH_TOKEN` (admin token)
- control-plane API keys (recommended for non-admin client access)

If both headers are present, Hecate uses `Authorization` first.

## Codex setup

Most Codex/OpenAI-compatible tools can be configured with OpenAI-style env vars.

Example:

```bash
export OPENAI_BASE_URL="http://127.0.0.1:8080/v1"
export OPENAI_API_KEY="hecate-client-token"
```

If your Codex client exposes custom headers instead of `OPENAI_API_KEY`, set either:

- `Authorization: Bearer hecate-client-token`, or
- `x-api-key: hecate-client-token`.

## Claude Code setup

Claude Code and Anthropic-style clients usually support:

```bash
export ANTHROPIC_BASE_URL="http://127.0.0.1:8080"
export ANTHROPIC_API_KEY="hecate-client-token"
```

Hecate accepts this key via `x-api-key`. If your client supports explicit auth headers, `Authorization: Bearer ...` also works.

## Smoke tests

### 1) Models

```bash
curl -sS "http://127.0.0.1:8080/v1/models" \
  -H "Authorization: Bearer hecate-client-token"
```

### 2) OpenAI-compatible chat

```bash
curl -sS "http://127.0.0.1:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer hecate-client-token" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

### 3) Anthropic messages

```bash
curl -sS "http://127.0.0.1:8080/v1/messages" \
  -H "Content-Type: application/json" \
  -H "x-api-key: hecate-client-token" \
  -d '{
    "model": "gpt-4o-mini",
    "max_tokens": 64,
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

## Common failures

- `401 unauthorized`: missing or invalid token.
- `403 forbidden`: token authenticated, but tenant/model/provider policy denies access.
- `402 payment_required`: budget is exhausted.
- `429 rate_limit_error`: request rate exceeded for the API key.

For response headers, traces, and OTLP details, see [`docs/telemetry.md`](telemetry.md).
