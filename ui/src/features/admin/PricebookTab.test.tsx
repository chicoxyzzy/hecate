import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import {
  PricebookTab,
  formatPricePerMillion,
  dollarsToMicros,
  describeAddedDetail,
  describeUpdatedDetail,
} from "./PricebookTab";
import type {
  ConfiguredPricebookRecord,
  ModelRecord,
  PricebookImportDiff,
} from "../../types/runtime";
import { createRuntimeConsoleActions, createRuntimeConsoleFixture } from "../../test/runtime-console-fixture";

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

const sampleRows: ConfiguredPricebookRecord[] = [
  {
    provider: "openai",
    model: "gpt-4o-mini",
    input_micros_usd_per_million_tokens: 150_000,
    output_micros_usd_per_million_tokens: 600_000,
    cached_input_micros_usd_per_million_tokens: 75_000,
    source: "imported",
  },
  {
    provider: "anthropic",
    model: "claude-sonnet-4-6",
    input_micros_usd_per_million_tokens: 3_000_000,
    output_micros_usd_per_million_tokens: 15_000_000,
    cached_input_micros_usd_per_million_tokens: 300_000,
    source: "manual",
  },
];

const presets = [
  { id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" },
  { id: "anthropic", name: "Anthropic", kind: "cloud", protocol: "anthropic", base_url: "https://api.anthropic.com" },
  { id: "gemini", name: "Google Gemini", kind: "cloud", protocol: "google", base_url: "https://generativelanguage.googleapis.com" },
  { id: "ollama", name: "Ollama", kind: "local", protocol: "openai", base_url: "http://127.0.0.1:11434" },
];

const emptyDiff: PricebookImportDiff = { fetched_at: "2026", added: [], updated: [], skipped: [], unchanged: 0 };

function setup(overrides: Record<string, unknown> = {}, actionOverrides: Record<string, unknown> = {}) {
  const adminConfig = {
    backend: "memory",
    tenants: [],
    api_keys: [],
    providers: [],
    policy_rules: [],
    pricebook: sampleRows,
    events: [],
  };
  const state = createRuntimeConsoleFixture({
    session: adminSession,
    adminConfig: adminConfig as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    providerPresets: presets as unknown as ReturnType<typeof createRuntimeConsoleFixture>["providerPresets"],
    ...overrides,
  });
  const actions = {
    ...createRuntimeConsoleActions(),
    previewPricebookImport: vi.fn(async () => emptyDiff),
    ...actionOverrides,
  };
  const user = userEvent.setup();
  return { state, actions, user };
}

// ─── Helper functions ────────────────────────────────────────────────────────

describe("PricebookTab helpers", () => {
  it("formatPricePerMillion formats a normal price", () => {
    expect(formatPricePerMillion(150_000)).toBe("$0.150 / 1M");
  });

  it("formatPricePerMillion returns dash for zero or negatives", () => {
    expect(formatPricePerMillion(0)).toBe("—");
    expect(formatPricePerMillion(-1)).toBe("—");
  });

  it("dollarsToMicros parses common forms", () => {
    expect(dollarsToMicros("0.15")).toBe(150_000);
    expect(dollarsToMicros("$0.15")).toBe(150_000);
    expect(dollarsToMicros("0.15 / 1M")).toBe(150_000);
  });

  it("dollarsToMicros rejects junk", () => {
    expect(dollarsToMicros("")).toBeNull();
    expect(dollarsToMicros("abc")).toBeNull();
    expect(dollarsToMicros("-5")).toBeNull();
  });

  it("describeAddedDetail shows in/out/cache for a fresh entry", () => {
    const detail = describeAddedDetail({
      provider: "openai",
      model: "gpt-4o",
      input_micros_usd_per_million_tokens: 2_500_000,
      output_micros_usd_per_million_tokens: 10_000_000,
      cached_input_micros_usd_per_million_tokens: 1_250_000,
      source: "imported",
    });
    expect(detail).toBe("in $2.500  out $10.000  cache $1.250");
  });

  it("describeUpdatedDetail only shows fields that actually changed", () => {
    const prev = {
      provider: "gemini", model: "gemini-flash-latest",
      input_micros_usd_per_million_tokens: 100_000,
      output_micros_usd_per_million_tokens: 300_000,
      cached_input_micros_usd_per_million_tokens: 0,
      source: "imported",
    };
    const next = { ...prev, cached_input_micros_usd_per_million_tokens: 25_000 };
    expect(describeUpdatedDetail(prev, next)).toBe("cache — → $0.025");
  });

  it("describeUpdatedDetail joins multiple changed fields", () => {
    const prev = {
      provider: "openai", model: "gpt-4o",
      input_micros_usd_per_million_tokens: 2_500_000,
      output_micros_usd_per_million_tokens: 10_000_000,
      cached_input_micros_usd_per_million_tokens: 0,
      source: "imported",
    };
    const next = {
      ...prev,
      input_micros_usd_per_million_tokens: 2_000_000,
      cached_input_micros_usd_per_million_tokens: 1_250_000,
    };
    expect(describeUpdatedDetail(prev, next)).toBe("in $2.500 → $2.000  cache — → $1.250");
  });
});

// ─── Catalog/pricebook merge & status ────────────────────────────────────────

describe("PricebookTab unified rows", () => {
  it("renders catalog models with no pricebook entry as 'unpriced'", () => {
    const models: ModelRecord[] = [
      { id: "gpt-4o", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } },
    ];
    const { state, actions } = setup({
      models,
      adminConfig: {
        backend: "memory",
        tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
        pricebook: [],
      } as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    });
    render(<PricebookTab state={state} actions={actions} />);
    expect(screen.getByText("gpt-4o")).toBeTruthy();
    // "unpriced" appears in the status tab AND on the badge — pick the
    // table cell version by scoping to the row.
    const row = screen.getByText("gpt-4o").closest("tr");
    expect(row).toBeTruthy();
    expect(within(row!).getByText("unpriced")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Set price for openai\/gpt-4o/i })).toBeTruthy();
  });

  it("renders pricebook entries that aren't in the catalog as 'deprecated'", () => {
    const { state, actions } = setup({ models: [] });
    render(<PricebookTab state={state} actions={actions} />);
    // Two deprecated rows in the table, plus one "deprecated" word in
    // the status-tab strip → 3 total matches.
    expect(screen.getAllByText("deprecated").length).toBe(3);
    expect(screen.getByText("claude-sonnet-4-6")).toBeTruthy();
    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
  });

  it("groups by provider name, sorts groups + models alphabetically", () => {
    const models: ModelRecord[] = [
      { id: "gpt-4o", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } },
      { id: "gpt-3.5-turbo", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } },
      { id: "claude-haiku-4-5", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
    ];
    const { state, actions } = setup({
      models,
      adminConfig: {
        backend: "memory",
        tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
        pricebook: [],
      } as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    });
    render(<PricebookTab state={state} actions={actions} />);

    const headers = screen.getAllByText(/^Anthropic$|^OpenAI$/);
    const anthropicIdx = headers.findIndex(h => h.textContent === "Anthropic");
    const openaiIdx = headers.findIndex(h => h.textContent === "OpenAI");
    expect(anthropicIdx).toBeLessThan(openaiIdx);

    const modelCells = screen.getAllByText(/^gpt-\d/);
    expect(modelCells[0].textContent).toBe("gpt-3.5-turbo");
    expect(modelCells[1].textContent).toBe("gpt-4o");
  });

  it("priced rows show the source badge (imported/manual)", () => {
    const models: ModelRecord[] = [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
      { id: "claude-sonnet-4-6", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
    ];
    const { state, actions } = setup({ models });
    render(<PricebookTab state={state} actions={actions} />);
    expect(screen.getByText("imported")).toBeTruthy();
    expect(screen.getByText("manual")).toBeTruthy();
  });

  it("excludes local-provider rows from catalog and pricebook", () => {
    // ollama is local (presets[3].kind === "local"). Even with models
    // and pricebook entries for it, the table must not render any.
    const models: ModelRecord[] = [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
      { id: "llama-3.1-8b", owned_by: "ollama", metadata: { provider: "ollama", provider_kind: "local", default: false } },
    ];
    const adminConfig = {
      backend: "memory",
      tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
      pricebook: [
        ...sampleRows,
        // A pricebook entry for a local provider should also be hidden.
        {
          provider: "ollama", model: "llama-3.1-8b",
          input_micros_usd_per_million_tokens: 0,
          output_micros_usd_per_million_tokens: 0,
          cached_input_micros_usd_per_million_tokens: 0,
          source: "manual",
        },
      ],
    };
    const { state, actions } = setup({
      models,
      adminConfig: adminConfig as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    });
    render(<PricebookTab state={state} actions={actions} />);
    expect(screen.queryByText("llama-3.1-8b")).toBeNull();
    expect(screen.queryByText("Ollama")).toBeNull();
    // Cloud rows still render.
    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
  });

  it("shows the 'no models' empty state when both catalog and pricebook are empty", () => {
    const { state, actions } = setup({
      models: [],
      adminConfig: {
        backend: "memory",
        tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
        pricebook: [],
      } as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    });
    render(<PricebookTab state={state} actions={actions} />);
    expect(screen.getByText(/No models known to the gateway/i)).toBeTruthy();
  });
});

