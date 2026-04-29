// Capture documentation screenshots against a running gateway on :8080.
//
// Run via the bun script (resolves its own cwd, no `cd ui` needed):
//   bun run capture-screenshots          # from ui/
//   make screenshots                     # from repo root
//
// Prerequisites:
//   1. `make reset-dev && ./hecate &` — gateway running on :8080 with fresh state
//   2. ollama running on :11434 with `ollama pull llama3.1:8b` (used to seed
//      one realistic chat session). Set HECATE_SKIP_OLLAMA=1 to skip.
//
// Optional optimize pass — the script auto-detects the best PNG
// optimizer on PATH (preference: oxipng > pngquant > magick) and runs
// it over each captured PNG. None of these are required to take
// captures; the standard "people usually use this for README PNGs"
// install is `brew install oxipng`. Set HECATE_SKIP_OPTIMIZE=1 to skip.
//
// The admin token is read from .data/hecate.bootstrap.json. Outputs to
// docs/screenshots/<name>.png.

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

// 1280×800 is a comfortable docs-rendering size — wide enough to show
// the full sidebar + main pane with no horizontal scrolling, narrow
// enough that GitHub's README column doesn't have to downscale much.
// Reducing from 1440×900 trims ~25% of the pixel surface up front.
const VIEWPORT = { width: 1280, height: 800 };

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

async function openWorkspace(page: Page, id: "overview" | "runs" | "chats" | "providers" | "admin") {
  await page.evaluate((workspace) => {
    window.localStorage.setItem("hecate.workspace", workspace);
  }, id);
  await page.reload();
  await page.waitForSelector(".hecate-activitybar", { timeout: 5_000 });
}

// pngOptimizer is the build configuration for whichever PNG
// optimizer was detected on PATH. Preference order matches what most
// open-source repos use to optimize README screenshots:
//
//   1. pngquant — lossy palette quantization with Floyd-Steinberg
//      dithering. At quality=80-100 the output is perceptually
//      indistinguishable from the source, files shrink ~3-4×.
//      This is "the README screenshot optimizer".
//   2. oxipng — gold-standard lossless. Smaller wins (5-15%) but no
//      quality loss. Use this if you're allergic to lossy.
//   3. magick — last-resort lossless re-encode. Marginal at best,
//      sometimes a no-op.
type PNGOptimizer = { name: string; args: (path: string) => string[]; lossy: boolean };

function detectOptimizer(): PNGOptimizer | null {
  const candidates: PNGOptimizer[] = [
    {
      name: "pngquant",
      // Lossy palette quantization with dithering. Quality range
      // 80-100 means: aim for ≥80% perceptual quality, fail if it
      // can't reach 100. --speed 1 = max compression effort. The
      // .png ext + --force makes it overwrite the input in place.
      args: path => ["--quality=80-100", "--speed", "1", "--strip", "--ext", ".png", "--force", path],
      lossy: true,
    },
    {
      name: "oxipng",
      // -o max enables every optimization pass; --strip safe drops
      // metadata that's safe to remove (timestamps, EXIF) without
      // touching color data; in-place via the bare path arg.
      args: path => ["-o", "max", "--strip", "safe", path],
      lossy: false,
    },
    {
      name: "magick",
      args: path => [path, "-strip", "-define", "png:compression-level=9", path],
      lossy: false,
    },
  ];
  for (const c of candidates) {
    const probe = spawnSync(c.name, ["--version"], { stdio: "ignore" });
    if (probe.status === 0 || probe.status === 1) return c; // some tools return 1 on --version
  }
  return null;
}

