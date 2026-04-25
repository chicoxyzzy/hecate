import { test as base, type Page } from "@playwright/test";

// ── Mock data ─────────────────────────────────────────────────────────────────

export const MOCK_PROVIDERS = [
  { name: "anthropic", kind: "cloud", healthy: true,  status: "healthy", default_model: "claude-sonnet-4-6", models: ["claude-opus-4-7", "claude-sonnet-4-6", "claude-opus-4-6"] },
  { name: "openai",    kind: "cloud", healthy: true,  status: "healthy", default_model: "gpt-4o",            models: ["gpt-4o", "gpt-4o-mini"] },
  { name: "ollama",    kind: "local", healthy: false, status: "open",    default_model: "llama3.1:8b",       models: [] },
  { name: "llamacpp",  kind: "local", healthy: false, status: "open",    default_model: "llama-3.2",         models: [] },
];

export const MOCK_PRESETS = [
  { id: "anthropic", name: "Anthropic", kind: "cloud", protocol: "anthropic", base_url: "https://api.anthropic.com/v1",  description: "Anthropic's Claude models." },
  { id: "openai",    name: "OpenAI",    kind: "cloud", protocol: "openai",    base_url: "https://api.openai.com/v1",     description: "OpenAI's GPT models." },
  { id: "ollama",    name: "Ollama",    kind: "local", protocol: "openai",    base_url: "http://127.0.0.1:11434/v1",     description: "Local inference via Ollama." },
  { id: "llamacpp",  name: "llama.cpp", kind: "local", protocol: "openai",    base_url: "http://127.0.0.1:8080/v1",      description: "Local inference via llama.cpp." },
];

export const MOCK_MODELS = [
  { id: "claude-opus-4-7",  owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: false } },
  { id: "claude-sonnet-4-6", owned_by: "anthropic", metadata: { provider: "anthropic", provider_kind: "cloud", default: true } },
  { id: "gpt-4o",           owned_by: "openai",    metadata: { provider: "openai",    provider_kind: "cloud", default: true } },
  { id: "gpt-4o-mini",      owned_by: "openai",    metadata: { provider: "openai",    provider_kind: "cloud", default: false } },
];

export const MOCK_ADMIN_CONFIG = {
  providers: [
    { id: "anthropic", name: "anthropic", kind: "cloud", protocol: "anthropic", base_url: "https://api.anthropic.com/v1", enabled: true, credential_configured: true },
    { id: "openai",    name: "openai",    kind: "cloud", protocol: "openai",    base_url: "https://api.openai.com/v1",    enabled: true, credential_configured: true },
  ],
  tenants: [],
  api_keys: [],
  policy_rules: [],
};

// ── Route mocking ─────────────────────────────────────────────────────────────

export async function mockGatewayAPIs(page: Page) {
  const ok = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route("/healthz", r => r.fulfill(ok({ status: "ok", time: "2026-04-25T00:00:00Z" })));

  await page.route("/v1/whoami", r =>
    r.fulfill(ok({
      object: "session",
      data: {
        authenticated: false,
        invalid_token: false,
        role: "anonymous",
        tenant: "",
        source: "",
        key_id: "",
      },
    })),
  );

  await page.route("/v1/models*", r =>
    r.fulfill(ok({ object: "list", data: MOCK_MODELS })),
  );

  await page.route("/admin/providers*", r =>
    r.fulfill(ok({ object: "list", data: MOCK_PROVIDERS })),
  );

  await page.route("/v1/provider-presets*", r =>
    r.fulfill(ok({ object: "list", data: MOCK_PRESETS })),
  );

  await page.route("/admin/budget*", r =>
    r.fulfill(ok({
      object: "budget_status",
      data: {
        key: "global", scope: "global", backend: "memory",
        balance_source: "config",
        debited_micros_usd: 0, debited_usd: "0.000000",
        credited_micros_usd: 1_000_000, credited_usd: "1.000000",
        balance_micros_usd: 1_000_000, balance_usd: "1.000000",
        available_micros_usd: 1_000_000, available_usd: "1.000000",
        enforced: false,
      },
    })),
  );

  await page.route("/admin/accounts/summary*", r =>
    r.fulfill(ok({ object: "account_summary", data: null })),
  );

  await page.route("/v1/chat/sessions*", r =>
    r.fulfill(ok({ object: "list", data: [], has_more: false })),
  );

  await page.route("/admin/requests*", r =>
    r.fulfill(ok({ object: "list", data: [] })),
  );

  await page.route("/admin/control-plane*", r =>
    r.fulfill(ok({ object: "configured_state", data: MOCK_ADMIN_CONFIG })),
  );

  await page.route("/admin/retention/runs*", r =>
    r.fulfill(ok({ object: "list", data: [] })),
  );

  await page.route("/admin/runtime/stats*", r =>
    r.fulfill(ok({ object: "runtime_stats", data: {} })),
  );

  await page.route("/admin/traces*", r =>
    r.fulfill(ok({ object: "list", data: [] })),
  );
}

// ── Extended test fixture ─────────────────────────────────────────────────────

export const test = base.extend<{ page: Page }>({
  page: async ({ page }, use) => {
    await mockGatewayAPIs(page);
    await use(page);
  },
});

export { expect } from "@playwright/test";