// ─── Filters ─────────────────────────────────────────────────────────────────

describe("PricebookTab filters", () => {
  function filterFixture() {
    const models: ModelRecord[] = [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
      { id: "gpt-4o", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } },
      { id: "claude-sonnet-4-6", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
      { id: "gemini-3.0-pro", owned_by: "gemini", metadata: { provider: "gemini", provider_kind: "cloud", default: false } },
    ];
    const adminConfig = {
      backend: "memory",
      tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
      pricebook: [
        ...sampleRows,
        {
          provider: "openai", model: "gpt-3.5-turbo",
          input_micros_usd_per_million_tokens: 500_000,
          output_micros_usd_per_million_tokens: 1_500_000,
          cached_input_micros_usd_per_million_tokens: 0,
          source: "imported",
        },
      ],
    };
    return setup({
      models,
      adminConfig: adminConfig as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    });
  }

  it("status tabs narrow by row status", async () => {
    const { state, actions, user } = filterFixture();
    render(<PricebookTab state={state} actions={actions} />);

    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
    expect(screen.getByText("gpt-4o")).toBeTruthy();
    expect(screen.getByText("gpt-3.5-turbo")).toBeTruthy();
    expect(screen.getByText("gemini-3.0-pro")).toBeTruthy();

    // Tabs use exact aria-labels so "priced" doesn't accidentally match
    // "unpriced" — both contain the substring.
    await user.click(screen.getByRole("tab", { name: "unpriced" }));
    expect(screen.queryByText("gpt-4o-mini")).toBeNull();
    expect(screen.getByText("gpt-4o")).toBeTruthy();
    expect(screen.getByText("gemini-3.0-pro")).toBeTruthy();
    expect(screen.queryByText("gpt-3.5-turbo")).toBeNull();

    await user.click(screen.getByRole("tab", { name: "deprecated" }));
    expect(screen.getByText("gpt-3.5-turbo")).toBeTruthy();
    expect(screen.queryByText("gpt-4o-mini")).toBeNull();

    await user.click(screen.getByRole("tab", { name: "priced" }));
    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
    expect(screen.queryByText("gpt-4o")).toBeNull();
  });

  it("provider filter narrows rows to one provider", async () => {
    const { state, actions, user } = filterFixture();
    render(<PricebookTab state={state} actions={actions} />);

    await user.click(screen.getByRole("button", { name: /All providers/i }));
    const dropdown = document.querySelector(".dropdown-menu");
    expect(dropdown).toBeTruthy();
    const openaiOption = Array.from(dropdown!.querySelectorAll(".dropdown-item")).find(d => d.textContent?.trim() === "OpenAI");
    expect(openaiOption).toBeTruthy();
    await user.click(openaiOption!);

    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
    expect(screen.getByText("gpt-4o")).toBeTruthy();
    expect(screen.getByText("gpt-3.5-turbo")).toBeTruthy();
    expect(screen.queryByText("claude-sonnet-4-6")).toBeNull();
    expect(screen.queryByText("gemini-3.0-pro")).toBeNull();
  });

  it("search box matches model id substrings (case-insensitive)", async () => {
    const { state, actions, user } = filterFixture();
    render(<PricebookTab state={state} actions={actions} />);

    await user.type(screen.getByLabelText(/Search models/i), "GPT-4");
    expect(screen.getByText("gpt-4o")).toBeTruthy();
    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
    expect(screen.queryByText("gemini-3.0-pro")).toBeNull();
    expect(screen.queryByText("claude-sonnet-4-6")).toBeNull();
  });

  it("renders the no-match empty state when filters yield zero rows", async () => {
    const { state, actions, user } = filterFixture();
    render(<PricebookTab state={state} actions={actions} />);

    await user.type(screen.getByLabelText(/Search models/i), "no-such-model-xyz");
    expect(screen.getByText(/No models match the current filters/i)).toBeTruthy();
  });
});

