import { expect, test } from "./fixtures";

// End-to-end UI flow: an empty workspace blocks chat behind a placeholder.
// Adding a provider unblocks it. Deleting the only provider blocks it again.
// Pure UI — relies on the stateful create/delete mocks in fixtures.ts.

test("adding a provider unblocks chat; deleting it blocks chat again", async ({ page }) => {
  page.on("dialog", d => void d.accept());

  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");

  // Default fixture starts empty — chat workspace shows the placeholder.
  await expect(page.getByText("No providers configured")).toBeVisible();

  // Move to Providers via the activity bar shortcut and add Ollama.
  await page.keyboard.press("2");
  await page.waitForSelector("text=Providers");
  await page.getByRole("button", { name: /add provider/i }).first().click();
  const dlg = page.getByRole("dialog");
  await dlg.getByRole("button", { name: "Local", exact: true }).click();
  await dlg.getByText("Ollama", { exact: true }).click();
  await dlg.getByRole("button", { name: "Add provider", exact: true }).click();
  await expect(page.locator("tbody tr", { hasText: "Ollama" })).toBeVisible();

  // Switch to Chats — the placeholder must be gone now that one provider exists.
  await page.keyboard.press("1");
  await expect(page.getByText("No providers configured")).not.toBeVisible();
  await expect(page.locator("textarea")).toBeVisible();

  // Back to Providers, delete the row.
  await page.keyboard.press("2");
  await page.waitForSelector("text=Providers");
  await page.getByTitle("Remove Ollama").click();
  await expect(page.locator("tbody tr", { hasText: "Ollama" })).toHaveCount(0);

  // Chats — placeholder is back.
  await page.keyboard.press("1");
  await expect(page.getByText("No providers configured")).toBeVisible();
});
