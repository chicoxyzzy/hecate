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

// MOCK_ADMIN_CONFIG mirrors the real backend contract: all 12 built-ins are always
// returned, and conflicts are pre-resolved (llamacpp + localai share 127.0.0.1:8080,
// so only the alphabetically-first — llamacpp — is enabled by default).
export const MOCK_ADMIN_CONFIG = {
  providers: [
    { id: "anthropic", name: "anthropic", kind: "cloud", protocol: "openai", base_url: "https://api.anthropic.com/v1", enabled: true,  credential_configured: true,  credential_source: "vault" },
    { id: "deepseek",  name: "deepseek",  kind: "cloud", protocol: "openai", base_url: "https://api.deepseek.com/v1",  enabled: true,  credential_configured: false },
    { id: "gemini",    name: "gemini",    kind: "cloud", protocol: "openai", base_url: "https://generativelanguage.googleapis.com/v1beta/openai", enabled: true, credential_configured: false },
    { id: "groq",      name: "groq",      kind: "cloud", protocol: "openai", base_url: "https://api.groq.com/openai/v1", enabled: true, credential_configured: false },
    { id: "mistral",   name: "mistral",   kind: "cloud", protocol: "openai", base_url: "https://api.mistral.ai/v1",     enabled: true, credential_configured: false },
    { id: "openai",    name: "openai",    kind: "cloud", protocol: "openai", base_url: "https://api.openai.com/v1",     enabled: true, credential_configured: true,  credential_source: "vault" },
    { id: "together_ai", name: "together_ai", kind: "cloud", protocol: "openai", base_url: "https://api.together.xyz/v1", enabled: true, credential_configured: false },
    { id: "xai",       name: "xai",       kind: "cloud", protocol: "openai", base_url: "https://api.x.ai/v1",           enabled: true, credential_configured: false },
    { id: "llamacpp",  name: "llamacpp",  kind: "local", protocol: "openai", base_url: "http://127.0.0.1:8080/v1",      enabled: true,  credential_configured: false },
    { id: "lmstudio",  name: "lmstudio",  kind: "local", protocol: "openai", base_url: "http://127.0.0.1:1234/v1",      enabled: true,  credential_configured: false },
    { id: "localai",   name: "localai",   kind: "local", protocol: "openai", base_url: "http://127.0.0.1:8080/v1",      enabled: false, credential_configured: false },
    { id: "ollama",    name: "ollama",    kind: "local", protocol: "openai", base_url: "http://127.0.0.1:11434/v1",     enabled: true,  credential_configured: false },
  ],
  tenants: [],
  api_keys: [],
  policy_rules: [],
};

export const MOCK_FULL_PRESETS = [
  ...MOCK_PRESETS,
  { id: "deepseek",  name: "DeepSeek",  kind: "cloud", protocol: "openai", base_url: "https://api.deepseek.com/v1",   description: "DeepSeek hosted models." },
  { id: "gemini",    name: "Google Gemini", kind: "cloud", protocol: "openai", base_url: "https://generativelanguage.googleapis.com/v1beta/openai", description: "Google Gemini." },
  { id: "groq",      name: "Groq",      kind: "cloud", protocol: "openai", base_url: "https://api.groq.com/openai/v1", description: "Groq inference." },
  { id: "mistral",   name: "Mistral",   kind: "cloud", protocol: "openai", base_url: "https://api.mistral.ai/v1",     description: "Mistral hosted models." },
  { id: "together_ai", name: "Together AI", kind: "cloud", protocol: "openai", base_url: "https://api.together.xyz/v1", description: "Together AI hosted models." },
  { id: "xai",       name: "xAI",       kind: "cloud", protocol: "openai", base_url: "https://api.x.ai/v1",           description: "xAI Grok models." },
  { id: "lmstudio",  name: "LM Studio", kind: "local", protocol: "openai", base_url: "http://127.0.0.1:1234/v1",      description: "Local inference via LM Studio." },
  { id: "localai",   name: "LocalAI",   kind: "local", protocol: "openai", base_url: "http://127.0.0.1:8080/v1",      description: "Local inference via LocalAI." },
];

// ── Route mocking ─────────────────────────────────────────────────────────────

export async function mockGatewayAPIs(page: Page) {
  const ok = (body: unknown) => ({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify(body),
  });

  await page.route("/healthz", r => r.fulfill(ok({ status: "ok", time: "2026-04-25T00:00:00Z" })));

  // Two-phase dashboard load gates admin-only fetches behind an authenticated
  // admin role. Anonymous → no /v1/models, /admin/control-plane, etc., so most
  // specs would render empty shells. Claim admin so all endpoints fire.
  await page.route("/v1/whoami", r =>
    r.fulfill(ok({
      object: "session",
      data: {
        authenticated: true,
        invalid_token: false,
        role: "admin",
        tenant: "",
        source: "bearer",
        key_id: "e2e-test-token",
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
    r.fulfill(ok({ object: "list", data: MOCK_FULL_PRESETS })),
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

  await page.route("/admin/mcp/cache*", r =>
    r.fulfill(ok({
      object: "mcp_cache_stats",
      data: { entries: 0, in_use: 0, idle: 0, max_entries: 0 },
    })),
  );

  await page.route("/admin/traces*", r =>
    r.fulfill(ok({ object: "list", data: [] })),
  );
}

// ── Extended test fixture ─────────────────────────────────────────────────────

// Seed a non-empty admin token in localStorage before any page script runs.
// AppShell's ConsoleShell routes to TokenGate when authToken is empty, so
// without this seed the workspace shell never renders and every spec that
// asserts on `.hecate-activitybar` (shell, chat, providers, admin) hangs in
// beforeEach. Tests that exercise the gate itself (auth.spec.ts) override
// this with their own `addInitScript` — multiple init scripts run in
// registration order, so a later `clear()` wins.
async function seedAdminToken(page: Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem("hecate.authToken", "e2e-test-token");
  });
}

export const test = base.extend<{ page: Page }>({
  page: async ({ page }, use) => {
    await seedAdminToken(page);
    await mockGatewayAPIs(page);
    await use(page);
  },
});

export { expect } from "@playwright/test";
