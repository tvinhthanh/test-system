import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { mkdirSync } from "node:fs";

// Run against `npm run dev:fixture`; this exercises client interactions only.
const require = createRequire(import.meta.url);
const { chromium } = require(process.argv[2] || "playwright-core");
const baseUrl = process.env.FIXTURE_URL || "http://127.0.0.1:5173";
const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
try {
  await page.goto(`${baseUrl}/p/commerce/auto`);
  await page.getByRole("heading", { name: "Auto test", exact: true }).waitFor();
  await page.waitForFunction(
    () => document.querySelector("#auto-environment")?.value === "env-preview",
  );
  await page.getByText("1 runnable case").waitFor();
  mkdirSync("test-results", { recursive: true });
  await page.screenshot({
    path: "test-results/auto-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 360, height: 800 });
  await page.screenshot({
    path: "test-results/auto-mobile.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.locator("#auto-environment").selectOption("__new");
  await page.locator("#env-key").fill("preview-new");
  await page.locator("#env-url").fill("https://new.example.test");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await page.waitForFunction(
    () => document.querySelector("#auto-environment")?.value !== "__new",
  );
  await page.getByRole("tab", { name: "Site scan" }).click();
  await page.waitForURL(/tab=scan/);
  await page.waitForFunction(
    () =>
      document.querySelector("#scan-url")?.value === "https://new.example.test",
  );
  assert.equal(
    await page.locator("#scan-url").inputValue(),
    "https://new.example.test",
  );
  await page.getByRole("tab", { name: "Runs", exact: true }).click();
  await page.waitForURL(/tab=runs/);
  await page.goBack();
  assert.equal(
    await page
      .getByRole("tab", { name: "Site scan" })
      .getAttribute("aria-selected"),
    "true",
  );
  await page.getByRole("tab", { name: "Cases", exact: true }).click();
  await page.getByRole("button", { name: /Browser × device/ }).click();
  await page.getByLabel("Firefox · phone").check();
  await page.getByText("1 case × 1 variant = 1 case runs").waitFor();
  await page.getByRole("button", { name: "Run all", exact: true }).click();
  await page.waitForURL(/\/auto\/runs\//);
  await page.getByRole("heading", { name: "1 case on the site" }).waitFor();
  await page.getByText("Firefox · Phone").waitFor();

  await page.goto(`${baseUrl}/p/commerce/start`);
  await page.getByRole("button", { name: /^Smoke/ }).click();
  await page.locator("#start-environment").selectOption("env-preview");
  await page.getByRole("button", { name: /Create plan and start/ }).click();
  await page.waitForURL(/\/plans\/[^/]+\/run$/);

  await page.evaluate(() =>
    sessionStorage.setItem("atp-fixture-session", "out"),
  );
  await page.goto(`${baseUrl}/signup?project=commerce`);
  await page
    .getByRole("heading", { name: "Ask to join", exact: true })
    .waitFor();
  await page.locator("#signup-email").fill("new@example.test");
  await page
    .getByRole("button", { name: "Send me the link", exact: true })
    .click();
  await page.getByRole("heading", { name: "Check your email" }).waitFor();
  await page.goto(`${baseUrl}/signup#atp_sgn_preview`);
  await page.getByRole("heading", { name: "Choose your password" }).waitFor();
  assert.equal(
    new URL(page.url()).hash,
    "",
    "sign-up token leaves the address bar",
  );
  await page.locator("#signup-password").fill("preview-password-123");
  await page.locator("#signup-confirm").fill("preview-password-123");
  await page.getByRole("button", { name: "Create account" }).click();
  await page.getByRole("heading", { name: "Your account is ready" }).waitFor();
  assert.deepEqual(errors, []);
  console.log(
    "Fixture workflows PASS: Auto matrix run, Start testing plan, and sign-up request/completion; no page errors. Fixture evidence only.",
  );
} finally {
  await browser.close();
}
