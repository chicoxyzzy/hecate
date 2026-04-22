import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useRuntimeConsole } from "./useRuntimeConsole";

describe("useRuntimeConsole", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    window.localStorage.clear();
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/healthz") {
        return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      }
      if (url === "/v1/whoami") {
        return jsonResponse({
          object: "session",
          data: {
            authenticated: false,
            invalid_token: false,
            role: "anonymous",
            source: "no_token",
          },
        });
      }
      if (url === "/v1/models") {
        return jsonResponse({
          object: "list",
          data: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } }],
        });
      }
      if (url === "/v1/provider-presets") {
        return jsonResponse({
          object: "provider_presets",
          data: [{ id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" }],
        });
      }
      if (url.startsWith("/admin/retention/runs")) {
        return unauthorizedResponse();
      }
      if (url.startsWith("/admin/accounts/summary")) {
        return unauthorizedResponse();
      }
      if (url.startsWith("/v1/chat/sessions")) {
        return unauthorizedResponse();
      }
      return unauthorizedResponse();
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("loads dashboard data and tolerates unauthorized admin endpoints", async () => {
    const { result } = renderHook(() => useRuntimeConsole());

    await waitFor(() => expect(result.current.state.loading).toBe(false));

    expect(result.current.state.health?.status).toBe("ok");
    expect(result.current.state.models).toHaveLength(1);
    expect(result.current.state.providerPresets).toHaveLength(1);
    expect(result.current.state.providers).toEqual([]);
    expect(result.current.state.budget).toBeNull();
    expect(result.current.state.controlPlane).toBeNull();
  });

  it("persists auth token changes to local storage", async () => {
    const { result } = renderHook(() => useRuntimeConsole());

    await waitFor(() => expect(result.current.state.loading).toBe(false));

    act(() => {
      result.current.actions.setAuthToken("tenant-secret");
    });

    await waitFor(() => {
      expect(window.localStorage.getItem("hecate.authToken")).toBe("tenant-secret");
    });
  });

  it("syncs the tenant field from the authenticated tenant session", async () => {
    fetchMock.mockImplementation(async (input, init) => {
      const url = String(input);
      const headers = new Headers(init?.headers);
      const authHeader = headers.get("Authorization");
      if (url === "/healthz") {
        return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      }
      if (url === "/v1/whoami") {
        if (authHeader === "Bearer tenant-secret") {
          return jsonResponse({
            object: "session",
            data: {
              authenticated: true,
              invalid_token: false,
              role: "tenant",
              tenant: "team-a",
              source: "control_plane_api_key",
              key_id: "team-a-dev",
            },
          });
        }
        return jsonResponse({
          object: "session",
          data: {
            authenticated: false,
            invalid_token: false,
            role: "anonymous",
            source: "no_token",
          },
        });
      }
      if (url === "/v1/models") {
        return jsonResponse({
          object: "list",
          data: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } }],
        });
      }
      if (url === "/v1/provider-presets") {
        return jsonResponse({
          object: "provider_presets",
          data: [{ id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" }],
        });
      }
      if (url.startsWith("/admin/retention/runs")) {
        return unauthorizedResponse();
      }
      if (url.startsWith("/admin/accounts/summary")) {
        return unauthorizedResponse();
      }
      if (url.startsWith("/v1/chat/sessions")) {
        return unauthorizedResponse();
      }
      return unauthorizedResponse();
    });

    const { result } = renderHook(() => useRuntimeConsole());

    await waitFor(() => expect(result.current.state.loading).toBe(false));

    act(() => {
      result.current.actions.setTenant("manual-tenant");
      result.current.actions.setAuthToken("tenant-secret");
    });

    await waitFor(() => {
      expect(result.current.state.session.kind).toBe("tenant");
      expect(result.current.state.session.tenant).toBe("team-a");
      expect(result.current.state.tenant).toBe("team-a");
    });
  });

  it("loads trace data after a successful chat request", async () => {
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/healthz") {
        return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      }
      if (url === "/v1/whoami") {
        return jsonResponse({
          object: "session",
          data: {
            authenticated: false,
            invalid_token: false,
            role: "anonymous",
            source: "no_token",
          },
        });
      }
      if (url === "/v1/models") {
        return jsonResponse({
          object: "list",
          data: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } }],
        });
      }
      if (url === "/v1/provider-presets") {
        return jsonResponse({
          object: "provider_presets",
          data: [{ id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" }],
        });
      }
      if (url.startsWith("/admin/retention/runs")) {
        return unauthorizedResponse();
      }
      if (url.startsWith("/admin/accounts/summary")) {
        return unauthorizedResponse();
      }
      if (url === "/v1/chat/sessions") {
        return jsonResponse({
          object: "chat_session",
          data: {
            id: "chat_123",
            title: "Say hello in one short sentence.",
            turns: [],
            created_at: "2026-04-21T00:00:00Z",
            updated_at: "2026-04-21T00:00:00Z",
          },
        });
      }
      if (url === "/v1/chat/sessions?limit=20") {
        return jsonResponse({
          object: "chat_sessions",
          data: [
            {
              id: "chat_123",
              title: "Say hello in one short sentence.",
              turn_count: 1,
              last_model: "gpt-4o-mini",
              last_provider: "openai",
              last_cost_usd: "0.000123",
              updated_at: "2026-04-21T00:00:01Z",
            },
          ],
        });
      }
      if (url === "/v1/chat/sessions/chat_123") {
        return jsonResponse({
          object: "chat_session",
          data: {
            id: "chat_123",
            title: "Say hello in one short sentence.",
            turns: [
              {
                id: "req-123",
                request_id: "req-123",
                user_message: { role: "user", content: "Say hello in one short sentence." },
                assistant_message: { role: "assistant", content: "Hello!" },
                provider: "openai",
                provider_kind: "cloud",
                model: "gpt-4o-mini",
                cost_micros_usd: 123,
                cost_usd: "0.000123",
                prompt_tokens: 10,
                completion_tokens: 2,
                total_tokens: 12,
                created_at: "2026-04-21T00:00:01Z",
              },
            ],
            created_at: "2026-04-21T00:00:00Z",
            updated_at: "2026-04-21T00:00:01Z",
          },
        });
      }
      if (url === "/v1/chat/completions") {
        return new Response(
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
              "X-Runtime-Attempts": "1",
              "X-Runtime-Retries": "0",
              "X-Runtime-Fallback-From": "",
              "X-Runtime-Cost-USD": "0.000123",
            },
          },
        );
      }
      if (url === "/v1/traces?request_id=req-123") {
        return jsonResponse({
          object: "trace",
          data: {
            request_id: "req-123",
            trace_id: "req-123",
            started_at: "2026-04-21T00:00:00Z",
            route: {
              final_provider: "openai",
              final_provider_kind: "cloud",
              final_model: "gpt-4o-mini",
              final_reason: "default_model",
              candidates: [
                {
                  provider: "openai",
                  provider_kind: "cloud",
                  model: "gpt-4o-mini",
                  outcome: "selected",
                },
              ],
            },
            spans: [
              {
                trace_id: "req-123",
                span_id: "span-1",
                name: "gateway.request",
                kind: "server",
                events: [{ name: "request.received", timestamp: "2026-04-21T00:00:00Z", attributes: { model: "gpt-4o-mini" } }],
              },
            ],
          },
        });
      }
      return unauthorizedResponse();
    });

    const { result } = renderHook(() => useRuntimeConsole());

    await waitFor(() => expect(result.current.state.loading).toBe(false));

    await act(async () => {
      await result.current.actions.submitChat({ preventDefault() {} } as never);
    });

    await waitFor(() => {
      expect(result.current.state.runtimeHeaders?.requestId).toBe("req-123");
      expect(result.current.state.traceSpans).toHaveLength(1);
      expect(result.current.state.traceRoute?.final_provider).toBe("openai");
      expect(result.current.state.traceSpans[0]?.events?.[0]?.name).toBe("request.received");
      expect(result.current.state.activeChatSession?.turns).toHaveLength(1);
    });
  });

  it("loads persisted retention history for admin sessions", async () => {
    fetchMock.mockImplementation(async (input, init) => {
      const url = String(input);
      const headers = new Headers(init?.headers);
      const authHeader = headers.get("Authorization");
      if (url === "/healthz") {
        return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      }
      if (url === "/v1/whoami") {
        if (authHeader === "Bearer admin-secret") {
          return jsonResponse({
            object: "session",
            data: {
              authenticated: true,
              invalid_token: false,
              role: "admin",
              name: "admin",
              source: "admin_token",
            },
          });
        }
        return jsonResponse({
          object: "session",
          data: {
            authenticated: false,
            invalid_token: false,
            role: "anonymous",
            source: "no_token",
          },
        });
      }
      if (url === "/v1/models") {
        return jsonResponse({
          object: "list",
          data: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } }],
        });
      }
      if (url === "/v1/provider-presets") {
        return jsonResponse({
          object: "provider_presets",
          data: [{ id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" }],
        });
      }
      if (url === "/admin/providers") {
        return jsonResponse({ object: "provider_status", data: [] });
      }
      if (url === "/admin/budget") {
        return unauthorizedResponse();
      }
      if (url === "/admin/control-plane") {
        return jsonResponse({ object: "control_plane", data: { backend: "file", tenants: [], api_keys: [], events: [] } });
      }
      if (url === "/admin/accounts/summary") {
        return jsonResponse({
          object: "account_summary",
          data: {
            account: {
              key: "global",
              scope: "global",
              backend: "memory",
              balance_source: "config",
              debited_micros_usd: 250000,
              debited_usd: "0.250000",
              credited_micros_usd: 1000000,
              credited_usd: "1.000000",
              balance_micros_usd: 750000,
              balance_usd: "0.750000",
              available_micros_usd: 750000,
              available_usd: "0.750000",
              enforced: true,
            },
            estimates: [
              {
                provider: "openai",
                provider_kind: "cloud",
                model: "gpt-4o-mini",
                priced: true,
                input_micros_usd_per_million_tokens: 150000,
                output_micros_usd_per_million_tokens: 600000,
                estimated_remaining_prompt_tokens: 5000000,
                estimated_remaining_output_tokens: 1250000,
              },
            ],
          },
        });
      }
      if (url === "/v1/chat/sessions?limit=20") {
        return jsonResponse({ object: "chat_sessions", data: [] });
      }
      if (url === "/admin/retention/runs?limit=10") {
        return jsonResponse({
          object: "retention_runs",
          data: [
            {
              started_at: "2026-04-22T10:00:00Z",
              finished_at: "2026-04-22T10:00:05Z",
              trigger: "manual",
              actor: "admin:req-1",
              request_id: "req-1",
              results: [{ name: "trace_snapshots", deleted: 12, max_age: "24h", max_count: 2000 }],
            },
          ],
        });
      }
      return unauthorizedResponse();
    });

    const { result } = renderHook(() => useRuntimeConsole());

    act(() => {
      result.current.actions.setAuthToken("admin-secret");
    });

    await waitFor(() => {
      expect(result.current.state.loading).toBe(false);
      expect(result.current.state.session.kind).toBe("admin");
      expect(result.current.state.retentionRuns).toHaveLength(1);
      expect(result.current.state.retentionLastRun?.request_id).toBe("req-1");
      expect(result.current.state.retentionLastRun?.actor).toBe("admin:req-1");
      expect(result.current.state.accountSummary?.estimates).toHaveLength(1);
    });
  });
});

function jsonResponse(payload: unknown): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function unauthorizedResponse(): Response {
  return new Response(JSON.stringify({ error: { message: "unauthorized" } }), {
    status: 401,
    headers: { "Content-Type": "application/json" },
  });
}
