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
  it("renders the operator tab labels", () => {
    const { state, actions } = setup();
    render(<AdminView state={state} actions={actions} />);
    for (const tab of ["Keys", "Tenants", "Balances", "Usage", "Pricing", "Policy", "Retention", "Clients"]) {
      expect(screen.getByRole("button", { name: tab })).toBeTruthy();
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
    await user.click(screen.getByRole("button", { name: "Tenants" }));
    expect(await screen.findByText(/New tenant/i)).toBeTruthy();
  });

  it("switches to retention tab on click", async () => {
    const { state, actions, user } = setup();
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Retention" }));
    expect(await screen.findByText(/Subsystems to prune/i)).toBeTruthy();
  });

  it("switches to budget tab and shows admin-required hint when no budget", async () => {
    const { state, actions, user } = setup({ budget: null });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Balances" }));
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
    await user.click(screen.getByRole("button", { name: "Retention" }));
    for (const sub of ["trace_snapshots", "budget_events", "audit_events", "exact_cache", "semantic_cache"]) {
      expect(await screen.findByText(sub)).toBeTruthy();
    }
  });

  it("clicking a chip calls setRetentionSubsystems", async () => {
    const setRetentionSubsystems = vi.fn();
    const { state, actions, user } = setup({}, { setRetentionSubsystems });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Retention" }));
    await user.click(await screen.findByText("audit_events"));
    expect(setRetentionSubsystems).toHaveBeenCalledWith("audit_events");
  });

  it("'Run now' button triggers runRetention action", async () => {
    const runRetention = vi.fn(async () => undefined);
    const { state, actions, user } = setup({}, { runRetention });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Retention" }));
    await user.click(await screen.findByRole("button", { name: /Run now/i }));
    expect(runRetention).toHaveBeenCalled();
  });
});

