import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ConsoleShell } from "./AppShell";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../test/runtime-console-fixture";

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
