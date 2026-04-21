import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { buildRequestOptions, chatCompletions, getBudget, getSession, getTrace } from "./api";

describe("api client", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("adds auth and json headers when posting a body", () => {
    const options = buildRequestOptions({
      authToken: "tenant-secret",
      method: "POST",
      body: { hello: "world" },
    });

    const headers = new Headers(options.headers);
    expect(options.method).toBe("POST");
    expect(headers.get("Authorization")).toBe("Bearer tenant-secret");
    expect(headers.get("Content-Type")).toBe("application/json");
    expect(options.body).toBe(JSON.stringify({ hello: "world" }));
  });

  it("builds budget requests with query strings intact", async () => {
    fetchMock.mockResolvedValue(jsonResponse({ object: "budget_status", data: { key: "global" } }));

    await getBudget("?scope=tenant_provider&tenant=team-a&provider=ollama", "admin-secret");

    expect(fetchMock).toHaveBeenCalledWith(
      "/admin/budget?scope=tenant_provider&tenant=team-a&provider=ollama",
      expect.objectContaining({
        method: "GET",
      }),
    );
  });

  it("fetches session details for auth introspection", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        object: "session",
        data: {
          authenticated: true,
          invalid_token: false,
          role: "tenant",
          tenant: "team-a",
          source: "control_plane_api_key",
          key_id: "team-a-dev",
        },
      }),
    );

    const result = await getSession("tenant-secret");

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/whoami",
      expect.objectContaining({
        method: "GET",
      }),
    );
    expect(result.data.tenant).toBe("team-a");
    expect(result.data.key_id).toBe("team-a-dev");
  });

  it("returns chat payload plus runtime headers", async () => {
    fetchMock.mockResolvedValue(
      new Response(
        JSON.stringify({
          id: "chatcmpl-123",
          model: "gpt-4o-mini",
          choices: [{ index: 0, finish_reason: "stop", message: { role: "assistant", content: "Hello!" } }],
        }),
        {
          status: 200,
          headers: {
            "Content-Type": "application/json",
            "X-Request-Id": "req-123",
            "X-Trace-Id": "trace-123",
            "X-Span-Id": "span-123",
            "X-Runtime-Provider": "openai",
            "X-Runtime-Provider-Kind": "cloud",
            "X-Runtime-Route-Reason": "explicit_model",
            "X-Runtime-Requested-Model": "gpt-4o-mini",
            "X-Runtime-Model": "gpt-4o-mini",
            "X-Runtime-Cache": "false",
            "X-Runtime-Cache-Type": "false",
            "X-Runtime-Semantic-Strategy": "postgres_pgvector",
            "X-Runtime-Semantic-Index": "hnsw",
            "X-Runtime-Semantic-Similarity": "0.981234",
            "X-Runtime-Attempts": "2",
            "X-Runtime-Retries": "1",
            "X-Runtime-Fallback-From": "ollama",
            "X-Runtime-Cost-USD": "0.000123",
          },
        },
      ),
    );

    const result = await chatCompletions(
      {
        model: "gpt-4o-mini",
        provider: "",
        user: "team-a",
        messages: [{ role: "user", content: "hello" }],
      },
      "tenant-secret",
    );

    expect(result.data.id).toBe("chatcmpl-123");
    expect(result.headers.traceId).toBe("trace-123");
    expect(result.headers.spanId).toBe("span-123");
    expect(result.headers.provider).toBe("openai");
    expect(result.headers.routeReason).toBe("explicit_model");
    expect(result.headers.cacheType).toBe("false");
    expect(result.headers.semanticStrategy).toBe("postgres_pgvector");
    expect(result.headers.semanticIndex).toBe("hnsw");
    expect(result.headers.semanticSimilarity).toBe("0.981234");
    expect(result.headers.attempts).toBe("2");
    expect(result.headers.retries).toBe("1");
    expect(result.headers.fallbackFrom).toBe("ollama");
  });

  it("fetches a request trace by request id", async () => {
    fetchMock.mockResolvedValue(
      jsonResponse({
        object: "trace",
        data: {
          request_id: "req-123",
          trace_id: "req-123",
          started_at: "2026-04-21T00:00:00Z",
          spans: [
            {
              trace_id: "req-123",
              span_id: "span-1",
              name: "gateway.request",
              kind: "server",
              events: [
                { name: "request.received", timestamp: "2026-04-21T00:00:00Z", attributes: { model: "gpt-4o-mini" } },
                { name: "response.returned", timestamp: "2026-04-21T00:00:01Z", attributes: { provider: "openai" } },
              ],
            },
          ],
        },
      }),
    );

    const result = await getTrace("req-123", "tenant-secret");

    expect(fetchMock).toHaveBeenCalledWith(
      "/v1/traces?request_id=req-123",
      expect.objectContaining({
        method: "GET",
      }),
    );
    expect(result.data.request_id).toBe("req-123");
    expect(result.data.spans).toHaveLength(1);
    expect(result.data.spans?.[0]?.events).toHaveLength(2);
  });
});

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
