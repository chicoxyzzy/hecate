import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { buildRequestOptions, chatCompletions, getBudget, getSession, getTrace, setProviderAPIKey, setProviderEnabled } from "./api";

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
            "X-Runtime-Route-Reason": "requested_model",
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
    expect(result.headers.routeReason).toBe("requested_model");
    expect(result.headers.cacheType).toBe("false");
    expect(result.headers.semanticStrategy).toBe("postgres_pgvector");
    expect(result.headers.semanticIndex).toBe("hnsw");
    expect(result.headers.semanticSimilarity).toBe("0.981234");
    expect(result.headers.attempts).toBe("2");
    expect(result.headers.retries).toBe("1");
    expect(result.headers.fallbackFrom).toBe("ollama");
  });

  it("turns browser fetch failures into actionable gateway errors", async () => {
    fetchMock.mockRejectedValue(new TypeError("Load failed"));

    await expect(
      chatCompletions({
        model: "llama3.1:8b",
        provider: "ollama",
        user: "team-a",
        messages: [{ role: "user", content: "hello" }],
      }),
    ).rejects.toThrow("Check that the gateway is running on http://127.0.0.1:8080");
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

  describe("provider REST API", () => {
    it("PATCH /providers/{id} to enable a provider", async () => {
      fetchMock.mockResolvedValue(jsonResponse({}));

      await setProviderEnabled("anthropic", true, "admin-secret");

      expect(fetchMock).toHaveBeenCalledWith(
        "/admin/control-plane/providers/anthropic",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ enabled: true }),
        }),
      );
    });

    it("PATCH /providers/{id} URL-encodes provider names with special characters", async () => {
      fetchMock.mockResolvedValue(jsonResponse({}));

      await setProviderEnabled("my provider", false, "admin-secret");

      expect(fetchMock).toHaveBeenCalledWith(
        "/admin/control-plane/providers/my%20provider",
        expect.anything(),
      );
    });

    it("PUT /providers/{id}/api-key to set credentials", async () => {
      fetchMock.mockResolvedValue(jsonResponse({}));

      await setProviderAPIKey("anthropic", "sk-new-key", "admin-secret");

      expect(fetchMock).toHaveBeenCalledWith(
        "/admin/control-plane/providers/anthropic/api-key",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({ key: "sk-new-key" }),
        }),
      );
    });

    it("PUT /providers/{id}/api-key with empty key clears credentials", async () => {
      fetchMock.mockResolvedValue(jsonResponse({}));

      await setProviderAPIKey("anthropic", "", "admin-secret");

      expect(fetchMock).toHaveBeenCalledWith(
        "/admin/control-plane/providers/anthropic/api-key",
        expect.objectContaining({
          method: "PUT",
          body: JSON.stringify({ key: "" }),
        }),
      );
    });
  });

  describe("buildRequestOptions edge cases", () => {
    it("omits Authorization header when no auth token is supplied", () => {
      const options = buildRequestOptions({ method: "GET" });
      const headers = new Headers(options.headers);
      // The dashboard token-gate path issues unauthenticated requests
      // (GET /healthz, GET /v1/whoami) — sending a stray Authorization
      // would land an empty bearer at the gateway and trip the
      // invalid-token branch unnecessarily.
      expect(headers.get("Authorization")).toBeNull();
    });

    it("omits Content-Type for body-less requests", () => {
      const options = buildRequestOptions({ method: "DELETE", authToken: "x" });
      const headers = new Headers(options.headers);
      expect(headers.get("Content-Type")).toBeNull();
      expect(options.body).toBeUndefined();
    });

    it("defaults method to GET when not specified", () => {
      const options = buildRequestOptions({});
      expect(options.method).toBe("GET");
    });
  });

  describe("error-mapping edge cases", () => {
    it("surfaces the gateway's error.message when the body is well-formed JSON", async () => {
      // Most error paths route through fetchJSON's !response.ok branch,
      // which extracts {error: {message}} from the body. A regression
      // here turns every 4xx into the generic "request failed" string
      // and operators lose actionable detail in their toasts.
      fetchMock.mockResolvedValue(
        new Response(
          JSON.stringify({ error: { type: "rate_limit_exceeded", message: "slow down" } }),
          { status: 429, headers: { "Content-Type": "application/json" } },
        ),
      );
      await expect(
        getBudget("?scope=global", "admin-secret"),
      ).rejects.toThrow(/slow down/);
    });

    it("falls back to the static label when the error body is not valid JSON", async () => {
      // Some gateway frontends (CDNs, legacy reverse proxies) return
      // text/html error bodies. Without the try/catch in errorMessage
      // the whole thing would crash with a JSON parse error instead of
      // a clean "request failed" — leaving the operator with a stack
      // trace toast.
      fetchMock.mockResolvedValue(
        new Response("<html>500</html>", {
          status: 500,
          headers: { "Content-Type": "text/html" },
        }),
      );
      await expect(
        getBudget("?scope=global", "admin-secret"),
      ).rejects.toThrow(/request failed/);
    });

    it("returns undefined for a 204 No Content response", async () => {
      // 204 is the contract for DELETE endpoints. fetchJSON must not
      // call .json() on an empty body — that throws "Unexpected end of
      // JSON input" and silently turns successful deletes into
      // toast-error noise. The deleteChatSession test in
      // useRuntimeConsole.test.tsx hits this real-world.
      fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
      // setProviderAPIKey returns Promise<void> — but it's the closest
      // 204-eligible path here; the assertion is that we don't throw.
      await expect(
        setProviderAPIKey("anthropic", "", "admin-secret"),
      ).resolves.not.toThrow();
    });

    it("rewrites 'Failed to fetch' network errors into actionable gateway URLs", async () => {
      // Different browsers throw different strings for network-level
      // failure: Chrome throws "Failed to fetch", Safari throws
      // "Load failed". Both must produce the same actionable hint.
      fetchMock.mockRejectedValue(new TypeError("Failed to fetch"));
      await expect(
        getBudget("?scope=global", "admin-secret"),
      ).rejects.toThrow(/Check that the gateway is running/);
    });

    it("rewrites 'NetworkError' substring matches the same way", async () => {
      // Firefox uses NetworkError. The .includes() check covers
      // arbitrary suffixes the browser may add; assert the rewrite
      // still fires with a less-than-exact match.
      fetchMock.mockRejectedValue(new TypeError("NetworkError when attempting to fetch resource."));
      await expect(
        getBudget("?scope=global", "admin-secret"),
      ).rejects.toThrow(/Check that the gateway is running/);
    });

    it("preserves non-network error messages with the request URL prepended", async () => {
      // A non-network error (e.g. AbortError or a custom one) goes
      // through the fallback branch of networkErrorMessage and gets
      // wrapped as "Gateway request failed (url): message" — the URL
      // is the operator's only clue about which call broke.
      fetchMock.mockRejectedValue(new Error("AbortError: aborted"));
      await expect(
        getBudget("?scope=global", "admin-secret"),
      ).rejects.toThrow(/\/admin\/budget.*AbortError: aborted/);
    });
  });
});

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
