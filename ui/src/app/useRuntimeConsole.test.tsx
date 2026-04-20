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
