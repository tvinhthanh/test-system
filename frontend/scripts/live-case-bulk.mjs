import assert from "node:assert/strict";
import { mkdirSync, readFileSync } from "node:fs";
import { chromium } from "playwright-core";

const base = process.env.ATP_UI || "http://127.0.0.1:5173";
assert(
  process.env.ATP_EMAIL && process.env.ATP_PASSWORD,
  "Demo credentials required",
);
const channel = process.env.ATP_BROWSER_CHANNEL || "chrome";
const browser = await chromium.launch({
  headless: true,
  ...(channel === "bundled" ? {} : { channel }),
});
const page = await browser.newPage({
  viewport: { width: 1440, height: 960 },
  ignoreHTTPSErrors: new URL(base).hostname === "localhost",
});
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
mkdirSync("test-results", { recursive: true });
try {
  await page.goto(base);
  await page.locator("input[type=email]").fill(process.env.ATP_EMAIL);
  await page.locator("input[type=password]").fill(process.env.ATP_PASSWORD);
  await Promise.all([
    page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/v1/projects" && response.ok(),
    ),
    page.locator("button[type=submit]").click(),
  ]);
  const session = await (await page.request.get(`${base}/v1/session`)).json();
  const projects = await (await page.request.get(`${base}/v1/projects`)).json();
  const project = process.env.ATP_PROJECT || projects.items[0].id;
  const scope = `${base}/v1/projects/${project}`;
  async function mutate(path, data, method = "POST") {
    const response = await page.request.fetch(path, {
      method,
      data,
      headers: { "X-CSRF-Token": session.csrfToken, Origin: base },
    });
    assert(response.ok(), `HTTP ${response.status()} ${await response.text()}`);
    return response.json();
  }
  const prefix = `Bulk check ${Date.now()}`;
  const cases = [];
  for (let i = 0; i < 3; i++)
    cases.push(
      await mutate(`${scope}/test-cases`, {
        title: `${prefix} ${i}`,
        externalKey: `${prefix}-${i}`,
        expected: "=SUM(1,2)",
        steps: [{ action: "Pay | check", expected: "Receipt\nPaid" }],
      }),
    );
  await page.goto(`${base}/p/${project}/cases?q=${encodeURIComponent(prefix)}`);
  await page.getByLabel(`Select ${cases[0].title}`, { exact: true }).waitFor();
  await page.getByLabel(`Select ${cases[0].title}`, { exact: true }).check();
  await page.getByLabel(`Select ${cases[1].title}`, { exact: true }).check();
  await mutate(
    `${scope}/test-cases/${cases[0].id}`,
    { testData: "Concurrent edit", expectedUpdatedAt: cases[0].updatedAt },
    "PATCH",
  );
  await page.getByLabel("Bulk value", { exact: true }).selectOption("p0");
  await page.getByRole("button", { name: "Apply", exact: true }).click();
  await page.getByText("1 updated", { exact: false }).waitFor();
  await page
    .getByText(`${cases[0].title}: Changed; refresh before retry`, {
      exact: true,
    })
    .waitFor();
  await page.getByLabel(`Select ${cases[1].title}`, { exact: true }).waitFor();
  assert.equal(
    await page
      .getByLabel(`Select ${cases[0].title}`, { exact: true })
      .isChecked(),
    true,
  );
  assert.equal(
    await page
      .getByLabel(`Select ${cases[1].title}`, { exact: true })
      .isChecked(),
    false,
  );
  const edited = await (
    await page.request.get(`${scope}/test-cases/${cases[1].id}`)
  ).json();
  assert.equal(edited.priority, "p0");
  assert(
    edited.audit.some(
      (change) =>
        change.changedFields.includes("priority") &&
        change.actor === process.env.ATP_EMAIL,
    ),
  );
  for (const [label, count] of [
    ["Export selected", 1],
    ["Export filtered", 3],
  ]) {
    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("button", { name: label, exact: true }).click(),
    ]);
    const path = `test-results/bulk-${count}.csv`;
    await download.saveAs(path);
    const csv = readFileSync(path, "utf8");
    assert(csv.includes("steps_json"));
    assert(csv.includes("'=SUM(1,2)"));
    assert.equal(cases.filter((item) => csv.includes(item.id)).length, count);
    await page.getByText(`Exported ${count} cases`, { exact: true }).waitFor();
  }
  for (const [name, width] of [
    ["desktop", 1440],
    ["tablet", 768],
    ["mobile", 360],
  ]) {
    await page.setViewportSize({ width, height: 960 });
    await page.screenshot({
      path: `test-results/case-bulk-${name}.png`,
      fullPage: true,
    });
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= window.innerWidth,
      ),
      `${name}: horizontal overflow`,
    );
  }
  assert.deepEqual(errors, []);
  console.log(
    "PASS real API: bulk conflict/success, actor audit, selected/full-filter downloads, 1440/768/360",
  );
} finally {
  await browser.close();
}
