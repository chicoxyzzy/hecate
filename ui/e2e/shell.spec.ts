import { expect, test } from "./fixtures";

// The default fixture claims admin; tests that assert anonymous-session
// behaviour register this override before navigating (last-registered
// route wins for the same URL pattern).
async function asAnonymous(page: import("@playwright/test").Page) {
  await page.route("/v1/whoami", r =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        object: "session",
        data: { authenticated: false, invalid_token: false, role: "anonymous", tenant: "", source: "", key_id: "" },
      }),
    }),
  );
}

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");
});

test("renders the activity bar with all workspace buttons", async ({ page }) => {
  const nav = page.locator(".hecate-activitybar");
  await expect(nav).toBeVisible();

  for (const label of ["Chats", "Observability", "Tasks", "Providers", "Costs", "Settings"]) {
    await expect(nav.locator(`[aria-label^="${label}"]`)).toBeVisible();
  }
});

test("shows the status bar with brand and session label", async ({ page }) => {
  await asAnonymous(page);
  await page.reload();
  await page.waitForSelector(".hecate-activitybar");
  const bar = page.locator(".hecate-statusbar");
  await expect(bar).toBeVisible();
  await expect(bar.locator(".hecate-statusbar__brand")).toHaveText("hecate");
  await expect(bar).toContainText("Anonymous");
});

test("status bar shows configured provider count and model count", async ({ page }) => {
  const bar = page.locator(".hecate-statusbar");
  // Wait for dashboard data to load
  await expect(bar).toContainText("configured");
  await expect(bar).toContainText("models");
});

test("keyboard shortcut 1 activates the Chats workspace", async ({ page }) => {
  await page.keyboard.press("2"); // navigate away first (Providers in admin lineup)
  await page.keyboard.press("1");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Chat/,
  );
});

test("keyboard shortcut 2 activates the Providers workspace", async ({ page }) => {
  await page.keyboard.press("2");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Providers/,
  );
});

test("keyboard shortcut 4 activates the Observability workspace", async ({ page }) => {
  await page.keyboard.press("4");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Observability/,
  );
});

test("clicking a nav button switches the active workspace", async ({ page }) => {
  await page.locator(".hecate-activitybar [aria-label^='Observability']").click();
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Observability/,
  );
});

test("keyboard shortcut 5 activates the Costs workspace", async ({ page }) => {
  await page.keyboard.press("5");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Costs/,
  );
});

test("keyboard shortcut 6 activates the Settings workspace", async ({ page }) => {
  await page.keyboard.press("6");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Settings/,
  );
});

test("does not show Settings nav button for anonymous session", async ({ page }) => {
  await asAnonymous(page);
  await page.reload();
  await page.waitForSelector(".hecate-activitybar");
  await expect(page.locator(".hecate-activitybar [aria-label^='Settings']")).not.toBeVisible();
});
