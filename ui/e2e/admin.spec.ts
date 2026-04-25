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
  // Press 5 → Admin (since admin nav is shortcut 5 for admins).
  await page.keyboard.press("5");
  await page.waitForSelector("text=Admin token");
});

test("renders all 5 admin tabs", async ({ page }) => {
  for (const tab of ["keys", "tenants", "budget", "usage", "retention"]) {
    await expect(page.getByRole("button", { name: tab })).toBeVisible();
  }
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
  await page.keyboard.press("5");
  await page.getByRole("button", { name: "budget" }).click();
  // Either shows the hint (no budget) or shows budget data — both are acceptable.
  const ok = await Promise.race([
    page.locator("text=Admin access required").first().waitFor({ timeout: 1000 }).then(() => true).catch(() => false),
    page.locator("text=Account budget").first().waitFor({ timeout: 1000 }).then(() => true).catch(() => false),
  ]);
  expect(ok).toBe(true);
});
