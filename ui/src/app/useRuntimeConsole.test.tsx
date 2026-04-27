import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useRuntimeConsole } from "./useRuntimeConsole";

describe("useRuntimeConsole", () => {
  const fetchMock = vi.fn<typeof fetch>();

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    window.localStorage.clear();
    // Seed an admin token so loadDashboard actually fires. The hook
    // skips the dashboard load when authToken is empty (TokenGate is
    // rendering anyway), but every test in this file is exercising the
    // post-auth dashboard path.
    window.localStorage.setItem("hecate.authToken", "test-bearer");
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

  it("does not fire any fetches when there is no auth token", async () => {
    // Override the beforeEach seed so we land on the empty-token branch.
    // TokenGate is what renders in this case; firing the dashboard would
    // 401-spam the eight admin/auth-required endpoints in the console.
    window.localStorage.removeItem("hecate.authToken");

    const { result } = renderHook(() => useRuntimeConsole());

    // Give the empty-token effect a tick to settle before asserting; it
    // should flip `loading` to false synchronously since there's nothing
    // to load. We assert via waitFor for resilience to scheduling.
    await waitFor(() => expect(result.current.state.loading).toBe(false));

    expect(fetchMock).not.toHaveBeenCalled();
    // Health stays at its initial null because /healthz never fired.
    expect(result.current.state.health).toBeNull();
  });

  it("loads dashboard data and tolerates unauthorized admin endpoints", async () => {
    // Use a tenant-authenticated session: the dashboard fires the
    // tenant-level fetches (models, providers, presets, sessions) but
    // gates admin-only fetches (budget, retention, accountSummary,
    // adminConfig, requestLedger) behind role=admin. With this gating
    // an anonymous bearer no longer fires those admin endpoints at
    // all (the previous "401 storm"), so this test simulates a
    // tenant whose admin endpoints are unauthorized — still tolerated
    // because they get skipped before the request goes out.
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/healthz") return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      if (url === "/v1/whoami") {
        return jsonResponse({
          object: "session",
          data: { authenticated: true, invalid_token: false, role: "tenant", tenant: "acme", source: "bearer" },
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
      // Tenant-level: providers + sessions return ok-but-empty.
      if (url.startsWith("/v1/providers")) return jsonResponse({ object: "list", data: [] });
      if (url.startsWith("/v1/chat/sessions")) return jsonResponse({ object: "chat_sessions", data: [] });
      // Admin-only paths: skipped before they fire, but mock 401 in
      // case they ever do — the resolvers fall back to defaults.
      return unauthorizedResponse();
    });

    const { result } = renderHook(() => useRuntimeConsole());

    await waitFor(() => expect(result.current.state.loading).toBe(false));

    expect(result.current.state.health?.status).toBe("ok");
    expect(result.current.state.models).toHaveLength(1);
    expect(result.current.state.providerPresets).toHaveLength(1);
    expect(result.current.state.providers).toEqual([]);
    expect(result.current.state.budget).toBeNull();
    expect(result.current.state.adminConfig).toBeNull();
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
              "X-Runtime-Route-Reason": "requested_model",
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
              final_reason: "provider_default_model",
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
      expect(result.current.state.activeChatSession?.turns).toHaveLength(1);
    });
  });

  it("surfaces a chat error in the toaster (not just inline) so it's consistent with other admin notices", async () => {
    // Without the toast wiring, a chat failure only shows in the
    // inline chat banner — easy to miss if the operator's eyes are on
    // the sidebar/admin panel. This test pins the toast surface so a
    // refactor doesn't silently drop it.
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/healthz") return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      if (url === "/v1/whoami") {
        return jsonResponse({
          object: "session",
          data: { authenticated: false, invalid_token: false, role: "anonymous", source: "no_token" },
        });
      }
      if (url === "/v1/models") return jsonResponse({ object: "list", data: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud" } }] });
      if (url === "/v1/provider-presets") return jsonResponse({ object: "provider_presets", data: [{ id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" }] });
      if (url === "/v1/chat/sessions") {
        return jsonResponse({ object: "chat_session", data: { id: "chat_err", title: "x", turns: [], created_at: "2026-04-21T00:00:00Z", updated_at: "2026-04-21T00:00:00Z" } });
      }
      if (url === "/v1/chat/completions") {
        // Backend now strips "client error: " before serializing —
        // simulate the cleaned shape we expect on the wire.
        return new Response(
          JSON.stringify({ error: { message: "api key is required for cloud provider anthropic when stub mode is disabled" } }),
          { status: 400, headers: { "Content-Type": "application/json" } },
        );
      }
      return unauthorizedResponse();
    });

    const { result } = renderHook(() => useRuntimeConsole());
    await waitFor(() => expect(result.current.state.loading).toBe(false));

    await act(async () => {
      await result.current.actions.submitChat({ preventDefault() {} } as never);
    });

    // Inline error stays for chat-context.
    await waitFor(() => expect(result.current.state.chatError).toContain("api key is required"));
    // Toast mirrors it so chat failures are visible from anywhere on
    // the page. Same kind ("error") as budget/retention/pricebook errors.
    expect(result.current.state.notice?.kind).toBe("error");
    expect(result.current.state.notice?.message).toContain("api key is required");
    // Critically: no leaked classification prefix from the backend.
    expect(result.current.state.notice?.message).not.toMatch(/^client error: /i);
    expect(result.current.state.chatError).not.toMatch(/^client error: /i);
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
        return jsonResponse({ object: "control_plane", data: { backend: "memory", tenants: [], api_keys: [], events: [] } });
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


  it("resets an unavailable preset example model for a configured provider", async () => {
    fetchMock.mockImplementation(async (input) => {
      const url = String(input);
      if (url === "/healthz") {
        return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
      }
      if (url === "/v1/whoami") {
        // Authenticated tenant — needed because dashboard now gates
        // /v1/models, /v1/provider-presets, /admin/providers behind
        // an authenticated session (the 401-storm fix).
        return jsonResponse({
          object: "session",
          data: {
            authenticated: true,
            invalid_token: false,
            role: "tenant",
            tenant: "acme",
            source: "bearer",
          },
        });
      }
      if (url === "/v1/models") {
        return jsonResponse({
          object: "list",
          data: [{ id: "llama3.1:8b", owned_by: "ollama", metadata: { provider: "ollama", provider_kind: "local", default: true } }],
        });
      }
      if (url === "/v1/provider-presets") {
        return jsonResponse({
          object: "provider_presets",
          data: [
            {
              id: "ollama",
              name: "Ollama",
              kind: "local",
              protocol: "openai",
              base_url: "http://127.0.0.1:11434/v1",
            },
          ],
        });
      }
      if (url === "/v1/providers" || url === "/admin/providers") {
        return jsonResponse({
          object: "provider_status",
          data: [{ name: "ollama", kind: "local", healthy: true, status: "healthy", default_model: "llama3.1:8b", models: ["llama3.1:8b"] }],
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
      result.current.actions.setProviderFilter("ollama");
    });

    await waitFor(() => expect(result.current.state.model).toBe("llama3.1:8b"));

    act(() => {
      result.current.actions.setModel("qwen2.5:7b");
    });

    await waitFor(() => expect(result.current.state.model).toBe("llama3.1:8b"));
  });
  // ─── applyPricebookImport: notice text per outcome ────────────────────────
  //
  // The toast wording on the dashboard's notice banner is the
  // operator's primary feedback for a bulk import. Three branches:
  //   * all rows succeeded → "Imported N rows."
  //   * mixed              → "Imported N, M failed."
  //   * all rows failed    → "Import failed for N rows."
  // These tests pin the wording so a refactor doesn't accidentally
  // collapse mixed/failed into a generic "applied" success notice.
  describe("applyPricebookImport notice variants", () => {
    function mockApplyResponse(data: Record<string, unknown>) {
      fetchMock.mockImplementation(async (input) => {
        const url = String(input);
        if (url === "/admin/control-plane/pricebook/import/apply") {
          return jsonResponse({ object: "control_plane_pricebook_import_diff", data });
        }
        if (url === "/healthz") return jsonResponse({ status: "ok", time: "2026-04-20T00:00:00Z" });
        if (url === "/v1/whoami") {
          return jsonResponse({
            object: "session",
            data: { authenticated: true, invalid_token: false, role: "admin", source: "bearer" },
          });
        }
        if (url === "/v1/models") return jsonResponse({ object: "list", data: [] });
        if (url === "/v1/provider-presets") return jsonResponse({ object: "provider_presets", data: [] });
        return unauthorizedResponse();
      });
    }

    it("success-only: notice reads 'Imported N rows.'", async () => {
      mockApplyResponse({
        fetched_at: "2026", unchanged: 0,
        applied: [
          { provider: "openai", model: "a", input_micros_usd_per_million_tokens: 1, output_micros_usd_per_million_tokens: 2, cached_input_micros_usd_per_million_tokens: 0, source: "imported" },
          { provider: "openai", model: "b", input_micros_usd_per_million_tokens: 1, output_micros_usd_per_million_tokens: 2, cached_input_micros_usd_per_million_tokens: 0, source: "imported" },
        ],
      });
      const { result } = renderHook(() => useRuntimeConsole());
      await waitFor(() => expect(result.current.state.loading).toBe(false));

      await act(async () => {
        await result.current.actions.applyPricebookImport(["openai/a", "openai/b"]);
      });
      await waitFor(() => expect(result.current.state.notice).not.toBeNull());
      expect(result.current.state.notice?.kind).toBe("success");
      expect(result.current.state.notice?.message).toBe("Imported 2 rows.");
    });

    it("mixed: notice reads 'Imported N, M failed.' and is an error notice", async () => {
      mockApplyResponse({
        fetched_at: "2026", unchanged: 0,
        applied: [
          { provider: "openai", model: "good", input_micros_usd_per_million_tokens: 1, output_micros_usd_per_million_tokens: 2, cached_input_micros_usd_per_million_tokens: 0, source: "imported" },
        ],
        failed: [
          { entry: { provider: "openai", model: "bad", input_micros_usd_per_million_tokens: 1, output_micros_usd_per_million_tokens: 2, cached_input_micros_usd_per_million_tokens: 0, source: "imported" }, error: "boom" },
        ],
      });
      const { result } = renderHook(() => useRuntimeConsole());
      await waitFor(() => expect(result.current.state.loading).toBe(false));

      await act(async () => {
        await result.current.actions.applyPricebookImport(["openai/good", "openai/bad"]);
      });
      await waitFor(() => expect(result.current.state.notice).not.toBeNull());
      expect(result.current.state.notice?.kind).toBe("error");
      expect(result.current.state.notice?.message).toBe("Imported 1, 1 failed.");
    });

    it("all-failed: notice reads 'Import failed for N rows.'", async () => {
      mockApplyResponse({
        fetched_at: "2026", unchanged: 0,
        applied: [],
        failed: [
          { entry: { provider: "openai", model: "x", input_micros_usd_per_million_tokens: 1, output_micros_usd_per_million_tokens: 2, cached_input_micros_usd_per_million_tokens: 0, source: "imported" }, error: "e1" },
          { entry: { provider: "openai", model: "y", input_micros_usd_per_million_tokens: 1, output_micros_usd_per_million_tokens: 2, cached_input_micros_usd_per_million_tokens: 0, source: "imported" }, error: "e2" },
        ],
      });
      const { result } = renderHook(() => useRuntimeConsole());
      await waitFor(() => expect(result.current.state.loading).toBe(false));

      await act(async () => {
        await result.current.actions.applyPricebookImport(["openai/x", "openai/y"]);
      });
      await waitFor(() => expect(result.current.state.notice).not.toBeNull());
      expect(result.current.state.notice?.kind).toBe("error");
      expect(result.current.state.notice?.message).toBe("Import failed for 2 rows.");
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
