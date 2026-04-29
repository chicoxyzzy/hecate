import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ProvidersView } from "./ProvidersView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";
import type { ConfiguredProviderRecord, ProviderPresetRecord, ProviderRecord } from "../../types/runtime";

const presets: ProviderPresetRecord[] = [
  { id: "anthropic", name: "Anthropic", kind: "cloud", protocol: "openai", base_url: "https://api.anthropic.com/v1", description: "" },
  { id: "llamacpp",  name: "llama.cpp", kind: "local", protocol: "openai", base_url: "http://127.0.0.1:8080/v1", description: "" },
  { id: "localai",   name: "LocalAI",   kind: "local", protocol: "openai", base_url: "http://127.0.0.1:8080/v1", description: "" },
  { id: "ollama",    name: "Ollama",    kind: "local", protocol: "openai", base_url: "http://127.0.0.1:11434/v1", description: "" },
];

function makeConfigured(id: string, overrides: Partial<ConfiguredProviderRecord> = {}): ConfiguredProviderRecord {
  const preset = presets.find(p => p.id === id);
  return {
    id, name: id,
    kind: preset?.kind ?? "cloud",
    protocol: preset?.protocol ?? "openai",
    base_url: preset?.base_url ?? "",
    enabled: true,
    credential_configured: false,
    ...overrides,
  };
}

function makeStatus(name: string, overrides: Partial<ProviderRecord> = {}): ProviderRecord {
  return {
    name,
    kind: "local",
    healthy: true,
    status: "healthy",
    models: [],
    ...overrides,
  };
}

const adminSession = {
  kind: "admin" as const, label: "Admin", role: "admin", isAdmin: true, isAuthenticated: true,
  capabilities: [], name: "", tenant: "", source: "", keyID: "",
  allowedProviders: [], allowedModels: [],
};

function emptyAdminConfig() {
  return {
    backend: "memory",
    tenants: [], api_keys: [], policy_rules: [], pricebook: [], events: [],
    providers: [] as ConfiguredProviderRecord[],
  };
}

