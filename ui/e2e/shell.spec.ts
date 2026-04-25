import { expect, test } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");
});

test("renders the activity bar with all workspace buttons", async ({ page }) => {
  const nav = page.locator(".hecate-activitybar");
  await expect(nav).toBeVisible();

  for (const label of ["Chat", "Observe", "Tasks", "Providers"]) {
    await expect(nav.locator(`[aria-label^="${label}"]`)).toBeVisible();
  }
});

test("shows the status bar with brand and session label", async ({ page }) => {
  const bar = page.locator(".hecate-statusbar");
  await expect(bar).toBeVisible();
  await expect(bar.locator(".hecate-statusbar__brand")).toHaveText("hecate");
  await expect(bar).toContainText("Anonymous");
});

test("status bar shows provider and model counts", async ({ page }) => {
  const bar = page.locator(".hecate-statusbar");
  // Wait for dashboard data to load
  await expect(bar).toContainText("providers");
  await expect(bar).toContainText("models");
});

test("keyboard shortcut 1 activates the Chat workspace", async ({ page }) => {
  await page.keyboard.press("2"); // navigate away first
  await page.keyboard.press("1");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Chat/,
  );
});

test("keyboard shortcut 2 activates the Observe workspace", async ({ page }) => {
  await page.keyboard.press("2");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Observe/,
  );
});

test("keyboard shortcut 4 activates the Providers workspace", async ({ page }) => {
  await page.keyboard.press("4");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Providers/,
  );
});

test("clicking a nav button switches the active workspace", async ({ page }) => {
  await page.locator(".hecate-activitybar [aria-label^='Observe']").click();
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute(
    "aria-label",
    /Observe/,
  );
});

test("does not show Admin nav button for anonymous session", async ({ page }) => {
  await expect(page.locator(".hecate-activitybar [aria-label^='Admin']")).not.toBeVisible();
});
