import { expect, test, MOCK_ADMIN_CONFIG } from "./fixtures";

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await page.waitForSelector(".hecate-activitybar");
  await page.keyboard.press("4");
  await page.waitForSelector("text=Cloud providers");
});

test("renders Cloud providers and Local inference section headings", async ({ page }) => {
  await expect(page.getByText("Cloud providers", { exact: true })).toBeVisible();
  await expect(page.getByText("Local inference", { exact: true })).toBeVisible();
});

test("renders all 12 built-in providers", async ({ page }) => {
  // Names use preset display names, not the raw ID.
  const cloudNames = ["Anthropic", "DeepSeek", "Google", "Groq", "Mistral", "OpenAI", "Together AI", "xAI"];
  const localNames = ["llama.cpp", "LM Studio", "LocalAI", "Ollama"];

  for (const name of [...cloudNames, ...localNames]) {
    await expect(page.locator(`text=${name}`).first()).toBeVisible();
  }
});

test("cloud section shows enabled/total count", async ({ page }) => {
  const cloud = MOCK_ADMIN_CONFIG.providers.filter(p => p.kind === "cloud");
  const enabled = cloud.filter(p => p.enabled).length;
  await expect(page.locator("text=Cloud providers").locator("..")).toContainText(`${enabled}/${cloud.length} enabled`);
});

test("local section shows enabled/total count", async ({ page }) => {
  const local = MOCK_ADMIN_CONFIG.providers.filter(p => p.kind === "local");
  const enabled = local.filter(p => p.enabled).length;
  // Use exact match — "Local inference" also appears in preset descriptions ("Local inference via …").
  const heading = page.getByText("Local inference", { exact: true });
  await expect(heading.locator("..")).toContainText(`${enabled}/${local.length} connected`);
});

test("conflicting local providers do not both appear enabled by default", async ({ page }) => {
  // llamacpp and localai both target 127.0.0.1:8080. Backend resolution: llamacpp wins.
  const llamacpp = page.getByRole("switch", { name: "Enable llama.cpp" });
  const localai  = page.getByRole("switch", { name: "Enable LocalAI" });

  await expect(llamacpp).toHaveAttribute("aria-checked", "true");
  await expect(localai).toHaveAttribute("aria-checked", "false");
});

test("conflicting providers display the warning indicator", async ({ page }) => {
  // Look for the amber ⚠ marker on the llama.cpp card (it conflicts with localai).
  const llamaCard = page.locator("text=llama.cpp").first().locator("..").locator("..");
  await expect(llamaCard.locator("text=⚠")).toBeVisible();
});

test("enabling a conflicting provider disables the others (full flow)", async ({ page }) => {
  // Stateful mock that mirrors real backend behaviour: a PATCH updates the in-memory
  // state and applies conflict resolution, so subsequent GETs return the new state.
  const config = JSON.parse(JSON.stringify(MOCK_ADMIN_CONFIG)) as typeof MOCK_ADMIN_CONFIG;

  await page.route("/admin/control-plane*", async route => {
    if (route.request().method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ object: "configured_state", data: config }),
      });
      return;
    }
    await route.continue();
  });

  await page.route("/admin/control-plane/providers/*", async route => {
    if (route.request().method() !== "PATCH") {
      await route.continue();
      return;
    }
    const id = decodeURIComponent(route.request().url().split("/").pop() ?? "");
    const body = JSON.parse(route.request().postData() ?? "{}");
    const target = config.providers.find(p => p.id === id);
    if (target) {
      target.enabled = body.enabled;
      // If we just enabled one, disable any other provider with the same base_url.
      if (body.enabled) {
        for (const p of config.providers) {
          if (p.id !== id && p.base_url === target.base_url) p.enabled = false;
        }
      }
    }
    await route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
  });

  // Reload so the new GET handler is used for the initial fetch too.
  await page.reload();
  await page.waitForSelector("text=Cloud providers");
  await page.keyboard.press("4");

  const llamacpp = page.getByRole("switch", { name: "Enable llama.cpp" });
  const localai  = page.getByRole("switch", { name: "Enable LocalAI" });

  // Initial: llamacpp on, localai off.
  await expect(llamacpp).toHaveAttribute("aria-checked", "true");
  await expect(localai).toHaveAttribute("aria-checked", "false");

  // Toggle localai on → backend disables llamacpp → eventual UI state has localai
  // enabled and llamacpp disabled.
  await localai.click();
  await expect(localai).toHaveAttribute("aria-checked", "true");
  await expect(llamacpp).toHaveAttribute("aria-checked", "false");
});

