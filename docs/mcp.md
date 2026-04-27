# MCP server

Hecate ships with an MCP (Model Context Protocol) server that exposes its task, chat-session, and observability surfaces to MCP-aware clients — Claude Desktop, Cursor, Zed, and anything else that speaks the [MCP spec](https://modelcontextprotocol.io/).

The server runs as a subcommand of the `hecate` binary on stdio, talking back to a running gateway over its public REST API. Operators add it to their MCP client's config, and the agent runtime surfaces become callable from inside the editor.

## What's available

Four tools in v0.1, all read-mostly:

| Tool | Description |
|---|---|
| `list_tasks` | Recent agent tasks: id, title, status, execution kind, step count |
| `get_task_status` | Detailed status of one task by id, including its latest run |
| `list_chat_sessions` | Recent chat sessions: id, title, tenant, turn count |
| `summarize_recent_traffic` | Aggregated request stats: by-provider breakdown, error rate, avg latency |

More tools (create_task, resolve_approval, search_traces) land in v0.2 alongside HTTP/SSE transport and the **client-side** integration that lets the agent runtime consume external MCP servers.

## Configure it

The MCP server is a stdio subprocess. Two environment variables control where it talks:

| Variable | Default | Notes |
|---|---|---|
| `HECATE_BASE_URL` | `http://127.0.0.1:8080` | URL of the running Hecate gateway |
| `HECATE_AUTH_TOKEN` | _required_ | The bearer token from the gateway's first-run banner (or `/data/hecate.bootstrap.json` → `admin_token`) |

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json` (macOS) or the Windows / Linux equivalent:

```json
{
  "mcpServers": {
    "hecate": {
      "command": "hecate",
      "args": ["mcp-server"],
      "env": {
        "HECATE_BASE_URL": "http://127.0.0.1:8080",
        "HECATE_AUTH_TOKEN": "<paste from first-run banner>"
      }
    }
  }
}
```

Restart Claude Desktop. The connector should appear in the tools menu; mention `@hecate` in a conversation to invoke a tool.

### Cursor / Zed / other MCP clients

Same shape. Cursor's `~/.cursor/mcp.json` and Zed's MCP settings both accept the `command` / `args` / `env` format above.

## Verify it locally

A quick smoke test without an MCP client:

```bash
printf '%s\n%s\n%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"smoke","version":"0"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  | HECATE_AUTH_TOKEN=<token> hecate mcp-server
```

Expected output: two JSON-RPC responses on stdout (initialize result + tools list). The startup line `hecate mcp-server: started on stdio, talking to ...` goes to stderr, which the protocol channel ignores.

## Behavior notes

- **Tool errors are not protocol errors.** When the upstream gateway is unreachable or returns a 5xx, the tool's `CallToolResult` carries `isError: true` with the error text in the content block. The MCP envelope itself stays a successful JSON-RPC response — that's what the spec requires, and it's also what clients render meaningfully.
- **Auth is per-process.** The token is read once at startup from `HECATE_AUTH_TOKEN` and used as `Authorization: Bearer <token>` on every gateway request. Rotate the token in the gateway and restart the MCP subprocess; there's no live re-read.
- **One token = one principal.** The MCP server runs against whatever role the token grants — admin token sees everything, a tenant API key sees only its own tasks/sessions. Pick deliberately.
- **Pure-Go single binary.** The MCP server has no extra dependencies; it's the same `hecate` binary you already have, dispatched by the first arg.

## Spec compliance

- **Protocol version**: `2024-11-05` (the first stable release). Newer revisions are additive on the wire; we'll bump as we adopt features.
- **Transport**: stdio with newline-delimited JSON-RPC 2.0 messages. HTTP/SSE is on the v0.2 roadmap.
- **Capabilities declared**: `tools` only. Resources, prompts, sampling, and logging come later.
