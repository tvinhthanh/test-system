import { createRequire } from "node:module";
import { mkdirSync } from "node:fs";
import assert from "node:assert/strict";

// Accept the host's installed Playwright package; do not couple the app to it.
const require = createRequire(import.meta.url);
const { chromium } = require(process.argv[2] || "playwright");
const browser = await chromium.launch({ channel: "chrome", headless: true });
const context = await browser.newContext({
  viewport: { width: 1440, height: 1000 },
});
const page = await context.newPage();
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
page.on("console", (message) => {
  if (message.type() === "error") errors.push(message.text());
});
mkdirSync("test-results", { recursive: true });
try {
  await page.goto("http://127.0.0.1:5173");
  await page.getByRole("link", { name: "Commerce Web" }).click();
  await page.getByRole("heading", { name: "Test runs", exact: true }).waitFor();
  await page.locator("tbody tr").first().waitFor();
  assert.equal(await page.locator("tbody tr").count(), 20);
  await page.screenshot({
    path: "test-results/runs-desktop.png",
    fullPage: true,
  });
  const next = page.getByRole("button", { name: "Next page", exact: true });
  await next.click();
  await page.waitForURL(/cursor=/);
  await page.locator("tbody tr").first().waitFor();
  await page.reload();
  await page.locator("tbody tr").first().waitFor();
  assert.match(page.url(), /cursor=/);
  await page.getByRole("button", { name: "First page", exact: true }).click();
  await page.waitForURL((url) => !url.searchParams.has("cursor"));
  await page
    .getByLabel("Search branch, commit or run")
    .fill("not-a-real-branch");
  await page.getByRole("heading", { name: "No matching runs" }).waitFor();
  await page
    .getByRole("button", { name: "Clear filters", exact: true })
    .click();
  await page.locator("tbody tr").first().waitFor();
  await page.locator(".run-link").nth(1).click();
  await page
    .getByRole("heading", { name: "Test results", exact: true })
    .waitFor();
  await page.locator(".test-row").first().waitFor();
  assert.equal(await page.locator(".test-row").count(), 12);
  await page.screenshot({
    path: "test-results/run-detail-desktop.png",
    fullPage: true,
  });
  await page.locator(".test-row").first().click();
  await page.getByText("Failure detail", { exact: true }).waitFor();
  await page.getByRole("heading", { name: "Attempts", exact: true }).waitFor();
  await page
    .getByRole("button", { name: "Copy Error", exact: true })
    .first()
    .click();
  await page.getByRole("button", { name: "Mark flaky", exact: true }).click();
  await page
    .getByRole("heading", { name: "Triage events", exact: true })
    .waitFor();
  await page
    .getByRole("link", { name: "Test intelligence", exact: true })
    .click();
  await page
    .getByRole("heading", { name: "Test history", exact: true })
    .waitFor();
  assert.equal(
    await page.locator('aside nav a[aria-current="page"]').count(),
    1,
  );
  assert.equal(
    await page
      .locator("aside")
      .getByRole("link", { name: "Test intelligence", exact: true })
      .getAttribute("aria-current"),
    "page",
  );
  await page.locator(".identity-row").first().waitFor();
  await page.getByRole("link", { name: "Flaky view", exact: true }).click();
  await page
    .getByRole("heading", { name: "Flaky tests", exact: true })
    .waitFor();
  await page.locator(".identity-row").first().click();
  await page.getByText("Identity history", { exact: true }).waitFor();
  await page.locator(".history-runs .event-row").first().waitFor();
  await page
    .getByRole("link", { name: "Test runs", exact: true })
    .last()
    .click();
  assert.equal(
    await page.locator('aside nav a[aria-current="page"]').count(),
    1,
  );
  await page.locator("tbody tr").first().waitFor();
  for (const width of [768, 360]) {
    await page.setViewportSize({ width, height: 900 });
    await page.screenshot({
      path: `test-results/runs-${width}.png`,
      fullPage: true,
    });
    const overflow = await page.evaluate(() => ({
      width: innerWidth,
      scroll: document.documentElement.scrollWidth,
      elements: [...document.querySelectorAll("body *")]
        .filter(
          (e) =>
            e.getBoundingClientRect().right > innerWidth &&
            getComputedStyle(e).position !== "absolute",
        )
        .slice(0, 12)
        .map(
          (e) =>
            `${e.tagName}.${e.className}: ${e.getBoundingClientRect().right}`,
        ),
    }));
    assert.ok(
      overflow.scroll <= overflow.width,
      `Overflow at ${width}: ${JSON.stringify(overflow)}`,
    );
  }
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole("link", { name: "ATP projects", exact: true }).click();
  await page.getByRole("link", { name: "Platform API" }).click();
  await page.getByRole("heading", { name: "No test runs yet" }).waitFor();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await page.getByRole("heading", { name: "Sign in", exact: true }).waitFor();
  await page.getByLabel("Email", { exact: true }).fill("review@example.test");
  await page.getByLabel("Password", { exact: true }).fill("preview-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByRole("heading", { name: "Projects", exact: true }).waitFor();
  assert.deepEqual(errors, []);
  console.log(
    "Browser PASS: project isolation UI, 20-row paging/reload, search/empty/reset, detail, sign-out/in, 1440/768/360 layouts; console clean. Fixture evidence only.",
  );
} catch (error) {
  await page.screenshot({
    path: "test-results/browser-failure.png",
    fullPage: true,
  });
  console.error(
    "Browser diagnostics:",
    page.url(),
    errors,
    await page.locator("body").innerText(),
  );
  throw error;
} finally {
  await browser.close();
}
