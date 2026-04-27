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
  const providerBtn = page.locator("button", { hasText: /all providers/i });
  await providerBtn.click();

  const menu = page.locator(".dropdown-menu").first();
  for (const p of healthyProviders) {
    await expect(menu).toContainText(p.name, { ignoreCase: true });
  }
});

test("provider picker does not show unhealthy providers", async ({ page }) => {
  const unhealthyProviders = MOCK_PROVIDERS.filter(p => !p.healthy);
  const providerBtn = page.locator("button", { hasText: /all providers/i });
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

test("Enter-switch toggle is visible in the input toolbar and clickable", async ({ page }) => {
  // The label is one of "↵ to send" or "⌘+↵ to send" / "Ctrl+↵ to send" depending on OS.
  const toggle = page.locator("button").filter({ hasText: /↵ to send/ });
  await expect(toggle).toBeVisible();
  const before = await toggle.textContent();
  await toggle.click();
  // After click, label should change.
  await expect(toggle).not.toHaveText(before ?? "");
});

test("Enter-switch preference persists across reload via localStorage", async ({ page }) => {
  const toggle = page.locator("button").filter({ hasText: /↵ to send/ });
  const initial = await toggle.textContent();
  await toggle.click();
  const after = await toggle.textContent();
  expect(after).not.toBe(initial);

  await page.reload();
  await page.waitForSelector(".hecate-activitybar");
  const reloaded = page.locator("button").filter({ hasText: /↵ to send/ });
  await expect(reloaded).toHaveText(after ?? "");
});

test("workspace selection persists across reload", async ({ page }) => {
  await page.keyboard.press("4"); // Providers
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute("aria-label", /Providers/);

  await page.reload();
  await page.waitForSelector(".hecate-activitybar");
  await expect(page.locator(".hecate-activitybar [aria-current='page']")).toHaveAttribute("aria-label", /Providers/);
});

// A failing /v1/chat/completions surfaces in two places: the inline
// error banner inside the chat view (next to the input), and a toast
// at the page level so an operator with their attention on a sidebar
// (admin, observe) doesn't miss it. The unit test in
// useRuntimeConsole.test.tsx pins the state shape; this e2e proves
// both surfaces actually render in a real DOM.
test("chat error surfaces in toast and inline banner", async ({ page }) => {
  await page.route("/v1/chat/sessions", r =>
    r.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        object: "chat_session",
        data: {
          id: "chat_err_e2e",
          title: "x",
          turns: [],
          created_at: "2026-04-21T00:00:00Z",
          updated_at: "2026-04-21T00:00:00Z",
        },
      }),
    }),
  );
  await page.route("/v1/chat/completions", r =>
    r.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({
        error: {
          type: "gateway_error",
          message: "api key is required for cloud provider anthropic when stub mode is disabled",
        },
      }),
    }),
  );

  await page.locator("textarea").first().fill("hello");
  await page.locator("button[type='submit']").click();

  // Toast: pinned visible with the error class so global notice surface
  // works even when the user's eyes aren't on the chat pane.
  const toast = page.locator(".toast.toast--error");
  await expect(toast).toBeVisible();
  await expect(toast).toContainText(/api key is required/i);
  // No leaked classification prefix from the backend (UserFacingMessage
  // strips this in the gateway). Locks in the contract on the wire.
  await expect(toast).not.toContainText(/^client error: /i);

  // Inline banner under the chat header carries the same message —
  // belt-and-braces so chat-context users see it without scanning.
  await expect(page.getByText(/api key is required/i).first()).toBeVisible();
});