async function optimize() {
  if (process.env.HECATE_SKIP_OPTIMIZE === "1") {
    console.log("→ skipping optimize (HECATE_SKIP_OPTIMIZE=1)");
    return;
  }
  const tool = detectOptimizer();
  if (!tool) {
    console.log("→ no PNG optimizer found on PATH (checked pngquant, oxipng, magick)");
    console.log("  install one for ~3-4× smaller files — recommended: `brew install pngquant`");
    return;
  }
  console.log(`→ optimizing PNGs (${tool.name}, ${tool.lossy ? "lossy palette" : "lossless"})`);
  // Each PNG is independent — run the optimizer in parallel. With docs
  // captures this drops a serial 2-3s pngquant pass to ~0.5s.
  const { spawn } = await import("node:child_process");
  await Promise.all(captured.map(path => new Promise<void>(resolve => {
    const before = statSync(path).size;
    const child = spawn(tool.name, tool.args(path), { stdio: ["ignore", "ignore", "pipe"] });
    let stderr = "";
    child.stderr?.on("data", chunk => { stderr += chunk.toString(); });
    child.on("close", code => {
      if (code !== 0) {
        console.warn(`  ${path.split("/").pop()}: ${tool.name} failed (${stderr.trim() || `exit ${code}`}); leaving original`);
        resolve();
        return;
      }
      const after = statSync(path).size;
      const delta = before - after;
      const pct = ((delta / before) * 100).toFixed(0);
      console.log(`  ${path.split("/").pop()}: ${(before / 1024).toFixed(1)} KB → ${(after / 1024).toFixed(1)} KB (-${pct}%)`);
      resolve();
    });
  })));
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
  // Ollama is optional — if it's not running or doesn't have the
  // model, we skip the chat turn and the chat screenshot just shows
  // the empty session. Set HECATE_SKIP_OLLAMA=1 to skip explicitly.
  const firstID = ids[0];
  if (process.env.HECATE_SKIP_OLLAMA === "1") {
    console.log("  HECATE_SKIP_OLLAMA=1 — leaving the chat session empty");
    return { firstID };
  }
  console.log(`  routing one chat through ollama/llama3.1:8b for ${firstID}…`);
  const start = Date.now();
  try {
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
      console.warn(`  chat seed skipped: ${chatRes.status} ${body.slice(0, 200)}`);
      console.warn("  (the chat screenshot will show an empty session)");
      return { firstID };
    }
    console.log(`  llama replied in ${((Date.now() - start) / 1000).toFixed(1)}s`);
  } catch (err) {
    console.warn(`  chat seed skipped: ${(err as Error).message}`);
    console.warn("  (the chat screenshot will show an empty session)");
  }
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
  await openWorkspace(page, "providers");
  await page.waitForSelector("text=Cloud providers", { timeout: 5_000 });
  await snap(page, "providers");

  console.log("→ provider setup");
  await page.getByText("OpenAI").first().click();
  await page.waitForSelector("text=API Key", { timeout: 5_000 });
  await page.locator("input[type='password']").fill("sk-live-••••••••••••••••••••");
  await page.waitForTimeout(300);
  await snap(page, "provider-setup");

  console.log("→ admin / pricebook");
  await openWorkspace(page, "admin");
  await page.getByRole("button", { name: /pricebook/i }).click();
  await page.waitForTimeout(800);
  await snap(page, "admin-pricebook");

  console.log("→ admin / budget");
  await page.getByRole("button", { name: /budget/i }).click();
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
  await openWorkspace(page, "admin");
  await page.getByRole("button", { name: /tenants/i }).click();
  await page.waitForTimeout(500);
  await snap(page, "admin-tenants");

  console.log("→ admin / keys");
  await page.getByRole("button", { name: /keys/i }).click();
  await page.waitForTimeout(500);
  await snap(page, "admin-keys");

  console.log("→ admin / integrations");
  await page.getByRole("button", { name: /integrations/i }).click();
  await page.waitForTimeout(400);
  await snap(page, "admin-integrations");

  console.log("→ observe");
  await openWorkspace(page, "overview");
  await page.waitForTimeout(800);
  await snap(page, "observe");

  console.log("→ tasks (runs workspace)");
  // Seed a task so the panel has at least one row instead of the
  // empty-state placeholder. Reload so TasksView's first fetch picks
  // it up.
  await seedTask();
  await page.reload();
  await page.waitForSelector(".hecate-activitybar", { timeout: 5_000 });
  await openWorkspace(page, "runs");
  await page.waitForTimeout(800);
  await snap(page, "tasks");

  await browser.close();
  await optimize();
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
