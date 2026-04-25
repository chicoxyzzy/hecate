import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ProvidersView } from "./ProvidersView";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";
import type { ConfiguredProviderRecord, ProviderPresetRecord, ProviderStatus } from "../../types/runtime";

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

function makeStatus(name: string, overrides: Partial<ProviderStatus> = {}): ProviderStatus {
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
    backend: "memory", path: "",
    tenants: [], api_keys: [], policy_rules: [], pricebook: [], events: [],
    providers: [] as ConfiguredProviderRecord[],
  };
}

describe("ProvidersView conflict resolution", () => {
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

  it("optimistically disables conflicting providers when the user enables one", () => {
    const setProviderEnabled = vi.fn(async () => undefined);
    const actions = { ...createRuntimeConsoleActions(), setProviderEnabled };

    // Start with llamacpp enabled, localai disabled.
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

    fireEvent.click(localaiToggle);

    // Backend was called for the toggled provider.
    expect(setProviderEnabled).toHaveBeenCalledWith("localai", true);

    // After the click, llamacpp should appear off (optimistic mutual exclusion).
    expect(screen.getByRole("switch", { name: "Enable llama.cpp" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("switch", { name: "Enable LocalAI"   }).getAttribute("aria-checked")).toBe("true");
  });

  it("does not flip conflicting providers when the user disables one", () => {
    const setProviderEnabled = vi.fn(async () => undefined);
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

    fireEvent.click(screen.getByRole("switch", { name: "Enable llama.cpp" }));

    expect(setProviderEnabled).toHaveBeenCalledWith("llamacpp", false);
    // llamacpp now off, localai stays where the CP put it (still off — backend hasn't re-resolved).
    expect(screen.getByRole("switch", { name: "Enable llama.cpp" }).getAttribute("aria-checked")).toBe("false");
    expect(screen.getByRole("switch", { name: "Enable LocalAI"   }).getAttribute("aria-checked")).toBe("false");
  });
});
