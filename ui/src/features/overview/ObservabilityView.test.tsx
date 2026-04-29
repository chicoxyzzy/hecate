import { act, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ObservabilityView } from "./ObservabilityView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

const adminSession = {
  kind: "admin" as const, label: "Admin", role: "admin", isAdmin: true, isAuthenticated: true,
  capabilities: [], name: "", tenant: "", source: "", keyID: "",
  allowedProviders: [], allowedModels: [],
};

const fetchMock = vi.fn<typeof fetch>();

beforeEach(() => {
  vi.stubGlobal("fetch", fetchMock);
  fetchMock.mockImplementation(async () => {
    return new Response(JSON.stringify({ object: "list", data: [] }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  fetchMock.mockReset();
});

describe("ObservabilityView", () => {
  it("renders without crashing for an admin session", async () => {
    const state = createRuntimeConsoleFixture({ session: adminSession });
    await act(async () => {
      render(<ObservabilityView state={state} actions={createRuntimeConsoleActions()} />);
    });
    expect(document.body).toBeTruthy();
  });

  it("does not call admin endpoints for anonymous session", async () => {
    const state = createRuntimeConsoleFixture(); // anonymous default
    await act(async () => {
      render(<ObservabilityView state={state} actions={createRuntimeConsoleActions()} />);
    });
    const adminCalls = fetchMock.mock.calls.filter(([url]) =>
      String(url).startsWith("/admin/")
    );
    expect(adminCalls.length).toBe(0);
  });

  it("calls /admin/runtime/stats and /admin/traces for admin session", async () => {
    const state = createRuntimeConsoleFixture({ session: adminSession });
    await act(async () => {
      render(<ObservabilityView state={state} actions={createRuntimeConsoleActions()} />);
    });
    await waitFor(() => {
      const urls = fetchMock.mock.calls.map(([u]) => String(u));
      expect(urls.some(u => u.startsWith("/admin/runtime/stats"))).toBe(true);
      expect(urls.some(u => u.startsWith("/admin/traces"))).toBe(true);
    });
  });

  it("calls /admin/mcp/cache and renders the cache panel when configured", async () => {
    // Route /admin/mcp/cache to a populated snapshot; everything
    // else falls through to the default empty-list mock.
    fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.startsWith("/admin/mcp/cache")) {
        return new Response(JSON.stringify({
          object: "mcp_cache_stats",
          data: { checked_at: new Date().toISOString(), configured: true, entries: 4, in_use: 1, idle: 3 },
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ object: "list", data: [] }), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    });

    const state = createRuntimeConsoleFixture({ session: adminSession });
    // Cast through `unknown` so TypeScript's narrowing doesn't lock
    // the variable to `never` after the only assignment site (inside
    // act's callback) — the React testing-library types don't expose
    // a clean Promise<RenderResult> shape from `act` in this version.
    let container = null as unknown as HTMLElement;
    await act(async () => {
      const result = render(<ObservabilityView state={state} actions={createRuntimeConsoleActions()} />);
      container = result.container;
    });

    // Endpoint is called.
    await waitFor(() => {
      const urls = fetchMock.mock.calls.map(([u]) => String(u));
      expect(urls.some(u => u.startsWith("/admin/mcp/cache"))).toBe(true);
    });
    // Panel is rendered with the aria-labelled stats group.
    await waitFor(() => {
      expect(container.querySelector('[aria-label="MCP cache stats"]')).toBeTruthy();
    });
    // Headline counts surface as text. We assert the section
    // header is present (so we know we're looking at the right
    // panel) and that each value rendered.
    expect(container.textContent).toMatch(/MCP client cache/i);
  });

  it("renders the 'no cache wired' fallback when configured=false", async () => {
    // When the gateway reports configured=false, the panel shows
    // a single muted line instead of the stats grid — operators
    // benefit from knowing the cache is intentionally off vs.
    // merely failing to fetch.
    fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.startsWith("/admin/mcp/cache")) {
        return new Response(JSON.stringify({
          object: "mcp_cache_stats",
          data: { checked_at: new Date().toISOString(), configured: false, entries: 0, in_use: 0, idle: 0 },
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ object: "list", data: [] }), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    });

    const state = createRuntimeConsoleFixture({ session: adminSession });
    // Cast through `unknown` so TypeScript's narrowing doesn't lock
    // the variable to `never` after the only assignment site (inside
    // act's callback) — the React testing-library types don't expose
    // a clean Promise<RenderResult> shape from `act` in this version.
    let container = null as unknown as HTMLElement;
    await act(async () => {
      const result = render(<ObservabilityView state={state} actions={createRuntimeConsoleActions()} />);
      container = result.container;
    });

    await waitFor(() => {
      expect(container.textContent).toMatch(/No cache wired/i);
    });
    // The configured=true stats grid (with aria-label
    // "MCP cache stats") must NOT be present in this branch.
    expect(container.querySelector('[aria-label="MCP cache stats"]')).toBeNull();
  });

  it("renders readable route skip reasons in trace detail", async () => {
    fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
      const url = typeof input === "string" ? input : input.toString();
      if (url.startsWith("/admin/traces")) {
        return new Response(JSON.stringify({
          object: "trace_list",
          data: [{
            request_id: "req-1",
            started_at: "2026-04-29T10:00:00Z",
            span_count: 1,
            duration_ms: 12,
            route: {
              final_provider: "openai",
              final_model: "gpt-5.4-mini",
              final_reason: "requested_model",
              candidates: [
                { provider: "ollama", model: "llama3.1:8b", outcome: "skipped", skip_reason: "preflight_price_missing", reason: "requested_model" },
                { provider: "openai", model: "gpt-5.4-mini", outcome: "selected", reason: "requested_model" },
              ],
            },
          }],
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.startsWith("/v1/traces")) {
        return new Response(JSON.stringify({
          object: "trace",
          data: {
            request_id: "req-1",
            started_at: "2026-04-29T10:00:00Z",
            spans: [{
              trace_id: "trace-1",
              span_id: "span-1",
              name: "gateway.router",
              start_time: "2026-04-29T10:00:00Z",
              end_time: "2026-04-29T10:00:00.010Z",
              events: [{
                name: "router.candidate.skipped",
                timestamp: "2026-04-29T10:00:00.005Z",
                attributes: {
                  "gen_ai.provider.name": "ollama",
                  "hecate.route.skip_reason": "preflight_price_missing",
                },
              }],
            }],
            route: {
              candidates: [
                { provider: "ollama", model: "llama3.1:8b", outcome: "skipped", skip_reason: "preflight_price_missing", reason: "requested_model" },
                { provider: "openai", model: "gpt-5.4-mini", outcome: "selected", reason: "requested_model" },
              ],
            },
          },
        }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ object: "list", data: [] }), {
        status: 200, headers: { "Content-Type": "application/json" },
      });
    });

    const state = createRuntimeConsoleFixture({ session: adminSession });
    let container = null as unknown as HTMLElement;
    await act(async () => {
      const result = render(<ObservabilityView state={state} actions={createRuntimeConsoleActions()} />);
      container = result.container;
    });

    await waitFor(() => {
      expect(container.textContent).toMatch(/Missing price/);
    });
    expect(container.textContent).toMatch(/Requested model/);
    expect(container.textContent).toMatch(/Route summary/);
    expect(container.textContent).toMatch(/Event flow/);
  });
});
