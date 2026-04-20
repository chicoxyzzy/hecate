import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { buildRequestOptions, chatCompletions, getBudget } from "./api";

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
            "X-Runtime-Provider": "openai",
            "X-Runtime-Provider-Kind": "cloud",
            "X-Runtime-Route-Reason": "explicit_model",
            "X-Runtime-Requested-Model": "gpt-4o-mini",
            "X-Runtime-Model": "gpt-4o-mini",
            "X-Runtime-Cache": "false",
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
    expect(result.headers.provider).toBe("openai");
    expect(result.headers.routeReason).toBe("explicit_model");
  });
});

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
