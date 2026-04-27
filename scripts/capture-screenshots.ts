// Capture documentation screenshots against a running gateway on :8080.
//
// Run with:  bun run /Users/chicoxyzzy/dev/hecate/scripts/capture-screenshots.ts
// (must run from a directory where Playwright is resolvable, e.g. `cd ui` first)
//
// Prerequisites:
//   1. `make reset-dev && ./hecate &` — gateway running on :8080 with fresh state
//   2. ollama running on :11434 with `ollama pull llama3.1:8b` (used to seed
//      one realistic chat session). Set HECATE_SKIP_OLLAMA=1 to skip.
//   3. ImageMagick (`magick`) on PATH for the post-capture optimize pass.
//      Set HECATE_SKIP_OPTIMIZE=1 to skip.
//
// The admin token is read from .data/hecate.bootstrap.json. Outputs to
// docs/screenshots/<name>.png. Each scene navigates to a route,
// optionally interacts to surface the right state, then snapshots; the
// optimize pass runs a `magick -strip -colors 256 -define
// png:compression-level=9` over each PNG (≈3-4× size reduction with no
// visible loss on dark UI screenshots).

import { chromium, type Page } from "@playwright/test";
import { readFileSync, mkdirSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { spawnSync } from "node:child_process";

const BASE_URL = process.env.HECATE_URL ?? "http://127.0.0.1:8080";
const OUT_DIR = resolve(import.meta.dirname, "..", "docs", "screenshots");
mkdirSync(OUT_DIR, { recursive: true });

const bootstrap = JSON.parse(
  readFileSync(resolve(import.meta.dirname, "..", ".data", "hecate.bootstrap.json"), "utf8"),
) as { admin_token: string };
const ADMIN_TOKEN = bootstrap.admin_token;

const VIEWPORT = { width: 1440, height: 900 };

async function clearAndNavigate(page: Page, path = "/") {
  await page.context().clearCookies();
  await page.goto(BASE_URL);
  await page.evaluate(() => window.localStorage.clear());
  await page.goto(`${BASE_URL}${path}`);
}

async function signIn(page: Page) {
  await page.evaluate((token) => {
    window.localStorage.setItem("hecate.authToken", token);
  }, ADMIN_TOKEN);
  await page.reload();
  await page.waitForSelector(".hecate-activitybar", { timeout: 10_000 });
}

const captured: string[] = [];

async function snap(page: Page, name: string) {
  const path = resolve(OUT_DIR, `${name}.png`);
  await page.screenshot({ path, fullPage: false });
  captured.push(path);
  console.log(`  saved ${path}`);
}

// optimize runs ImageMagick over each captured PNG to strip metadata,
// quantize the palette to 256 colors (no visible loss on dark UI
// screenshots, which have << 256 distinct colors), and re-encode at
// max compression. Typical reduction is 3-4× on the dashboard
// screenshots — drops the docs/screenshots total from ~500 KB to
// ~180 KB.
function optimize() {
  if (process.env.HECATE_SKIP_OPTIMIZE === "1") {
    console.log("→ skipping optimize (HECATE_SKIP_OPTIMIZE=1)");
    return;
  }
  console.log("→ optimizing PNGs (magick -strip -colors 256)");
  for (const path of captured) {
    const before = statSync(path).size;
    const result = spawnSync("magick", [
      path,
      "-strip",
      "-colors", "256",
      "-define", "png:compression-level=9",
      "-define", "png:compression-filter=5",
      path,
    ], { stdio: ["ignore", "pipe", "pipe"] });
    if (result.error) {
      console.warn(`  ${path}: magick failed (${result.error.message}); leaving original`);
      continue;
    }
    if (result.status !== 0) {
      console.warn(`  ${path}: magick exited ${result.status}: ${result.stderr.toString().trim()}`);
      continue;
    }
    const after = statSync(path).size;
    const reduction = ((1 - after / before) * 100).toFixed(0);
    console.log(`  ${path.split("/").pop()}: ${(before / 1024).toFixed(1)} KB → ${(after / 1024).toFixed(1)} KB (-${reduction}%)`);
  }
}

// seedChatSessions creates a few chat sessions through Hecate's API so
// the sidebar isn't empty. The first session also gets a real
// completion so the chat pane renders an assistant turn — without that
// the screenshot is just an empty "Send a message…" placeholder.
async function seedChatSessions() {
  const headers = {
    "Content-Type": "application/json",
    "Authorization": `Bearer ${ADMIN_TOKEN}`,
  };

  // Session titles are taken from POST body; the gateway uses them as
  // sidebar labels until the first turn lands.
  const titles = [
    "Go interfaces vs structs",
    "Postgres logical replication",
    "Sort TS array without mutating",
  ];
  const ids: string[] = [];
  for (const title of titles) {
    const res = await fetch(`${BASE_URL}/v1/chat/sessions`, {
      method: "POST",
      headers,
      body: JSON.stringify({ title }),
    });
    const json = (await res.json()) as { data: { id: string } };
    ids.push(json.data.id);
    console.log(`  seeded session ${json.data.id} — ${title}`);
  }

  // Real completion against llama3.1:8b for the first session. Keep
  // the prompt short so the model finishes in seconds, not minutes.
  const firstID = ids[0];
  console.log(`  routing one chat through ollama/llama3.1:8b for ${firstID}…`);
  const start = Date.now();
  const chatRes = await fetch(`${BASE_URL}/v1/chat/completions`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      model: "llama3.1:8b",
      provider: "ollama",
      session_id: firstID,
      messages: [{
        role: "user",
        content: "In two sentences: when do you reach for a Go interface vs a struct?",
      }],
    }),
  });
  if (!chatRes.ok) {
    const body = await chatRes.text();
    throw new Error(`chat failed: ${chatRes.status} ${body}`);
  }
  console.log(`  llama replied in ${((Date.now() - start) / 1000).toFixed(1)}s`);
  return { firstID };
}