// ─── Inline actions ──────────────────────────────────────────────────────────

describe("PricebookTab inline actions", () => {
  it("priced row → Edit + Delete buttons; clicking delete calls the action", async () => {
    const deletePricebookEntry = vi.fn(async () => undefined);
    const models: ModelRecord[] = [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
    ];
    const { state, actions, user } = setup({ models }, { deletePricebookEntry });
    render(<PricebookTab state={state} actions={actions} />);

    // The broom-icon Clear button opens a styled ConfirmModal (no
    // longer uses the native `window.confirm`). The action only
    // fires after the operator clicks Confirm in the modal.
    await user.click(screen.getByRole("button", { name: /Clear openai\/gpt-4o-mini/i }));
    const dialog = await screen.findByRole("dialog", { name: /Clear price/i });
    await user.click(within(dialog).getByRole("button", { name: /^Clear price$/i }));
    expect(deletePricebookEntry).toHaveBeenCalledWith("openai", "gpt-4o-mini");
  });

  it("editing an imported row promotes it to manual on save", async () => {
    const upsertPricebookEntry = vi.fn(async (_entry: ConfiguredPricebookRecord) => undefined);
    const models: ModelRecord[] = [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
    ];
    const { state, actions, user } = setup({ models }, { upsertPricebookEntry });
    render(<PricebookTab state={state} actions={actions} />);

    await user.click(screen.getByRole("button", { name: /Edit openai\/gpt-4o-mini/i }));
    const inputBox = screen.getByLabelText("Input price");
    await user.clear(inputBox);
    await user.type(inputBox, "0.250");
    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    expect(upsertPricebookEntry).toHaveBeenCalledTimes(1);
    const patch = upsertPricebookEntry.mock.calls[0][0];
    expect(patch.input_micros_usd_per_million_tokens).toBe(250_000);
    expect(patch.source).toBe("manual");
  });

  it("unpriced row with no LiteLLM data shows 'Set price' which switches to edit mode", async () => {
    const upsertPricebookEntry = vi.fn(async (_entry: ConfiguredPricebookRecord) => undefined);
    const models: ModelRecord[] = [
      { id: "gpt-4o", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } },
    ];
    const { state, actions, user } = setup(
      {
        models,
        adminConfig: {
          backend: "memory",
          tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
          pricebook: [],
        } as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
      },
      { upsertPricebookEntry },
    );
    render(<PricebookTab state={state} actions={actions} />);

    await user.click(screen.getByRole("button", { name: /Set price for openai\/gpt-4o/i }));
    const inputBox = screen.getByLabelText("Input price");
    const outputBox = screen.getByLabelText("Output price");
    expect((inputBox as HTMLInputElement).value).toBe("");
    expect((outputBox as HTMLInputElement).value).toBe("");

    await user.type(inputBox, "0.500");
    await user.type(outputBox, "1.000");
    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    expect(upsertPricebookEntry).toHaveBeenCalledTimes(1);
    const patch = upsertPricebookEntry.mock.calls[0][0];
    expect(patch.provider).toBe("openai");
    expect(patch.model).toBe("gpt-4o");
    expect(patch.source).toBe("manual");
  });

  it("manual row whose LiteLLM proposal differs gets an Import button (separate column)", async () => {
    // Manual rows used to be locked out of inline import. With the
    // Skipped section now carrying LiteLLM's proposal, the dedicated
    // LiteLLM column shows an Import button on manual rows whose
    // LiteLLM price differs from the manual one. Clicking it applies
    // a single-key import — the backend allows manual override when
    // the key is explicitly listed.
    const applyPricebookImport = vi.fn(async () => emptyDiff);
    const manualRow: ConfiguredPricebookRecord = {
      provider: "openai", model: "gpt-4o-mini",
      input_micros_usd_per_million_tokens: 80_000, // operator's negotiated rate
      output_micros_usd_per_million_tokens: 200_000,
      cached_input_micros_usd_per_million_tokens: 0,
      source: "manual",
    };
    const litellmProposal: ConfiguredPricebookRecord = {
      provider: "openai", model: "gpt-4o-mini",
      input_micros_usd_per_million_tokens: 150_000,
      output_micros_usd_per_million_tokens: 600_000,
      cached_input_micros_usd_per_million_tokens: 75_000,
      source: "imported",
    };
    const previewPricebookImport = vi.fn(async () => ({
      fetched_at: "2026", added: [], updated: [],
      // Skipped now pairs LiteLLM proposal (entry) with current manual (previous).
      skipped: [{ entry: litellmProposal, previous: manualRow }],
      unchanged: 0,
    } as PricebookImportDiff));
    const adminConfig = {
      backend: "memory",
      tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
      pricebook: [manualRow],
    };
    const models: ModelRecord[] = [
      { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
    ];
    const { state, actions, user } = setup(
      { models, adminConfig: adminConfig as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"] },
      { applyPricebookImport, previewPricebookImport },
    );
    render(<PricebookTab state={state} actions={actions} />);

    // Clicking the row's Import opens a ConfirmModal. The actual
    // apply runs only after the modal's Confirm is clicked.
    const importBtn = await screen.findByRole("button", { name: /Import update for openai\/gpt-4o-mini/i });
    await user.click(importBtn);
    const dialog = await screen.findByRole("dialog", { name: /Import price update/i });
    await user.click(within(dialog).getByRole("button", { name: /^Import$/i }));
    expect(applyPricebookImport).toHaveBeenCalledWith(["openai/gpt-4o-mini"]);
  });

  it("unpriced row with LiteLLM data shows 'Import' which calls applyPricebookImport with one key", async () => {
    const applyPricebookImport = vi.fn(async () => emptyDiff);
    const litellmAdded: ConfiguredPricebookRecord = {
      provider: "openai", model: "gpt-4o",
      input_micros_usd_per_million_tokens: 2_500_000,
      output_micros_usd_per_million_tokens: 10_000_000,
      cached_input_micros_usd_per_million_tokens: 0,
      source: "imported",
    };
    const previewPricebookImport = vi.fn(async () => ({
      fetched_at: "2026", added: [litellmAdded], updated: [], skipped: [], unchanged: 0,
    } as PricebookImportDiff));
    const models: ModelRecord[] = [
      { id: "gpt-4o", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } },
    ];
    const { state, actions, user } = setup(
      {
        models,
        adminConfig: {
          backend: "memory",
          tenants: [], api_keys: [], providers: [], policy_rules: [], events: [],
          pricebook: [],
        } as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
      },
      { applyPricebookImport, previewPricebookImport },
    );
    render(<PricebookTab state={state} actions={actions} />);

    const importBtn = await screen.findByRole("button", { name: /Import update for openai\/gpt-4o/i });
    await user.click(importBtn);
    const dialog = await screen.findByRole("dialog", { name: /Import price update/i });
    await user.click(within(dialog).getByRole("button", { name: /^Import$/i }));

    expect(applyPricebookImport).toHaveBeenCalledWith(["openai/gpt-4o"]);
  });
});

