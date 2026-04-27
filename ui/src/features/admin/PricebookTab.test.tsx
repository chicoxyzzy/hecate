import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { PricebookTab, formatPricePerMillion, dollarsToMicros } from "./PricebookTab";
import type {
  ConfiguredPricebookRecord,
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

function setup(overrides: Record<string, unknown> = {}, actionOverrides = {}) {
  const adminConfig = {
    backend: "memory",
    tenants: [],
    api_keys: [],
    providers: [],
    policy_rules: [],
    pricebook: sampleRows,
    events: [],
  };
  const providerPresets = [
    { id: "openai", name: "OpenAI", kind: "cloud", protocol: "openai", base_url: "https://api.openai.com" },
    { id: "anthropic", name: "Anthropic", kind: "cloud", protocol: "anthropic", base_url: "https://api.anthropic.com" },
  ];
  const state = createRuntimeConsoleFixture({
    session: adminSession,
    adminConfig: adminConfig as unknown as ReturnType<typeof createRuntimeConsoleFixture>["adminConfig"],
    providerPresets: providerPresets as unknown as ReturnType<typeof createRuntimeConsoleFixture>["providerPresets"],
    ...overrides,
  });
  const actions = { ...createRuntimeConsoleActions(), ...actionOverrides };
  const user = userEvent.setup();
  return { state, actions, user };
}

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
});

describe("PricebookTab rendering", () => {
  it("renders existing rows from state.adminConfig.pricebook", () => {
    const { state, actions } = setup();
    render(<PricebookTab state={state} actions={actions} />);
    // Provider names appear in both the table and the add-form dropdown.
    expect(screen.getAllByText("openai").length).toBeGreaterThan(0);
    expect(screen.getByText("gpt-4o-mini")).toBeTruthy();
    expect(screen.getAllByText("anthropic").length).toBeGreaterThan(0);
    expect(screen.getByText("claude-sonnet-4-6")).toBeTruthy();
    // Source badges.
    expect(screen.getByText("imported")).toBeTruthy();
    expect(screen.getByText("manual")).toBeTruthy();
  });

  it("shows an empty state when there are no rows", () => {
    const { state, actions } = setup({
      adminConfig: {
        backend: "memory",
        tenants: [],
        api_keys: [],
        providers: [],
        policy_rules: [],
        pricebook: [],
        events: [],
      },
    });
    render(<PricebookTab state={state} actions={actions} />);
    expect(screen.getByText(/No pricebook entries/i)).toBeTruthy();
  });
});

describe("PricebookTab add form", () => {
  it("calls upsertPricebookEntry with the right shape", async () => {
    const upsertPricebookEntry = vi.fn(async (_entry: ConfiguredPricebookRecord) => undefined);
    const { state, actions, user } = setup({}, { upsertPricebookEntry });
    render(<PricebookTab state={state} actions={actions} />);

    // Type into the model input.
    const modelInput = screen.getByPlaceholderText(/gpt-4o-mini/i);
    await user.type(modelInput, "my-new-model");
    await user.type(screen.getByPlaceholderText("0.150"), "0.500");
    await user.type(screen.getByPlaceholderText("0.600"), "1.000");

    await user.click(screen.getByRole("button", { name: /^Add$/ }));

    expect(upsertPricebookEntry).toHaveBeenCalledTimes(1);
    const arg = upsertPricebookEntry.mock.calls[0][0];
    expect(arg.model).toBe("my-new-model");
    expect(arg.input_micros_usd_per_million_tokens).toBe(500_000);
    expect(arg.output_micros_usd_per_million_tokens).toBe(1_000_000);
    expect(arg.source).toBe("manual");
  });
});

describe("PricebookTab delete", () => {
  it("calls deletePricebookEntry with the right (provider, model)", async () => {
    const deletePricebookEntry = vi.fn(async () => undefined);
    const { state, actions, user } = setup({}, { deletePricebookEntry });
    render(<PricebookTab state={state} actions={actions} />);

    await user.click(screen.getByRole("button", { name: /Delete openai\/gpt-4o-mini/i }));

    expect(deletePricebookEntry).toHaveBeenCalledWith("openai", "gpt-4o-mini");
  });
});