async function main() {
  const browser = await chromium.launch({ headless: true });
  // deviceScaleFactor: 1 keeps the PNG file size reasonable for an
  // open-source repo's docs/ — 2x makes the same screenshot ~3-4x
  // larger with little visible benefit at the sizes README renders.
  const context = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 1 });
  const page = await context.newPage();

  console.log("→ onboard-wizard");
  await clearAndNavigate(page);
  await page.waitForSelector("text=Admin token required", { timeout: 5_000 });
  await snap(page, "onboard-wizard");

  // Seed session content before signing the browser in, so the chat
  // sidebar already has rows on first paint.
  console.log("→ seeding chat sessions");
  const { firstID } = await seedChatSessions();

  console.log("→ chat (with seeded sessions, llama3.1:8b conversation)");
  await signIn(page);
  await page.waitForTimeout(500);
  // Click the seeded session by its title so the main pane renders
  // the user/assistant turn from the ollama completion above.
  await page.getByText("Go interfaces vs structs").first().click();
  await page.waitForTimeout(1500);
  await snap(page, "chat");

  console.log("→ providers");
  await page.keyboard.press("4");
  await page.waitForSelector("text=Cloud providers", { timeout: 5_000 });
  await snap(page, "providers");

  console.log("→ admin / pricebook");
  await page.keyboard.press("5");
  await page.waitForSelector("button:has-text('pricebook')", { timeout: 5_000 });
  await page.click("button:has-text('pricebook')");
  await page.waitForTimeout(800);
  await snap(page, "admin-pricebook");

  console.log("→ admin / budget");
  await page.click("button:has-text('budget')");
  await page.waitForTimeout(500);
  await snap(page, "admin-budget");

  console.log("→ admin / tenants");
  // Seed a couple of tenants + API keys before the snapshot so the
  // table has rows. Tenants & keys are control-plane resources; the
  // API matches what the UI fires when the operator clicks "New
  // tenant" or "Create API key". Reload after seeding so the
  // dashboard's loadDashboard() picks up the new rows — clicking a
  // tab alone doesn't refetch.
  await seedTenants();
  await page.reload();
  await page.waitForSelector(".hecate-activitybar", { timeout: 5_000 });
  await page.keyboard.press("5");
  await page.click("button:has-text('tenants')");
  await page.waitForTimeout(500);
  await snap(page, "admin-tenants");

  console.log("→ admin / keys");
  await page.click("button:has-text('keys')");
  await page.waitForTimeout(500);
  await snap(page, "admin-keys");

  console.log("→ admin / integrations");
  await page.click("button:has-text('integrations')");
  await page.waitForTimeout(400);
  await snap(page, "admin-integrations");

  console.log("→ observe");
  await page.keyboard.press("2");
  await page.waitForTimeout(800);
  await snap(page, "observe");

  console.log("→ tasks (runs workspace)");
  // Seed a task so the panel has at least one row instead of the
  // empty-state placeholder. Reload so TasksView's first fetch picks
  // it up.
  await seedTask();
  await page.reload();
  await page.waitForSelector(".hecate-activitybar", { timeout: 5_000 });
  await page.keyboard.press("3");
  await page.waitForTimeout(800);
  await snap(page, "tasks");

  await browser.close();
  optimize();
  console.log("done.");
}

async function seedTenants() {
  const headers = {
    "Content-Type": "application/json",
    "Authorization": `Bearer ${ADMIN_TOKEN}`,
  };
  const tenants = [
    { id: "team-a", name: "Team A" },
    { id: "team-b", name: "Team B" },
  ];
  for (const t of tenants) {
    await fetch(`${BASE_URL}/admin/control-plane/tenants`, {
      method: "POST",
      headers,
      body: JSON.stringify({ ...t, enabled: true }),
    });
  }
  // One API key per tenant — keep names + scopes representative.
  const keys = [
    { id: "team-a-prod", name: "team-a / prod", tenant: "team-a", role: "tenant",
      key: "sk-team-a-prod-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
      allowed_providers: ["openai", "anthropic"],
      allowed_models: [], enabled: true },
    { id: "team-b-staging", name: "team-b / staging", tenant: "team-b", role: "tenant",
      key: "sk-team-b-staging-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
      allowed_providers: ["ollama"],
      allowed_models: [], enabled: true },
  ];
  for (const k of keys) {
    await fetch(`${BASE_URL}/admin/control-plane/api-keys`, {
      method: "POST",
      headers,
      body: JSON.stringify(k),
    });
  }
  console.log(`  seeded ${tenants.length} tenants + ${keys.length} API keys`);
}

async function seedTask() {
  const headers = {
    "Content-Type": "application/json",
    "Authorization": `Bearer ${ADMIN_TOKEN}`,
  };
  const res = await fetch(`${BASE_URL}/v1/tasks`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      title: "Reproduce flaky integration test",
      prompt: "Run the integration suite three times back-to-back and surface any test that fails at least once.",
    }),
  });
  if (!res.ok) {
    console.warn(`  task seed failed: ${res.status}`);
    return;
  }
  const json = (await res.json()) as { data: { id: string } };
  console.log(`  seeded task ${json.data.id}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
