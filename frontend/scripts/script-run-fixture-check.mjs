import assert from "node:assert/strict";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require(process.argv[2] || "playwright-core");
const baseUrl = process.env.FIXTURE_URL || "http://127.0.0.1:5173";
const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const failures = [];
page.on("pageerror", (error) => failures.push(error.message));
try {
  await page.goto(`${baseUrl}/p/commerce/script-runs`);
  await page.setInputFiles("#script-archive", {
    name: "tests.tar.gz",
    mimeType: "application/gzip",
    buffer: Buffer.from("fixture archive"),
  });
  await page.selectOption("#script-environment", "env-preview");
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/script-runs\/[^/]+$/);
  await page.waitForTimeout(7000);
  assert.match(await page.locator("body").innerText(), /3 passed/);
  assert.deepEqual(failures, []);
  console.log("Script run fixture workflow PASS: multipart upload, detail navigation and polling.");
} finally {
  await browser.close();
}
