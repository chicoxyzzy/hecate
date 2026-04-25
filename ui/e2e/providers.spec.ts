import { expect, test, MOCK_ADMIN_CONFIG } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");
  await page.keyboard.press("4");
  // Wait for provider cards to render
  await page.waitForSelector("[data-testid='provider-card'], .btn", { timeout: 5000 });
});

test("shows Cloud providers and Local inference section headings", async ({ page }) => {
  await expect(page.getByText("Cloud providers", { exact: true })).toBeVisible();
  await expect(page.getByText("Local inference", { exact: true })).toBeVisible();
});

test("renders a card for each provider from mock data", async ({ page }) => {
  for (const name of ["Anthropic", "OpenAI"]) {
    await expect(page.locator(`text=${name}`).first()).toBeVisible();
  }
  for (const name of ["Ollama", "llama.cpp"]) {
    await expect(page.locator(`text=${name}`).first()).toBeVisible();
  }
});

test("cloud section shows enabled/total count", async ({ page }) => {
  const enabledCount = MOCK_ADMIN_CONFIG.providers.filter(p => p.enabled).length;
  await expect(page.locator("text=Cloud providers").locator("..")).toContainText(
    `${enabledCount}/`,
  );
});


test("selecting a configured provider opens the detail panel", async ({ page }) => {
  // Click the Anthropic card
  await page.locator("text=Anthropic").first().click();

  // Detail panel should appear on the right
  await expect(page.locator("text=Rotate API key")).toBeVisible();
  await expect(page.locator("text=Remove provider")).toBeVisible();
});

test("detail panel closes when the same card is clicked again", async ({ page }) => {
  const card = page.locator("text=Anthropic").first();
  await card.click();
  await expect(page.locator("text=Rotate API key")).toBeVisible();

  // Clicking the same card again deselects it
  await card.click();
  await expect(page.locator("text=Rotate API key")).not.toBeVisible();
});

test("base URL is shown on provider cards", async ({ page }) => {
  await expect(page.locator("text=api.anthropic.com").first()).toBeVisible();
  await expect(page.locator("text=api.openai.com").first()).toBeVisible();
});

test("provider toggle calls PATCH endpoint", async ({ page }) => {
  let patchURL = "";
  let patchBody = "";

  await page.route("/admin/control-plane/providers/*", async route => {
    if (route.request().method() === "PATCH") {
      patchURL = route.request().url();
      patchBody = route.request().postData() ?? "";
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({}) });
    } else {
      await route.continue();
    }
  });

  // Find the first provider toggle and click it
  const toggles = page.locator(".toggle-wrap");
  if (await toggles.count() > 0) {
    await toggles.first().click();
    expect(patchURL).toContain("/admin/control-plane/providers/");
    expect(patchBody).toContain("enabled");
  }
});
