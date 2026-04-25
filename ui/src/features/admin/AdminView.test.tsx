import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
  const user = userEvent.setup();
  return { state, actions, user };
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

  it("switches to tenants tab on click", async () => {
    const { state, actions, user } = setup();
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "tenants" }));
    expect(await screen.findByText(/New tenant/i)).toBeTruthy();
  });

  it("switches to retention tab on click", async () => {
    const { state, actions, user } = setup();
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "retention" }));
    expect(await screen.findByText(/Subsystems to prune/i)).toBeTruthy();
  });

  it("switches to budget tab and shows admin-required hint when no budget", async () => {
    const { state, actions, user } = setup({ budget: null });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "budget" }));
    expect(await screen.findByText(/Admin access required/i)).toBeTruthy();
  });
});

describe("AdminView admin token panel", () => {
  it("shows 'not set' when authToken is empty", () => {
    const { state, actions } = setup({ authToken: "" });
    render(<AdminView state={state} actions={actions} />);
    expect(screen.getAllByText(/not set/i).length).toBeGreaterThan(0);
  });

  it("masks the token by default and reveals on click", async () => {
    const { state, actions, user } = setup({ authToken: "super-secret-token-123" });
    render(<AdminView state={state} actions={actions} />);
    expect(screen.queryByText("super-secret-token-123")).toBeNull();
    await user.click(screen.getByRole("button", { name: /reveal/i }));
    expect(await screen.findByText("super-secret-token-123")).toBeTruthy();
  });
});

describe("AdminView retention tab", () => {
  it("shows known subsystems as toggle chips", async () => {
    const { state, actions, user } = setup();
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "retention" }));
    for (const sub of ["trace_snapshots", "budget_events", "audit_events", "exact_cache", "semantic_cache"]) {
      expect(await screen.findByText(sub)).toBeTruthy();
    }
  });

  it("clicking a chip calls setRetentionSubsystems", async () => {
    const setRetentionSubsystems = vi.fn();
    const { state, actions, user } = setup({}, { setRetentionSubsystems });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "retention" }));
    await user.click(await screen.findByText("audit_events"));
    expect(setRetentionSubsystems).toHaveBeenCalledWith("audit_events");
  });

  it("'Run now' button triggers runRetention action", async () => {
    const runRetention = vi.fn(async () => undefined);
    const { state, actions, user } = setup({}, { runRetention });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "retention" }));
    await user.click(await screen.findByRole("button", { name: /Run now/i }));
    expect(runRetention).toHaveBeenCalled();
  });
});

describe("AdminView usage tab", () => {
  it("shows empty state when ledger is empty", async () => {
    const { state, actions, user } = setup({ requestLedger: [] });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "usage" }));
    expect(await screen.findByText(/No usage events recorded yet/i)).toBeTruthy();
  });

  it("renders ledger entries when present", async () => {
    const { state, actions, user } = setup({
      requestLedger: [
        { request_id: "req-1", timestamp: "2026-04-25T10:00:00Z", tenant: "team-a", model: "gpt-4o-mini", total_tokens: 42, amount_usd: "$0.001" } as any,
      ],
    });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "usage" }));
    expect(await screen.findByText("req-1")).toBeTruthy();
    expect(await screen.findByText("team-a")).toBeTruthy();
  });
});
