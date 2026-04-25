import { expect, test, MOCK_MODELS, MOCK_PROVIDERS } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  // Chat is the default workspace
  await page.waitForSelector(".hecate-activitybar");
});

test("renders the message textarea and send button", async ({ page }) => {
  await expect(page.locator("textarea")).toBeVisible();
  await expect(page.locator("button[type='submit']")).toBeVisible();
});

test("send button is disabled when message is empty", async ({ page }) => {
  await page.locator("textarea").fill("");
  await expect(page.locator("button[type='submit']")).toBeDisabled();
});

test("send button becomes enabled when message has content", async ({ page }) => {
  await page.locator("textarea").fill("Hello");
  await expect(page.locator("button[type='submit']")).toBeEnabled();
});

test("model picker opens and lists models from mock data", async ({ page }) => {
  // Wait for models to load, then open the picker
  const modelBtn = page.locator("button", { hasText: /claude|gpt|model/i }).first();
  await modelBtn.click();

  for (const m of MOCK_MODELS) {
    await expect(page.locator(`.dropdown-menu`)).toContainText(m.id);
  }
});

test("model picker filters by search input", async ({ page }) => {
  const modelBtn = page.locator("button", { hasText: /claude|gpt|model/i }).first();
  await modelBtn.click();

  const menu = page.locator(".dropdown-menu");
  await menu.locator("input").fill("gpt");

  await expect(menu).toContainText("gpt-4o");
  await expect(menu).not.toContainText("claude");
});

test("selecting a model closes the picker and updates the button label", async ({ page }) => {
  const modelBtn = page.locator("button", { hasText: /claude|gpt|model/i }).first();
  await modelBtn.click();

  await page.locator(".dropdown-menu").locator("text=gpt-4o-mini").first().click();

  await expect(page.locator(".dropdown-menu")).not.toBeVisible();
  await expect(modelBtn).toContainText("gpt-4o-mini");
});

test("provider picker shows healthy providers", async ({ page }) => {
  const healthyProviders = MOCK_PROVIDERS.filter(p => p.healthy);
  const providerBtn = page.locator("button", { hasText: /any provider/i });
  await providerBtn.click();

  const menu = page.locator(".dropdown-menu").first();
  for (const p of healthyProviders) {
    await expect(menu).toContainText(p.name, { ignoreCase: true });
  }
});

test("provider picker does not show unhealthy providers", async ({ page }) => {
  const unhealthyProviders = MOCK_PROVIDERS.filter(p => !p.healthy);
  const providerBtn = page.locator("button", { hasText: /any provider/i });
  await providerBtn.click();

  const menu = page.locator(".dropdown-menu").first();
  for (const p of unhealthyProviders) {
    await expect(menu).not.toContainText(p.name, { ignoreCase: true });
  }
});

test("New session button clears the active conversation", async ({ page }) => {
  // Fill the message box so we can verify the state resets
  await page.locator("textarea").fill("some prior message");
  await page.locator("button", { hasText: /new session/i }).click();
  // After starting a new session, the empty-state message is shown
  await expect(page.locator("text=Send a message to start a conversation.")).toBeVisible();
});

test("system prompt editor opens and closes", async ({ page }) => {
  const systemBtn = page.locator("button", { hasText: /system/i });
  await systemBtn.click();
  await expect(page.getByText("SYSTEM PROMPT")).toBeVisible();
  await expect(page.locator("textarea").nth(1)).toBeVisible();

  await systemBtn.click();
  await expect(page.getByText("SYSTEM PROMPT")).not.toBeVisible();
});
