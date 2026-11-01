import assert from "node:assert/strict";
import { createRequire } from "node:module";

// Run against `npm run dev:fixture`. This catches missing fixture contracts
// and layout regressions on the main routes without claiming live API coverage.
const require = createRequire(import.meta.url);
const { chromium } = require(process.argv[2] || "playwright-core");
const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const routes = [
  "/",
  "/p/commerce/overview",
  "/p/commerce/roi",
  "/p/commerce/runs",
  "/p/commerce/runs/7e14a1047-1d5c-48a6-8ae7-101001001001",
  "/p/commerce/runs/7e14a1047-1d5c-48a6-8ae7-101001001001/results/result-1",
  "/p/commerce/tests",
  "/p/commerce/tests/identity-0/history",
  "/p/commerce/cases",
  "/p/commerce/cases/case-0",
  "/p/commerce/requirements",
  "/p/commerce/plans",
  "/p/commerce/plans/plan-1",
  "/p/commerce/plans/plan-1/run",
  "/p/commerce/releases",
  "/p/commerce/releases/rel-2",
  "/p/commerce/start",
  "/p/commerce/auto",
  "/p/commerce/auto/runs/auto-run-preview",
  "/p/commerce/auto/scans/scan-preview",
  "/p/commerce/specialized-results",
  "/p/commerce/url-checks",
  "/p/commerce/url-checks/url-check-preview",
  "/p/commerce/tokens",
  "/p/commerce/members",
  "/p/commerce/guide",
  "/team",
  "/account/security",
];
const failures = [];
let current = "";
page.on("pageerror", (error) => failures.push(`${current}: ${error.message}`));
try {
  for (const width of [1440, 360]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of routes) {
      current = `${width}px ${route}`;
      await page.goto(`http://127.0.0.1:5173${route}`);
      await page.waitForTimeout(1000);
      const state = await page.evaluate(() => ({
        overflow: document.documentElement.scrollWidth > innerWidth + 1,
        text: document.body.innerText,
      }));
      if (state.overflow) failures.push(`${current}: page overflow`);
      if (
        state.text.includes(
          "This resource is unavailable or no longer exists.",
        ) ||
        state.text.includes(
          "The request could not be completed. Please try again.",
        )
      )
        failures.push(`${current}: resource error`);
    }
  }
  assert.deepEqual(failures, []);
  console.log(
    `Fixture routes PASS: ${routes.length} routes at 1440px and 360px, no page errors or overflow. Fixture evidence only.`,
  );
} finally {
  await browser.close();
}
