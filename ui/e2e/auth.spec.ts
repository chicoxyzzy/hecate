import { expect, test } from "./fixtures";

// Auth-gate scenarios. The default `mockGatewayAPIs` fixture stubs every
// endpoint and returns an anonymous /v1/whoami, which is exactly what the
// empty-token flow expects. The rejected-token flow overrides /v1/whoami
// to report an invalid token so ConsoleShell routes to TokenGate(rejected).

test.describe("TokenGate — empty token flow", () => {
  test("operator with no saved token sees the gate, pastes one, and lands on the workspace", async ({ page }) => {
    // Fresh browser context already has empty localStorage; we belt-and-brace
    // it inside an init script so this stays robust if the runner is ever
    // configured to reuse storage state across tests.
    await page.addInitScript(() => window.localStorage.clear());

    await page.goto("/");

    // The gate is visible; the workspace shell is not.
    await expect(page.getByRole("heading", { name: /admin token required/i })).toBeVisible();
    await expect(page.locator(".hecate-activitybar")).toHaveCount(0);
    // Empty-token branch must not show the rejected-token banner.
    await expect(page.getByText(/saved token was rejected/i)).toHaveCount(0);

    await page.getByLabel(/admin bearer token/i).fill("fresh-token");
    await page.getByRole("button", { name: /^connect$/i }).click();

    // Once the token is set, the dashboard reloads and the workspace activity
    // bar renders. The fixture's anonymous /v1/whoami is fine here — we're
    // verifying the gate transitions away, not the admin/tenant distinction.
    await expect(page.locator(".hecate-activitybar")).toBeVisible();
    await expect(page.getByRole("heading", { name: /admin token required/i })).toHaveCount(0);

    // The submitted token is persisted to localStorage so a refresh skips
    // the gate next time.
    const stored = await page.evaluate(() => window.localStorage.getItem("hecate.authToken"));
    expect(stored).toBe("fresh-token");
  });

  test("submitting an empty token shows an inline error and does not advance", async ({ page }) => {
    await page.addInitScript(() => window.localStorage.clear());
    await page.goto("/");

    await expect(page.getByRole("heading", { name: /admin token required/i })).toBeVisible();
    await page.getByRole("button", { name: /^connect$/i }).click();

    await expect(page.getByText(/paste the token from your gateway logs/i)).toBeVisible();
    await expect(page.locator(".hecate-activitybar")).toHaveCount(0);
  });
});

test.describe("TokenGate — rejected token flow", () => {
  test("operator with a stale saved token sees the rejected banner", async ({ page }) => {
    // The gateway has rotated keys (or the operator pasted a bad token);
    // /v1/whoami flags invalid_token. ConsoleShell must catch this and
    // route to TokenGate with the inline rejection banner instead of
    // dumping the operator into a 401-spinning workspace.
    await page.route("/v1/whoami*", r =>
      r.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          object: "session",
          data: {
            authenticated: false,
            invalid_token: true,
            role: "anonymous",
            tenant: "",
            source: "auth_token",
            key_id: "",
          },
        }),
      }),
    );

    // Seed localStorage *before* page scripts run so the synchronous
    // useState hydration in useRuntimeConsole picks the bad token up on
    // the very first render.
    await page.addInitScript(() => {
      window.localStorage.clear();
      window.localStorage.setItem("hecate.authToken", "stale-bearer");
    });

    await page.goto("/");

    await expect(page.getByRole("heading", { name: /admin token required/i })).toBeVisible();
    await expect(page.getByText(/saved token was rejected by the gateway/i)).toBeVisible();
    // The workspace must not flash through during the loading-to-rejected transition.
    await expect(page.locator(".hecate-activitybar")).toHaveCount(0);
  });
});
