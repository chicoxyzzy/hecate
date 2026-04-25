import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AdminView } from "./AdminView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

const adminSession = {
  kind: "admin" as const, label: "Admin", role: "admin", isAdmin: true, isAuthenticated: true,
  capabilities: [], name: "", tenant: "", source: "", keyID: "",
  allowedProviders: [], allowedModels: [],
};

function setup(stateOverrides = {}, actionOverrides = {}) {
  const state = createRuntimeConsoleFixture({ session: adminSession, ...stateOverrides });
  const actions = { ...createRuntimeConsoleActions(), ...actionOverrides };
  return { state, actions };
}

describe("AdminView tabs", () => {
  it("renders all five tabs", () => {
    const { state, actions } = setup();
    render(<AdminView state={state} actions={actions} />);
    for (const tab of ["keys", "tenants", "budget", "usage", "retention"]) {
      expect(screen.getAllByText(tab, { exact: false }).length).toBeGreaterThan(0);
    }
  });

  it("starts on the keys tab", () => {
    const { state, actions } = setup();
    render(<AdminView state={state} actions={actions} />);
    expect(screen.getAllByText(/API keys/i).length).toBeGreaterThan(0);
  });

  it("switches to tenants tab on click", () => {
    const { state, actions } = setup();
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "tenants" }));
    expect(screen.getByText(/New tenant/i)).toBeTruthy();
  });

  it("switches to retention tab on click", () => {
    const { state, actions } = setup();
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "retention" }));
    expect(screen.getByText(/Subsystems to prune/i)).toBeTruthy();
  });

  it("switches to budget tab and shows admin-required hint when no budget", () => {
    const { state, actions } = setup({ budget: null });
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "budget" }));
    expect(screen.getByText(/Admin access required/i)).toBeTruthy();
  });
});

describe("AdminView admin token panel", () => {
  it("shows 'not set' when authToken is empty", () => {
    const { state, actions } = setup({ authToken: "" });
    render(<AdminView state={state} actions={actions} />);
    expect(screen.getAllByText(/not set/i).length).toBeGreaterThan(0);
  });

  it("masks the token by default and reveals on click", () => {
    const { state, actions } = setup({ authToken: "super-secret-token-123" });
    render(<AdminView state={state} actions={actions} />);
    expect(screen.queryByText("super-secret-token-123")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /reveal/i }));
    expect(screen.getByText("super-secret-token-123")).toBeTruthy();
  });
});

describe("AdminView retention tab", () => {
  it("shows known subsystems as toggle chips", () => {
    const { state, actions } = setup();
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "retention" }));
    for (const sub of ["trace_snapshots", "budget_events", "audit_events", "exact_cache", "semantic_cache"]) {
      expect(screen.getByText(sub)).toBeTruthy();
    }
  });

  it("clicking a chip calls setRetentionSubsystems", () => {
    const setRetentionSubsystems = vi.fn();
    const { state, actions } = setup({}, { setRetentionSubsystems });
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "retention" }));
    fireEvent.click(screen.getByText("audit_events"));
    expect(setRetentionSubsystems).toHaveBeenCalledWith("audit_events");
  });

  it("'Run now' button triggers runRetention action", () => {
    const runRetention = vi.fn(async () => undefined);
    const { state, actions } = setup({}, { runRetention });
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "retention" }));
    fireEvent.click(screen.getByRole("button", { name: /Run now/i }));
    expect(runRetention).toHaveBeenCalled();
  });
});

describe("AdminView usage tab", () => {
  it("shows empty state when ledger is empty", () => {
    const { state, actions } = setup({ requestLedger: [] });
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "usage" }));
    expect(screen.getByText(/No usage events recorded yet/i)).toBeTruthy();
  });

  it("renders ledger entries when present", () => {
    const { state, actions } = setup({
      requestLedger: [
        { request_id: "req-1", timestamp: "2026-04-25T10:00:00Z", tenant: "team-a", model: "gpt-4o-mini", total_tokens: 42, amount_usd: "$0.001" } as any,
      ],
    });
    render(<AdminView state={state} actions={actions} />);
    fireEvent.click(screen.getByRole("button", { name: "usage" }));
    expect(screen.getByText("req-1")).toBeTruthy();
    expect(screen.getByText("team-a")).toBeTruthy();
  });
});
