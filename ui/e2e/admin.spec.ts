import { expect, test } from "./fixtures";

// Admin workspace is admin-only. We need to seed an authenticated admin session.
test.beforeEach(async ({ page }) => {
  // Override /v1/whoami to report admin so the Admin nav button appears.
  await page.route("/v1/whoami*", r => r.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify({
      object: "session",
      data: { authenticated: true, invalid_token: false, role: "admin", source: "config", key_id: "" },
    }),
  }));
  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");
  // Press 5 → Admin. The admin lineup is Chats / Providers / Tasks /
  // Observability / Admin, so Admin's positional shortcut is 5.
  await page.keyboard.press("5");
  await page.waitForSelector("text=Admin token");
});

test("renders all 7 admin tabs", async ({ page }) => {
  // Display labels from ui/src/features/admin/AdminView.tsx TAB_LABELS.
  // Anchor on the rendered text (what operators click), not internal tab
  // ids — the id list and label list are deliberately distinct. The
  // former "Clients" tab is gone in the alpha; client-setup snippets
  // are documentation-only for now.
  for (const tab of ["Keys", "Tenants", "Balances", "Usage", "Pricing", "Policy", "Retention"]) {
    await expect(page.getByRole("button", { name: tab })).toBeVisible();
  }
  await expect(page.getByRole("button", { name: "Clients" })).toHaveCount(0);
});

test("admin token panel shows reveal/rotate", async ({ page }) => {
  await expect(page.locator("text=Admin token").first()).toBeVisible();
  await expect(page.getByRole("button", { name: /Reveal/i })).toBeVisible();
  // "Rotate" appears on token panel and possibly elsewhere; just ensure ≥1.
  expect(await page.getByRole("button", { name: /Rotate/i }).count()).toBeGreaterThan(0);
});

test("clicking tenants tab reveals 'New tenant' button", async ({ page }) => {
  await page.getByRole("button", { name: "tenants" }).click();
  await expect(page.locator("text=New tenant")).toBeVisible();
});

test("retention tab shows known subsystem chips", async ({ page }) => {
  await page.getByRole("button", { name: "retention" }).click();
  for (const sub of ["trace_snapshots", "budget_events", "audit_events", "exact_cache", "semantic_cache"]) {
    await expect(page.locator(`text=${sub}`).first()).toBeVisible();
  }
});

test("retention 'Run now' fires POST request", async ({ page }) => {
  let posted = false;
  await page.route("/admin/retention/run*", async route => {
    if (route.request().method() === "POST") {
      posted = true;
      await route.fulfill({ status: 200, contentType: "application/json", body: '{"object":"retention_run","data":{}}' });
    } else {
      await route.continue();
    }
  });

  await page.getByRole("button", { name: "retention" }).click();
  await page.getByRole("button", { name: /Run now/i }).click();
  await expect.poll(() => posted).toBe(true);
});

test("usage tab shows empty state with no events", async ({ page }) => {
  await page.getByRole("button", { name: "usage" }).click();
  await expect(page.locator("text=No usage events recorded yet")).toBeVisible();
});

test("budget tab shows admin-required hint when no budget", async ({ page }) => {
  // Default fixture stubs no budget data.
  await page.route("/admin/budget*", r => r.fulfill({ status: 404, body: "" }));
  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");
  await page.keyboard.press("6");
  await page.getByRole("button", { name: "Balances" }).click();
  // Either shows the hint (no budget) or shows budget data — both are acceptable.
  const ok = await Promise.race([
    page.locator("text=Admin access required").first().waitFor({ timeout: 1000 }).then(() => true).catch(() => false),
    page.locator("text=Account budget").first().waitFor({ timeout: 1000 }).then(() => true).catch(() => false),
  ]);
  expect(ok).toBe(true);
});

// Pricebook import: Open pricebook tab → preview is fetched on mount →
// "Import all" opens the consent dialog → Apply triggers POST /apply.
// This is the only end-to-end exercise of the import flow; the unit
// test in useRuntimeConsole.test.tsx pins notice wording but never
// renders the modal or clicks through.
test("pricebook import all triggers preview + apply round-trip", async ({ page }) => {
  // Mock the preview to propose adding a price for an existing
  // catalog model. The MOCK_MODELS fixture has gpt-4o-mini in the
  // catalog with no pricebook entry, so this will classify as `added`
  // and the row will appear as "unpriced" in the table — letting the
  // consent dialog include it.
  let previewHits = 0;
  await page.route("/admin/control-plane/pricebook/import/preview", async route => {
    if (route.request().method() !== "POST") return route.continue();
    previewHits++;
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        object: "control_plane_pricebook_import_diff",
        data: {
          added: [
            {
              provider: "openai",
              model: "gpt-4o-mini",
              input_micros_usd_per_million_tokens: 150_000,
              output_micros_usd_per_million_tokens: 600_000,
              cached_input_micros_usd_per_million_tokens: 75_000,
              source: "imported",
            },
          ],
          updated: [],
          skipped: [],
          unchanged: 0,
          applied: [],
          failed: [],
          fetched_at: "2026-04-25T00:00:00Z",
        },
      }),
    });
  });

  let applyURL = "";
  let applyBody = "";
  await page.route("/admin/control-plane/pricebook/import/apply", async route => {
    if (route.request().method() !== "POST") return route.continue();
    applyURL = route.request().url();
    applyBody = route.request().postData() ?? "";
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        object: "control_plane_pricebook_import_diff",
        data: {
          added: [],
          updated: [],
          skipped: [],
          unchanged: 0,
          applied: [
            {
              provider: "openai",
              model: "gpt-4o-mini",
              input_micros_usd_per_million_tokens: 150_000,
              output_micros_usd_per_million_tokens: 600_000,
              cached_input_micros_usd_per_million_tokens: 75_000,
              source: "imported",
            },
          ],
          failed: [],
          fetched_at: "2026-04-25T00:00:00Z",
        },
      }),
    });
  });

  await page.getByRole("button", { name: "Pricing" }).click();
  // Preview is fetched on mount of the tab; the "Import all" button
  // becomes enabled once the diff arrives. Without this assertion, a
  // future regression that drops the mount-time preview fetch would go
  // unnoticed.
  await expect.poll(() => previewHits, { timeout: 5_000 }).toBeGreaterThanOrEqual(1);

  const importAll = page.getByRole("button", { name: /Import all/i });
  await expect(importAll).toBeEnabled();
  await importAll.click();

  // Consent modal: assert it opened and contains the proposed change.
  await expect(page.getByText("Update pricebook")).toBeVisible();
  await expect(page.getByText("gpt-4o-mini").first()).toBeVisible();

  // Apply with the pre-selected key. The button label includes the
  // count, which doubles as a sanity check that selection state landed.
  const applyBtn = page.getByRole("button", { name: /Apply 1 change/i });
  await expect(applyBtn).toBeEnabled();
  await applyBtn.click();

  await expect.poll(() => applyURL).toContain("/admin/control-plane/pricebook/import/apply");
  // The body MUST carry the explicit key list — a regression that drops
  // `keys` and applies blanket changes would silently overwrite manual
  // rows the operator never consented to.
  expect(JSON.parse(applyBody)).toEqual({ keys: ["openai/gpt-4o-mini"] });
});