describe("PricebookTab edit row", () => {
  it("promotes an imported row to manual when the operator saves an edit", async () => {
    // The first row in sampleRows is openai/gpt-4o-mini with source="imported".
    // Editing any field must persist with source="manual" — operator intent
    // wins over the import. This is the UI half of the option-A guarantee
    // (the backend half is tested in handler_pricebook_import_test.go via
    // TestPricebookImportApplyPreservesManualRows).
    const upsertPricebookEntry = vi.fn(async (_entry: ConfiguredPricebookRecord) => undefined);
    const { state, actions, user } = setup({}, { upsertPricebookEntry });
    render(<PricebookTab state={state} actions={actions} />);

    await user.click(screen.getByRole("button", { name: /Edit openai\/gpt-4o-mini/i }));

    // Edit the input price. The view-row had three displayed prices; the
    // edit-row swaps them for three text inputs in the same order. Grab
    // the first one (input price) and bump it.
    const priceInputs = screen.getAllByRole("textbox");
    // The first price input shows "0.150" (150_000 micros / 1M).
    await user.clear(priceInputs[0]);
    await user.type(priceInputs[0], "0.250");

    await user.click(screen.getByRole("button", { name: /^Save$/ }));

    expect(upsertPricebookEntry).toHaveBeenCalledTimes(1);
    const patch = upsertPricebookEntry.mock.calls[0][0];
    expect(patch.provider).toBe("openai");
    expect(patch.model).toBe("gpt-4o-mini");
    expect(patch.input_micros_usd_per_million_tokens).toBe(250_000);
    // The crucial assertion: operator edits always promote to manual,
    // regardless of the row's previous source.
    expect(patch.source).toBe("manual");
  });
});

describe("PricebookTab import modal", () => {
  it("opens a modal that calls previewPricebookImport and shows section counts", async () => {
    const fakeDiff: PricebookImportDiff = {
      fetched_at: "2026-04-27T10:00:00Z",
      added: [
        {
          provider: "groq",
          model: "llama-3.1-8b-instant",
          input_micros_usd_per_million_tokens: 50_000,
          output_micros_usd_per_million_tokens: 80_000,
          cached_input_micros_usd_per_million_tokens: 0,
          source: "imported",
        },
      ],
      updated: [
        {
          entry: {
            provider: "openai",
            model: "gpt-4o-mini",
            input_micros_usd_per_million_tokens: 200_000,
            output_micros_usd_per_million_tokens: 800_000,
            cached_input_micros_usd_per_million_tokens: 100_000,
            source: "imported",
          },
          previous: {
            provider: "openai",
            model: "gpt-4o-mini",
            input_micros_usd_per_million_tokens: 150_000,
            output_micros_usd_per_million_tokens: 600_000,
            cached_input_micros_usd_per_million_tokens: 75_000,
            source: "imported",
          },
        },
      ],
      unchanged: 5,
      skipped: [
        {
          provider: "anthropic",
          model: "claude-sonnet-4-6",
          input_micros_usd_per_million_tokens: 3_000_000,
          output_micros_usd_per_million_tokens: 15_000_000,
          cached_input_micros_usd_per_million_tokens: 300_000,
          source: "manual",
        },
      ],
    };
    const previewPricebookImport = vi.fn(async () => fakeDiff);
    const { state, actions, user } = setup({}, { previewPricebookImport });
    render(<PricebookTab state={state} actions={actions} />);

    await user.click(screen.getByRole("button", { name: /Import latest from LiteLLM/i }));

    await waitFor(() => expect(previewPricebookImport).toHaveBeenCalled());
    // Section counts (Added 1, Updated 1, Skipped 1).
    expect(await screen.findByText("Added")).toBeTruthy();
    expect(screen.getByText("Updated")).toBeTruthy();
    expect(screen.getByText(/Skipped/)).toBeTruthy();
    // Apply button reflects the default-checked count (added + updated = 2).
    expect(await screen.findByRole("button", { name: /Apply selected \(2\)/ })).toBeTruthy();
    // Unchanged count line.
    expect(screen.getByText(/5 unchanged/i)).toBeTruthy();
  });
});