test("toggling off the conflict winner does not auto-enable the peer", async ({ page }) => {
  // Pins the user-reported flow: "toggling off one of conflicting providers
  // should not toggle on any other." The backend's resolveDefaultProviderConflicts
  // used to auto-promote the alphabetically-first peer when no provider in a
  // conflict group was explicitly enabled. That made llamacpp → off "promote"
  // localai to on visually, which read as the system enabling something the
  // operator hadn't asked for.
  //
  // Stateful mock: PATCH updates in-memory state without auto-promoting peers.
  // Same backend semantics now exist on the real server.
  const config = JSON.parse(JSON.stringify(MOCK_ADMIN_CONFIG)) as typeof MOCK_ADMIN_CONFIG;

  await page.route("/admin/control-plane*", async route => {
    if (route.request().method() === "GET") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ object: "configured_state", data: config }),
      });
      return;
    }
    await route.continue();
  });

  await page.route("/admin/control-plane/providers/*", async route => {
    if (route.request().method() !== "PATCH") {
      await route.continue();
      return;
    }
    const id = decodeURIComponent(route.request().url().split("/").pop() ?? "");
    const body = JSON.parse(route.request().postData() ?? "{}");
    const target = config.providers.find(p => p.id === id);
    if (target) {
      target.enabled = body.enabled;
      // Mirror the real backend: only auto-disable peers when ENABLING. On a
      // disable, leave every other provider's state alone — no promotion.
      if (body.enabled) {
        for (const p of config.providers) {
          if (p.id !== id && p.base_url === target.base_url) p.enabled = false;
        }
      }
    }
    await route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
  });

  await page.reload();
  await page.waitForSelector("text=Cloud providers");
  await page.keyboard.press("4");

  const llamacpp = page.getByRole("switch", { name: "Enable llama.cpp" });
  const localai  = page.getByRole("switch", { name: "Enable LocalAI" });

  // Pre-toggle state from MOCK_ADMIN_CONFIG: llamacpp on, localai off.
  await expect(llamacpp).toHaveAttribute("aria-checked", "true");
  await expect(localai).toHaveAttribute("aria-checked", "false");

  // Toggle llamacpp off. localai must stay off — neither is enabled now.
  await llamacpp.click();
  await expect(llamacpp).toHaveAttribute("aria-checked", "false");
  await expect(localai).toHaveAttribute("aria-checked", "false");
});

test("toggle calls PATCH endpoint with correct payload", async ({ page }) => {
  let patchURL = "";
  let patchBody = "";

  await page.route("/admin/control-plane/providers/*", async route => {
    if (route.request().method() === "PATCH") {
      patchURL = route.request().url();
      patchBody = route.request().postData() ?? "";
      await route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
    } else {
      await route.continue();
    }
  });

  await page.getByRole("switch", { name: "Enable Anthropic" }).click();
  await expect.poll(() => patchURL).toContain("/admin/control-plane/providers/anthropic");
  expect(JSON.parse(patchBody)).toEqual({ enabled: false });
});

test("clicking a cloud provider opens panel with API key input", async ({ page }) => {
  await page.locator("text=Anthropic").first().click();

  // Cloud panel shows a password input + the Save/Update button. Anthropic has a vault
  // credential configured so the placeholder is masked dots, and the action is "Update".
  await expect(page.locator("input[type='password']")).toBeVisible();
  await expect(page.locator("text=Update API key")).toBeVisible();
});

test("clicking a local provider opens panel with no API key input", async ({ page }) => {
  await page.locator("text=Ollama").first().click();

  // Local providers don't need credentials — no input or save/update button.
  await expect(page.locator("input[type='password']")).toHaveCount(0);
  await expect(page.locator("text=Save API key")).not.toBeVisible();
  await expect(page.locator("text=Update API key")).not.toBeVisible();
});

test("removed legacy controls are gone", async ({ page }) => {
  await page.locator("text=Anthropic").first().click();
  // Buttons that used to exist on cards/panel are removed.
  await expect(page.locator("text=Rotate API key")).not.toBeVisible();
  await expect(page.locator("text=Remove provider")).not.toBeVisible();
  await expect(page.locator("text=Test connection")).not.toBeVisible();
  // The "test" button on local cards is gone too.
  await page.keyboard.press("Escape");
});

test("saving an API key calls PUT /api-key with the new key", async ({ page }) => {
  let putURL = "";
  let putBody = "";

  await page.route("/admin/control-plane/providers/*/api-key", async route => {
    if (route.request().method() === "PUT") {
      putURL = route.request().url();
      putBody = route.request().postData() ?? "";
      await route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
    } else {
      await route.continue();
    }
  });

  await page.locator("text=Anthropic").first().click();
  const input = page.getByPlaceholder(/sk-|••••/);
  await input.fill("sk-new-key-value");
  await page.locator("text=Update API key").click();

  await expect.poll(() => putURL).toContain("/admin/control-plane/providers/anthropic/api-key");
  expect(JSON.parse(putBody)).toEqual({ key: "sk-new-key-value" });
});

test("clearing an API key calls PUT /api-key with empty key", async ({ page }) => {
  let putURL = "";
  let putBody = "";

  await page.route("/admin/control-plane/providers/*/api-key", async route => {
    if (route.request().method() === "PUT") {
      putURL = route.request().url();
      putBody = route.request().postData() ?? "";
      await route.fulfill({ status: 200, contentType: "application/json", body: "{}" });
    } else {
      await route.continue();
    }
  });

  // Anthropic has credential_configured + credential_source: "vault", so the
  // "Delete API key" button is visible.
  await page.locator("text=Anthropic").first().click();
  await page.locator("text=Delete API key").click();

  await expect.poll(() => putURL).toContain("/admin/control-plane/providers/anthropic/api-key");
  expect(JSON.parse(putBody)).toEqual({ key: "" });
});

test("base URL is shown on provider cards", async ({ page }) => {
  await expect(page.locator("text=api.anthropic.com").first()).toBeVisible();
  await expect(page.locator("text=api.openai.com").first()).toBeVisible();
  await expect(page.locator("text=127.0.0.1:11434").first()).toBeVisible();
});

test("clicking the same card twice toggles the panel closed", async ({ page }) => {
  const card = page.locator("text=Anthropic").first();
  await card.click();
  await expect(page.locator("input[type='password']")).toBeVisible();

  await card.click();
  await expect(page.locator("input[type='password']")).not.toBeVisible();
});
