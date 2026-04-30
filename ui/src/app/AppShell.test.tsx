import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ConsoleShell, getAvailableWorkspaces } from "./AppShell";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../test/runtime-console-fixture";

// Role-aware workspace nav. Tenants see chats / runs / overview;
// admins see the full set including providers + admin; anonymous
// fallthrough sees just [chats] so the activity bar isn't empty if
// a session somehow slips past the TokenGate.
describe("getAvailableWorkspaces", () => {
  it("admin lineup is chats / providers / runs / overview / admin", () => {
    const ws = getAvailableWorkspaces({ isAdmin: true, isAuthenticated: true });
    expect(ws.map(w => w.id)).toEqual(["chats", "providers", "runs", "overview", "admin"]);
    // Shortcuts are positional 1..N.
    expect(ws.map(w => w.shortcut)).toEqual(["1", "2", "3", "4", "5"]);
  });

  it("tenant lineup drops Providers and Admin", () => {
    const ws = getAvailableWorkspaces({ isAdmin: false, isAuthenticated: true });
    expect(ws.map(w => w.id)).toEqual(["chats", "runs", "overview"]);
  });

  it("anonymous fallthrough is chats only", () => {
    const ws = getAvailableWorkspaces({ isAdmin: false, isAuthenticated: false });
    expect(ws.map(w => w.id)).toEqual(["chats"]);
  });

  it("legacy boolean signature still works (maps true → admin lineup)", () => {
    const ws = getAvailableWorkspaces(true);
    expect(ws.map(w => w.id)).toContain("admin");
    expect(ws.map(w => w.id)).toContain("providers");
  });
});

// TokenGate is a private helper inside AppShell. We exercise it indirectly
// through ConsoleShell: an empty `authToken` triggers the empty-token
// branch (default copy), and `session.kind === "invalid"` triggers the
// rejected branch (inline banner). Neither branch renders the workspace
// views, so no fetch stubbing is needed for these tests.

const invalidSession = {
  kind: "invalid" as const,
  label: "Invalid token",
  role: "anonymous",
  isAdmin: false,
  isAuthenticated: false,
  capabilities: [],
  name: "",
  tenant: "",
  source: "",
  keyID: "",
  allowedProviders: [],
  allowedModels: [],
};

function renderEmptyTokenGate(overrides: Partial<ReturnType<typeof createRuntimeConsoleActions>> = {}) {
  const actions = { ...createRuntimeConsoleActions(), ...overrides };
  const state = createRuntimeConsoleFixture({ authToken: "" });
  render(
    <ConsoleShell
      activeWorkspace="overview"
      onSelectWorkspace={() => {}}
      state={state}
      actions={actions}
    />,
  );
  return { actions };
}

function renderRejectedTokenGate(overrides: Partial<ReturnType<typeof createRuntimeConsoleActions>> = {}) {
  const actions = { ...createRuntimeConsoleActions(), ...overrides };
  const state = createRuntimeConsoleFixture({
    authToken: "stale-token",
    session: invalidSession,
  });
  render(
    <ConsoleShell
      activeWorkspace="overview"
      onSelectWorkspace={() => {}}
      state={state}
      actions={actions}
    />,
  );
  return { actions };
}

describe("TokenGate (empty token)", () => {
  it("renders the heading, description, and the bearer-token input", () => {
    renderEmptyTokenGate();

    expect(screen.getByRole("heading", { name: /admin token required/i })).toBeInTheDocument();
    expect(screen.getByText(/auto-generates an admin bearer token/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/admin bearer token/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^connect$/i })).toBeInTheDocument();
    // No rejected-banner copy in the empty-token branch.
    expect(screen.queryByText(/saved token was rejected/i)).toBeNull();
  });

  it("submits a trimmed token via setAuthToken", () => {
    const setAuthToken = vi.fn();
    renderEmptyTokenGate({ setAuthToken });

    const input = screen.getByLabelText(/admin bearer token/i);
    fireEvent.change(input, { target: { value: "  fresh-token  " } });
    fireEvent.click(screen.getByRole("button", { name: /^connect$/i }));

    expect(setAuthToken).toHaveBeenCalledWith("fresh-token");
  });

  it("shows an inline error and skips setAuthToken when submitted empty", () => {
    const setAuthToken = vi.fn();
    renderEmptyTokenGate({ setAuthToken });

    fireEvent.click(screen.getByRole("button", { name: /^connect$/i }));

    expect(screen.getByText(/paste the token from your gateway logs/i)).toBeInTheDocument();
    expect(setAuthToken).not.toHaveBeenCalled();
  });

  it("treats whitespace-only input as empty", () => {
    const setAuthToken = vi.fn();
    renderEmptyTokenGate({ setAuthToken });

    const input = screen.getByLabelText(/admin bearer token/i);
    fireEvent.change(input, { target: { value: "   \t  " } });
    fireEvent.click(screen.getByRole("button", { name: /^connect$/i }));

    expect(screen.getByText(/paste the token from your gateway logs/i)).toBeInTheDocument();
    expect(setAuthToken).not.toHaveBeenCalled();
  });

  it("clears the local error message as soon as the operator types again", () => {
    renderEmptyTokenGate();

    fireEvent.click(screen.getByRole("button", { name: /^connect$/i }));
    expect(screen.getByText(/paste the token from your gateway logs/i)).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText(/admin bearer token/i), { target: { value: "x" } });
    expect(screen.queryByText(/paste the token from your gateway logs/i)).toBeNull();
  });

  it("submits via the form (Enter inside the input) without clicking the button", () => {
    const setAuthToken = vi.fn();
    renderEmptyTokenGate({ setAuthToken });

    const input = screen.getByLabelText(/admin bearer token/i);
    fireEvent.change(input, { target: { value: "enter-token" } });
    // Submitting the form is equivalent to pressing Enter inside the input.
    fireEvent.submit(input.closest("form")!);

    expect(setAuthToken).toHaveBeenCalledWith("enter-token");
  });
});

