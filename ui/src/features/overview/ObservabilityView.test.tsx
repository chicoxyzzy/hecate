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
});