// ─── Bulk import: "Import all" → consent SlideOver ───────────────────────────

const sampleAdded: ConfiguredPricebookRecord = {
  provider: "openai", model: "gpt-4o-mini",
  input_micros_usd_per_million_tokens: 150_000,
  output_micros_usd_per_million_tokens: 600_000,
  cached_input_micros_usd_per_million_tokens: 75_000,
  source: "imported",
};

const sampleUpdated = {
  entry: {
    provider: "openai", model: "gpt-4o",
    input_micros_usd_per_million_tokens: 2_000_000,
    output_micros_usd_per_million_tokens: 8_000_000,
    cached_input_micros_usd_per_million_tokens: 0,
    source: "imported",
  },
  previous: {
    provider: "openai", model: "gpt-4o",
    input_micros_usd_per_million_tokens: 2_500_000,
    output_micros_usd_per_million_tokens: 10_000_000,
    cached_input_micros_usd_per_million_tokens: 0,
    source: "imported",
  },
};

function setupForConsent(opts: {
  models?: ModelRecord[];
  currentPricebook?: ConfiguredPricebookRecord[];
  diff: PricebookImportDiff;
  applyPricebookImport?: (keys: string[]) => Promise<PricebookImportDiff>;
}) {
  const { models = [], currentPricebook = [], diff, applyPricebookImport } = opts;
  const adminConfig = {
    backend: "memory",
    tenants: [], api_keys: [], policy_rules: [], events: [],
    providers: [],
    pricebook: currentPricebook,
  };
  const previewPricebookImport = vi.fn(async () => diff);
  return setup(
    { models, adminConfig: adminConfig as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"] },
    {
      previewPricebookImport,
      ...(applyPricebookImport ? { applyPricebookImport: vi.fn(applyPricebookImport) } : {}),
    },
  );
}

describe("PricebookTab Import all → consent SlideOver", () => {
  it("Import all button is disabled until LiteLLM has actionable changes", async () => {
    // Catalog has gpt-4o-mini, but the diff is empty → nothing to import.
    const { state, actions } = setupForConsent({
      models: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } }],
      diff: emptyDiff,
    });
    render(<PricebookTab state={state} actions={actions} />);
    const btn = await screen.findByRole("button", { name: /Import all/i });
    expect((btn as HTMLButtonElement).disabled).toBe(true);
  });

  it("clicking 'Import all' opens a SlideOver listing only catalog cloud changes", async () => {
    // gpt-4o-mini is in catalog → consent dialog shows it.
    // cohere/command-r is LiteLLM-only (not in catalog) → must NOT appear.
    const cohereAdded: ConfiguredPricebookRecord = {
      provider: "cohere", model: "command-r",
      input_micros_usd_per_million_tokens: 500_000, output_micros_usd_per_million_tokens: 1_500_000,
      cached_input_micros_usd_per_million_tokens: 0, source: "imported",
    };
    const { state, actions, user } = setupForConsent({
      models: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } }],
      diff: { fetched_at: "2026", added: [sampleAdded, cohereAdded], updated: [], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    const btn = await screen.findByRole("button", { name: /Import all/i });
    await user.click(btn);

    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    expect(within(dialog).getByText("gpt-4o-mini")).toBeTruthy();
    // Consent dialog now renders provider sub-headers; "command-r" the
    // model is suppressed AND there's no "cohere" provider header.
    expect(within(dialog).queryByText("command-r")).toBeNull();
    expect(within(dialog).queryByText(/cohere/i)).toBeNull();
  });

  it("consent dialog shows 'Add' section for new entries with in/out/cache detail", async () => {
    const { state, actions, user } = setupForConsent({
      models: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } }],
      diff: { fetched_at: "2026", added: [sampleAdded], updated: [], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    expect(within(dialog).getByText("New prices")).toBeTruthy();
    // testing-library normalizes whitespace, so two-space separators
    // collapse to one. Match the visible text accordingly.
    expect(within(dialog).getByText(/in \$0\.150 out \$0\.600 cache \$0\.075/)).toBeTruthy();
  });

  it("consent dialog shows 'Update' section with prev → next price detail", async () => {
    const { state, actions, user } = setupForConsent({
      models: [{ id: "gpt-4o", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: false } }],
      currentPricebook: [sampleUpdated.previous as ConfiguredPricebookRecord],
      diff: { fetched_at: "2026", added: [], updated: [sampleUpdated], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    expect(within(dialog).getByText("Price updates")).toBeTruthy();
    expect(within(dialog).getByText(/\$2\.500 → \$2\.000/)).toBeTruthy();
  });

  it("all changes are checked by default; Apply button reflects selection count", async () => {
    const second: ConfiguredPricebookRecord = {
      provider: "anthropic", model: "claude-opus-4-7",
      input_micros_usd_per_million_tokens: 200_000, output_micros_usd_per_million_tokens: 1_000_000,
      cached_input_micros_usd_per_million_tokens: 0, source: "imported",
    };
    const { state, actions, user } = setupForConsent({
      models: [
        { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
        { id: "claude-opus-4-7", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
      ],
      diff: { fetched_at: "2026", added: [sampleAdded, second], updated: [], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    // Both rows pre-checked → button reads "Apply 2 changes".
    expect(within(dialog).getByRole("button", { name: /Apply 2 changes/i })).toBeTruthy();

    // Uncheck one row → button drops to 1.
    await user.click(within(dialog).getByRole("checkbox", { name: "gpt-4o-mini" }));
    expect(within(dialog).getByRole("button", { name: /Apply 1 change/i })).toBeTruthy();
  });

  it("Apply sends only the still-checked keys, then closes the dialog", async () => {
    const applyPricebookImport = vi.fn(async () => emptyDiff);
    const second: ConfiguredPricebookRecord = {
      provider: "anthropic", model: "claude-opus-4-7",
      input_micros_usd_per_million_tokens: 200_000, output_micros_usd_per_million_tokens: 1_000_000,
      cached_input_micros_usd_per_million_tokens: 0, source: "imported",
    };
    const { state, actions, user } = setupForConsent({
      models: [
        { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
        { id: "claude-opus-4-7", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
      ],
      diff: { fetched_at: "2026", added: [sampleAdded, second], updated: [], skipped: [], unchanged: 0 },
      applyPricebookImport,
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });

    // Uncheck the openai entry; only claude should ship.
    await user.click(within(dialog).getByRole("checkbox", { name: "gpt-4o-mini" }));
    await user.click(within(dialog).getByRole("button", { name: /Apply 1 change/i }));
    expect(applyPricebookImport).toHaveBeenCalledWith(["anthropic/claude-opus-4-7"]);
  });

  it("'select all' / 'deselect all' header toggle flips every row at once", async () => {
    const { state, actions, user } = setupForConsent({
      models: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } }],
      diff: { fetched_at: "2026", added: [sampleAdded], updated: [], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });

    // Initially all checked → label says "deselect all".
    const toggleAll = within(dialog).getByRole("checkbox", { name: /Toggle all/i });
    expect((toggleAll as HTMLInputElement).checked).toBe(true);
    expect(within(dialog).getByText(/deselect all/i)).toBeTruthy();

    await user.click(toggleAll);
    expect((toggleAll as HTMLInputElement).checked).toBe(false);
    expect(within(dialog).getByText(/select all/i)).toBeTruthy();
    expect(within(dialog).getByRole("button", { name: /Apply 0 changes/i })).toBeTruthy();
  });

  it("renders 'Replace manual' section for manual rows where LiteLLM differs", async () => {
    // Skipped now carries LiteLLM's proposal for differing manual rows.
    // The consent dialog surfaces them in their own section so the
    // operator can opt in to overwriting individual rows.
    const manualRow: ConfiguredPricebookRecord = {
      provider: "openai", model: "gpt-4o-mini",
      input_micros_usd_per_million_tokens: 80_000,
      output_micros_usd_per_million_tokens: 200_000,
      cached_input_micros_usd_per_million_tokens: 0,
      source: "manual",
    };
    const litellmProposal: ConfiguredPricebookRecord = {
      ...manualRow,
      input_micros_usd_per_million_tokens: 150_000,
      output_micros_usd_per_million_tokens: 600_000,
      source: "imported",
    };
    const { state, actions, user } = setupForConsent({
      models: [{ id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } }],
      currentPricebook: [manualRow],
      diff: { fetched_at: "2026", added: [], updated: [], skipped: [{ entry: litellmProposal, previous: manualRow }], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    expect(within(dialog).getByText("Override manual")).toBeTruthy();
    expect(within(dialog).getByText("gpt-4o-mini")).toBeTruthy();
    // Update detail format includes the diff arrows.
    expect(within(dialog).getByText(/\$0\.080 → \$0\.150/)).toBeTruthy();
  });

  it("pre-selects only the rows matching the active filter; modal still shows everything", async () => {
    // Two added rows — different providers. Filter to openai.
    // Modal opens listing both, but only the openai row is checked.
    const otherAdded: ConfiguredPricebookRecord = {
      provider: "anthropic", model: "claude-opus-4-7",
      input_micros_usd_per_million_tokens: 200_000,
      output_micros_usd_per_million_tokens: 1_000_000,
      cached_input_micros_usd_per_million_tokens: 0,
      source: "imported",
    };
    const { state, actions, user } = setupForConsent({
      models: [
        { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
        { id: "claude-opus-4-7", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
      ],
      diff: { fetched_at: "2026", added: [sampleAdded, otherAdded], updated: [], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);

    // Switch the provider filter to OpenAI.
    await user.click(screen.getByRole("button", { name: /All providers/i }));
    const dropdown = document.querySelector(".dropdown-menu");
    const openaiOption = Array.from(dropdown!.querySelectorAll(".dropdown-item")).find(d => d.textContent?.trim() === "OpenAI");
    await user.click(openaiOption!);

    // Counter on the button reflects only the filtered subset.
    expect(await screen.findByRole("button", { name: /Import all.*1/i })).toBeTruthy();

    // Open the dialog. Both rows are listed, but only the openai
    // checkbox is checked → "Apply 1 change".
    await user.click(screen.getByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    expect(within(dialog).getByText("gpt-4o-mini")).toBeTruthy();
    expect(within(dialog).getByText("claude-opus-4-7")).toBeTruthy();
    const openaiBox = within(dialog).getByRole("checkbox", { name: "gpt-4o-mini" });
    const anthropicBox = within(dialog).getByRole("checkbox", { name: "claude-opus-4-7" });
    expect((openaiBox as HTMLInputElement).checked).toBe(true);
    expect((anthropicBox as HTMLInputElement).checked).toBe(false);
    expect(within(dialog).getByRole("button", { name: /Apply 1 change/i })).toBeTruthy();
  });

  it("excludes local-provider entries from the consent dialog even if LiteLLM proposes them", async () => {
    // ollama is local. A LiteLLM "added" entry for ollama/* shouldn't
    // surface in the dialog because the table doesn't show local rows.
    const localAdded: ConfiguredPricebookRecord = {
      provider: "ollama", model: "llama-3.1-8b",
      input_micros_usd_per_million_tokens: 0, output_micros_usd_per_million_tokens: 0,
      cached_input_micros_usd_per_million_tokens: 0, source: "imported",
    };
    const { state, actions, user } = setupForConsent({
      models: [
        { id: "gpt-4o-mini", owned_by: "openai", metadata: { provider: "openai", provider_kind: "cloud", default: true } },
        { id: "llama-3.1-8b", owned_by: "ollama", metadata: { provider: "ollama", provider_kind: "local", default: false } },
      ],
      diff: { fetched_at: "2026", added: [sampleAdded, localAdded], updated: [], skipped: [], unchanged: 0 },
    });
    render(<PricebookTab state={state} actions={actions} />);
    await user.click(await screen.findByRole("button", { name: /Import all/i }));
    const dialog = await screen.findByRole("dialog", { name: /Update pricebook/i });
    expect(within(dialog).getByText("gpt-4o-mini")).toBeTruthy();
    // Provider-grouped layout: assert both the model row and the
    // provider sub-header for ollama are absent.
    expect(within(dialog).queryByText("llama-3.1-8b")).toBeNull();
    expect(within(dialog).queryByText(/ollama/i)).toBeNull();
  });
});