describe("TokenGate (rejected token)", () => {
  it("renders the rejected banner alongside the gate", () => {
    renderRejectedTokenGate();

    expect(screen.getByRole("heading", { name: /admin token required/i })).toBeInTheDocument();
    expect(screen.getByText(/saved token was rejected by the gateway/i)).toBeInTheDocument();
  });

  it("keeps the rejected banner visible while the operator types a replacement", () => {
    // The rejected banner reflects server state, not local validation, so
    // it must persist until the dashboard reloads with a fresh session.
    // Local-validation errors clear on input; the rejected banner does not.
    renderRejectedTokenGate();

    fireEvent.change(screen.getByLabelText(/admin bearer token/i), { target: { value: "new-token" } });
    expect(screen.getByText(/saved token was rejected by the gateway/i)).toBeInTheDocument();
  });

  it("submits the new token via setAuthToken on Connect", () => {
    const setAuthToken = vi.fn();
    renderRejectedTokenGate({ setAuthToken });

    fireEvent.change(screen.getByLabelText(/admin bearer token/i), { target: { value: "replacement" } });
    fireEvent.click(screen.getByRole("button", { name: /^connect$/i }));

    expect(setAuthToken).toHaveBeenCalledWith("replacement");
  });

  it("local empty-input error preempts the rejected banner when both would apply", () => {
    // If the operator clicks Connect with an empty input on the rejected
    // gate, the local validation message wins (it's the more actionable
    // error for that moment). We assert the local copy appears and the
    // rejected copy is suppressed so we don't double up red banners.
    const setAuthToken = vi.fn();
    renderRejectedTokenGate({ setAuthToken });

    fireEvent.click(screen.getByRole("button", { name: /^connect$/i }));

    expect(screen.getByText(/paste the token from your gateway logs/i)).toBeInTheDocument();
    expect(screen.queryByText(/saved token was rejected by the gateway/i)).toBeNull();
    expect(setAuthToken).not.toHaveBeenCalled();
  });
});

// Status bar version render — guards the conditional that hides the
// version chip when /healthz didn't include one (older gateway, or the
// field genuinely missing). The workspace branch renders the embedded
// views, which fan out fetches on mount; we stub fetch globally here so
// those calls don't blow up under jsdom.
describe("status bar version chip", () => {
  const adminSession = {
    kind: "admin" as const,
    label: "Admin",
    role: "admin",
    isAdmin: true,
    isAuthenticated: true,
    capabilities: [],
    name: "",
    tenant: "",
    source: "",
    keyID: "",
    allowedProviders: [],
    allowedModels: [],
  };

  function renderWorkspace(healthOverrides: Record<string, unknown> | null) {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        new Response(JSON.stringify({ object: "list", data: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const state = createRuntimeConsoleFixture({
      authToken: "tok",
      session: adminSession,
      // null clears the fixture's default { status: "ok", time: ... };
      // anything else replaces the whole object so the render branch
      // sees the version we feed it (or its absence).
      health: healthOverrides as never,
    });
    render(
      <ConsoleShell
        activeWorkspace="overview"
        onSelectWorkspace={() => {}}
        state={state}
        actions={createRuntimeConsoleActions()}
      />,
    );
  }

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("renders the version when /healthz returned one", () => {
    const sampleVersion = "test-build-abc123";
    renderWorkspace({ status: "ok", time: "2026-04-25T00:00:00Z", version: sampleVersion });

    // Version sits inside the status bar; scope the query so a stray
    // version string elsewhere on screen wouldn't false-positive the
    // test.
    const statusbar = document.querySelector(".hecate-statusbar");
    expect(statusbar).not.toBeNull();
    expect(statusbar!.textContent).toContain(sampleVersion);
  });

  it("hides the version chip when /healthz did not include one", () => {
    renderWorkspace({ status: "ok", time: "2026-04-25T00:00:00Z" });

    const statusbar = document.querySelector(".hecate-statusbar");
    expect(statusbar).not.toBeNull();
    // Status bar renders brand · session · configured · models (3
    // separators); the version chip stays out — that would bring it
    // to 4. Assert by counting separators.
    const sepCount = statusbar!.querySelectorAll(".hecate-statusbar__sep").length;
    expect(sepCount).toBe(3);
  });
});