describe("AdminView policy tab", () => {
  function adminConfigWith(rules: unknown[]) {
    return {
      backend: "memory",
      tenants: [
        { id: "team-a", name: "team-a", enabled: true, allowed_providers: [], allowed_models: [] },
      ],
      api_keys: [],
      providers: [],
      pricebook: [],
      policy_rules: rules,
      events: [],
    } as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"];
  }

  it("renders the empty state when no rules are configured", async () => {
    const { state, actions, user } = setup({ adminConfig: adminConfigWith([]) });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));
    expect(await screen.findByText(/No policy rules/i)).toBeTruthy();
  });

  it("lists existing rules with action badge + match summary + effect", async () => {
    const { state, actions, user } = setup({
      adminConfig: adminConfigWith([
        {
          id: "deny-cloud",
          action: "deny",
          reason: "team-a is local-only",
          tenants: ["team-a"],
          provider_kinds: ["cloud"],
        },
        {
          id: "downgrade-team-b",
          action: "rewrite_model",
          tenants: ["team-b"],
          models: ["gpt-4o"],
          rewrite_model_to: "gpt-4o-mini",
        },
      ]),
    });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));

    // Each row's id renders as mono.
    expect(await screen.findByText("deny-cloud")).toBeTruthy();
    expect(screen.getByText("downgrade-team-b")).toBeTruthy();

    // Action badges (lowercase labels match the badge text).
    expect(screen.getByText("deny")).toBeTruthy();
    expect(screen.getByText("rewrite")).toBeTruthy();

    // Match summary picks up the populated dimensions.
    expect(screen.getByText(/tenant: team-a · kind: cloud/)).toBeTruthy();
    expect(screen.getByText(/tenant: team-b · model: gpt-4o/)).toBeTruthy();

    // Effect column shows the deny reason and the rewrite arrow.
    expect(screen.getByText("team-a is local-only")).toBeTruthy();
    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
  });

  it("'New rule' opens the SlideOver with the empty form", async () => {
    const { state, actions, user } = setup({ adminConfig: adminConfigWith([]) });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));
    await user.click(screen.getByRole("button", { name: /New rule/i }));
    expect(await screen.findByRole("dialog", { name: /New policy rule/i })).toBeTruthy();
    // The deny radio is selected by default — the reason field shows.
    expect(screen.getByText(/REASON \(shown in the 403/i)).toBeTruthy();
  });

  it("switching to rewrite_model swaps the reason input for the target-model input", async () => {
    const { state, actions, user } = setup({ adminConfig: adminConfigWith([]) });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));
    await user.click(screen.getByRole("button", { name: /New rule/i }));
    // Click the rewrite_model radio.
    await user.click(screen.getByLabelText("rewrite_model"));
    expect(screen.getByText(/REWRITE TO MODEL/i)).toBeTruthy();
    // Save is disabled while target model is empty even if id is set.
    const id = screen.getByPlaceholderText(/deny-cloud-for-team-a/i);
    await user.type(id, "downgrade-x");
    expect((screen.getByRole("button", { name: /Save rule/i }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("Save calls upsertPolicyRule with the trimmed payload", async () => {
    const upsertPolicyRule = vi.fn(async () => undefined);
    const { state, actions, user } = setup(
      { adminConfig: adminConfigWith([]) },
      { upsertPolicyRule },
    );
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));
    await user.click(screen.getByRole("button", { name: /New rule/i }));
    await user.type(screen.getByPlaceholderText(/deny-cloud-for-team-a/i), "deny-test");
    await user.type(screen.getByPlaceholderText(/team-a is local-only/i), "test reason");
    await user.click(screen.getByRole("button", { name: /Save rule/i }));
    expect(upsertPolicyRule).toHaveBeenCalledWith(expect.objectContaining({
      id: "deny-test",
      action: "deny",
      reason: "test reason",
    }));
  });

  it("clicking a row opens the edit form prefilled with that rule", async () => {
    const { state, actions, user } = setup({
      adminConfig: adminConfigWith([
        { id: "deny-cloud", action: "deny", reason: "test", provider_kinds: ["cloud"] },
      ]),
    });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));
    await user.click(screen.getByText("deny-cloud"));
    expect(await screen.findByRole("dialog", { name: /Edit policy rule/i })).toBeTruthy();
    // The id field should have the existing id pre-filled.
    const idInput = screen.getByPlaceholderText(/deny-cloud-for-team-a/i) as HTMLInputElement;
    expect(idInput.value).toBe("deny-cloud");
  });

  it("Delete opens a confirm modal that calls deletePolicyRule with the id", async () => {
    const deletePolicyRule = vi.fn(async () => undefined);
    const { state, actions, user } = setup(
      {
        adminConfig: adminConfigWith([
          { id: "deny-cloud", action: "deny" },
        ]),
      },
      { deletePolicyRule },
    );
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Policy" }));
    await user.click(screen.getByRole("button", { name: /Delete rule deny-cloud/i }));
    const dialog = await screen.findByRole("dialog", { name: /Delete policy rule/i });
    expect(dialog).toBeTruthy();
    await user.click(screen.getByRole("button", { name: /^Delete rule$/i }));
    expect(deletePolicyRule).toHaveBeenCalledWith("deny-cloud");
  });
});

describe("AdminView usage tab", () => {
  it("shows empty state when ledger is empty", async () => {
    const { state, actions, user } = setup({ requestLedger: [] });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Usage" }));
    expect(await screen.findByText(/No usage events recorded yet/i)).toBeTruthy();
  });

  it("renders ledger entries when present", async () => {
    const { state, actions, user } = setup({
      requestLedger: [
        { request_id: "req-1", timestamp: "2026-04-25T10:00:00Z", tenant: "team-a", model: "gpt-4o-mini", total_tokens: 42, amount_usd: "$0.001" } as any,
      ],
    });
    render(<AdminView state={state} actions={actions} />);
    await user.click(screen.getByRole("button", { name: "Usage" }));
    expect(await screen.findByText("req-1")).toBeTruthy();
    expect(await screen.findByText("team-a")).toBeTruthy();
  });
});