describe("ProvidersView conflict resolution", () => {
  it("shows provider health diagnostics and last errors", async () => {
    const state = createRuntimeConsoleFixture({
      session: adminSession,
      providerPresets: presets,
      adminConfig: {
        ...emptyAdminConfig(),
        providers: [makeConfigured("ollama", { enabled: true })],
      },
      providers: [
        makeStatus("ollama", {
          healthy: false,
          status: "open",
          last_error: "connect: connection refused",
          last_error_class: "rate_limit",
          model_count: 1,
          credential_state: "not_required",
          routing_ready: false,
          routing_blocked_reason: "provider_rate_limited",
          discovery_source: "live",
          last_checked_at: "2026-04-29T10:00:00Z",
          open_until: "2026-04-29T10:01:00Z",
          last_latency_ms: 980,
          consecutive_failures: 1,
          total_failures: 4,
          rate_limits: 2,
        }),
      ],
    });

    render(<ProvidersView state={state} actions={createRuntimeConsoleActions()} />);
    expect(screen.getByText("connect: connection refused")).toBeTruthy();
    expect(screen.getByText("route blocked")).toBeTruthy();
    expect(screen.getByText("Cooling down after upstream 429")).toBeTruthy();

    const user = userEvent.setup();
    await user.click(screen.getByText("Ollama"));
    expect(screen.getByText("Diagnostics")).toBeTruthy();
    expect(screen.getAllByText("Not required").length).toBeGreaterThan(0);
    expect(screen.getByText(/discovery:/)).toBeTruthy();
    expect(screen.getByText(/error class:/)).toBeTruthy();
    expect(screen.getByText(/open until:/)).toBeTruthy();
    expect(screen.getByText(/health:/)).toBeTruthy();
    expect(screen.getByText(/totals:/)).toBeTruthy();
    expect(screen.getByText(/checked:/)).toBeTruthy();
  });

  it("reflects the resolved enabled state from /admin/control-plane (not runtime status)", () => {
    // Backend has reconciled: llamacpp enabled, localai disabled (they share 127.0.0.1:8080).
    // Runtime status would report both as healthy — the UI must NOT use that for the toggle.
    const state = createRuntimeConsoleFixture({
      session: adminSession,
      providerPresets: presets,
      adminConfig: {
        ...emptyAdminConfig(),
        providers: [
          makeConfigured("anthropic"),
          makeConfigured("llamacpp", { enabled: true }),
          makeConfigured("localai",  { enabled: false }),
          makeConfigured("ollama",   { enabled: true }),
        ],
      },
      providers: [makeStatus("llamacpp"), makeStatus("localai"), makeStatus("ollama")],
    });

    render(<ProvidersView state={state} actions={createRuntimeConsoleActions()} />);

    expect(screen.getByRole("switch", { name: "Enable llama.cpp" }).getAttribute("aria-checked")).toBe("true");
    expect(screen.getByRole("switch", { name: "Enable LocalAI"   }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("switch", { name: "Enable Ollama"    }).getAttribute("aria-checked")).toBe("true");
  });

  it("optimistically disables conflicting providers when the user enables one", async () => {
    // Use a deferred promise so we can observe the optimistic UI state before
    // the action resolves and clears the pending toggles.
    let resolveAction: (() => void) | null = null;
    const setProviderEnabled = vi.fn(() => new Promise<void>(r => { resolveAction = () => r(); }));
    const actions = { ...createRuntimeConsoleActions(), setProviderEnabled };

    const state = createRuntimeConsoleFixture({
      session: adminSession,
      providerPresets: presets,
      adminConfig: {
        ...emptyAdminConfig(),
        providers: [
          makeConfigured("llamacpp", { enabled: true }),
          makeConfigured("localai",  { enabled: false }),
        ],
      },
      providers: [makeStatus("llamacpp"), makeStatus("localai")],
    });

    render(<ProvidersView state={state} actions={actions} />);

    const localaiToggle = screen.getByRole("switch", { name: "Enable LocalAI" });
    expect(localaiToggle.getAttribute("aria-checked")).toBe("false");

    const user = userEvent.setup();
    await user.click(localaiToggle);

    // The action's promise has not resolved — optimistic UI state is visible.
    expect(setProviderEnabled).toHaveBeenCalledWith("localai", true);
    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "Enable llama.cpp" }).getAttribute("aria-checked")).toBe("false");
      expect(screen.getByRole("switch", { name: "Enable LocalAI"   }).getAttribute("aria-checked")).toBe("true");
    });

    // Resolve and let the .then() that clears pending toggles run, wrapped in act
    // so React batches the resulting state update without a warning.
    await act(async () => { resolveAction!(); });
  });

  it("does not flip conflicting providers when the user disables one", async () => {
    let resolveAction: (() => void) | null = null;
    const setProviderEnabled = vi.fn(() => new Promise<void>(r => { resolveAction = () => r(); }));
    const actions = { ...createRuntimeConsoleActions(), setProviderEnabled };

    const state = createRuntimeConsoleFixture({
      session: adminSession,
      providerPresets: presets,
      adminConfig: {
        ...emptyAdminConfig(),
        providers: [
          makeConfigured("llamacpp", { enabled: true }),
          makeConfigured("localai",  { enabled: false }),
        ],
      },
      providers: [makeStatus("llamacpp"), makeStatus("localai")],
    });

    render(<ProvidersView state={state} actions={actions} />);

    const user = userEvent.setup();
    await user.click(screen.getByRole("switch", { name: "Enable llama.cpp" }));

    expect(setProviderEnabled).toHaveBeenCalledWith("llamacpp", false);
    // llamacpp now off, localai stays off (backend hasn't re-resolved).
    await waitFor(() => {
      expect(screen.getByRole("switch", { name: "Enable llama.cpp" }).getAttribute("aria-checked")).toBe("false");
      expect(screen.getByRole("switch", { name: "Enable LocalAI"   }).getAttribute("aria-checked")).toBe("false");
    });

    await act(async () => { resolveAction!(); });
  });

  it("defaults all conflicting providers to off when none are explicitly configured", async () => {
    // Fresh-install case. Neither llamacpp nor localai has a CP record yet
    // — both are known only via the preset catalog, both default to :8080.
    // Per the user's spec, we shouldn't show two providers as "enabled" at
    // the same URL: that's a visual contradiction (only one can serve
    // traffic), and toggling either one off would then visibly leave the
    // other on, which reads as auto-enabling.
    //
    // Behavior we pin: an unconfigured provider that conflicts with any
    // peer at the same base URL displays as off. The operator must
    // explicitly opt one in. Non-conflicting unconfigured providers
    // (Ollama at :11434, cloud presets) keep the legacy default-on.
    const state = createRuntimeConsoleFixture({
      session: adminSession,
      providerPresets: presets,
      adminConfig: {
        ...emptyAdminConfig(),
        // No CP records for any provider. providers[] is empty. The conflict
        // map is built from the preset catalog directly.
        providers: [
          // Cloud preset must appear in adminConfig for the row to render
          // — the conflict-default rule applies to it transitively (no
          // conflicts at https://api.anthropic.com → still on).
          makeConfigured("anthropic", { kind: "cloud", base_url: "https://api.anthropic.com/v1" }),
          // Local presets without explicit enable; both at :8080.
          makeConfigured("llamacpp", { kind: "local", base_url: "http://127.0.0.1:8080/v1" }),
          makeConfigured("localai",  { kind: "local", base_url: "http://127.0.0.1:8080/v1" }),
          makeConfigured("ollama",   { kind: "local", base_url: "http://127.0.0.1:11434/v1" }),
        ].map(p => ({ ...p, enabled: undefined as unknown as boolean })),
      },
      providers: [makeStatus("llamacpp"), makeStatus("localai"), makeStatus("ollama")],
    });

    render(<ProvidersView state={state} actions={createRuntimeConsoleActions()} />);

    // Both conflicting locals default to off.
    expect(screen.getByRole("switch", { name: "Enable llama.cpp" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("switch", { name: "Enable LocalAI"   }).getAttribute("aria-checked")).toBe("false");
    // Non-conflicting providers keep the legacy on default.
    expect(screen.getByRole("switch", { name: "Enable Ollama"    }).getAttribute("aria-checked")).toBe("true");
    expect(screen.getByRole("switch", { name: "Enable Anthropic" }).getAttribute("aria-checked")).toBe("true");
  });
});
