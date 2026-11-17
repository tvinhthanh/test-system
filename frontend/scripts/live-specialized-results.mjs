// Synthetic contract fixtures, not measurements of the platform's capacity.
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { chromium } from "playwright-core";

const base = process.env.ATP_UI || "http://127.0.0.1:5173";
assert(
  process.env.ATP_EMAIL && process.env.ATP_PASSWORD,
  "ATP_EMAIL / ATP_PASSWORD required",
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
const errors = [],
  requests = [];
page.on("pageerror", (e) => errors.push(e.message));
page.on("request", (r) => {
  if (new URL(r.url()).pathname.includes("/specialized-results"))
    requests.push(r.url());
});
mkdirSync("test-results", { recursive: true });
try {
  await page.goto(base);
  await page.locator("input[type=email]").fill(process.env.ATP_EMAIL);
  await page.locator("input[type=password]").fill(process.env.ATP_PASSWORD);
  await Promise.all([
    page.waitForResponse(
      (r) => new URL(r.url()).pathname === "/v1/projects" && r.ok(),
    ),
    page.locator("button[type=submit]").click(),
  ]);
  const session = await (await page.request.get(`${base}/v1/session`)).json();
  const projects = await (await page.request.get(`${base}/v1/projects`)).json();
  const project = process.env.ATP_PROJECT || projects.items[0].id;
  const path = `/p/${project}/specialized-results`,
    api = `${base}/v1/projects/${project}/specialized-results`;
  const headers = { "X-CSRF-Token": session.csrfToken, Origin: base };
  const stamp = Date.now();
  const start = new Date(Date.now() - 120000).toISOString(),
    finish = new Date(Date.now() - 60000).toISOString();
  const kinds = [
    "performance",
    "security",
    "accessibility",
    "compatibility",
    "recovery",
    "usability",
  ];
  const metadata = {
    Environment: "synthetic-fixture",
    "Application version": "fixture-1",
    "Source / tool": "synthetic-fixture",
    "Tool version": "1",
    "Adapter version": "atp-report-v1",
  };
  const completeForm = async (title) => {
    await page.getByLabel("Report title", { exact: true }).fill(title);
    for (const [label, value] of Object.entries(metadata))
      await page.getByLabel(label, { exact: true }).fill(value);
    await page
      .getByLabel("Started at", { exact: true })
      .fill(start.slice(0, 16));
    await page
      .getByLabel("Finished at", { exact: true })
      .fill(finish.slice(0, 16));
    await page
      .getByLabel("Original report", { exact: true })
      .fill("<script>synthetic fixture only</script>");
  };
  const record = async () => {
    const [response] = await Promise.all([
      page.waitForResponse(
        (r) => r.url() === api && r.request().method() === "POST",
      ),
      page.getByRole("button", { name: "Record result", exact: true }).click(),
    ]);
    assert.equal(response.status(), 201, await response.text());
    const value = await response.json();
    await page.waitForURL((url) => url.searchParams.get("result") === value.id);
    await page.getByRole("article", { name: "Specialized result" }).waitFor();
    return value;
  };
  const originals = [];
  for (const kind of kinds) {
    await page.goto(base + path);
    await page.getByRole("button", { name: "New result", exact: true }).click();
    await page.getByLabel("Testing group", { exact: true }).selectOption(kind);
    await completeForm(`Synthetic ${kind} ${stamp}`);
    const value = await record();
    assert.equal(value.kind, kind);
    assert.equal(
      value.outcome,
      "inconclusive",
      "missing typed evidence must not pass",
    );
    originals.push(value);
    for (const width of [1440, 768, 360]) {
      await page.setViewportSize({ width, height: 960 });
      await page
        .getByRole("article", { name: "Specialized result" })
        .waitFor({ state: "visible" });
      await page.evaluate(async () => {
        await document.fonts.ready;
        await new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        );
      });
      assert(
        await page.evaluate(
          () => document.documentElement.scrollWidth <= innerWidth,
        ),
        `overflow ${kind} ${width}`,
      );
      const screenshot = await page.screenshot({
        path: `test-results/specialized-${kind}-${width}.png`,
        fullPage: true,
        animations: "disabled",
      });
      assert(screenshot.length > 15000, `blank screenshot ${kind} ${width}`);
    }
    await page.setViewportSize({ width: 1440, height: 960 });
  }
  await page
    .getByRole("button", { name: "Record retest", exact: true })
    .click();
  await page.getByLabel("Report title", { exact: true }).waitFor();
  assert.equal(
    await page.getByLabel("Original report", { exact: true }).inputValue(),
    "",
  );
  await page
    .getByLabel("Execution state", { exact: true })
    .selectOption("not_run");
  await page
    .getByLabel("Notes", { exact: true })
    .fill("Synthetic retest not executed");
  const retest = await record();
  assert.equal(retest.input.retestOf, originals.at(-1).id);
  assert.equal(retest.outcome, "not_run");
  assert(
    await page.getByText("Not recorded", { exact: true }).first().isVisible(),
  );

  const findings = {
    standard: "Synthetic scope",
    standardVersion: "1",
    method: "combined",
    coverage: ["Synthetic login"],
    assessmentComplete: true,
    findings: [],
  };
  const fixtures = {
    performance: {
      subtype: "stress",
      scenario: "Synthetic checkout",
      loadConfiguration: "Synthetic 10 users",
      dataConfiguration: "Synthetic 100 records",
      durationSeconds: 60,
      windowSeconds: 30,
      p95Ms: 10,
      p99Ms: 20,
      throughputPerSecond: 10,
      errorRatePercent: 0,
      thresholdsRecordedAt: new Date(Date.parse(start) - 60000).toISOString(),
      thresholds: { p95MaxMs: 30 },
    },
    security: findings,
    accessibility: findings,
    compatibility: {
      matrix: [
        {
          browser: "Chrome",
          browserVersion: "fixture",
          os: "Windows",
          osVersion: "fixture",
          device: "Laptop",
          status: "passed",
          evidence: "Synthetic evidence",
        },
      ],
    },
    recovery: {
      fault: "Synthetic shutdown",
      initialConditions: "Synthetic DB",
      recoveryObjective: "Synthetic recovery",
      targetRecoverySeconds: 60,
      actualRecoverySeconds: 10,
      allowedDataLossBytes: 0,
      actualDataLossBytes: 0,
      evidence: "Synthetic log",
    },
    usability: {
      method: "Synthetic observation",
      participants: 2,
      conclusion: "Synthetic criteria met",
      tasks: [
        {
          task: "Pay",
          successCriterion: "Receipt",
          observation: "Synthetic receipt",
          conclusion: "Synthetic success",
          status: "passed",
        },
      ],
    },
  };
  for (const kind of kinds) {
    const input = {
      ...originals.find((r) => r.kind === kind).input,
      idempotencyKey: `valid-${kind}-${stamp}`,
      startedAt: start,
      finishedAt: finish,
      originalReport: "Synthetic fixture only, not platform benchmark",
      details: fixtures[kind],
    };
    const created = await page.request.post(api, { headers, data: input });
    assert.equal(created.status(), 201, await created.text());
    const value = await created.json();
    assert.equal(value.outcome, "passed", kind);
    await page.goto(`${base}${path}?result=${value.id}`);
    await page.getByRole("article", { name: "Specialized result" }).waitFor();
    assert.equal(
      (await (await page.request.get(`${api}/${value.id}`)).json()).outcome,
      "passed",
    );
    const replay = await page.request.post(api, { headers, data: input });
    assert.equal((await replay.json()).id, value.id);
    const conflict = await page.request.post(api, {
      headers,
      data: { ...input, title: "Changed payload" },
    });
    assert.equal(conflict.status(), 409);
  }
  const imported = {
    ...originals[0].input,
    originalReport: "<script>synthetic import</script>",
  };
  delete imported.idempotencyKey;
  await page.goto(base + path);
  const upload = async () => {
    const [response] = await Promise.all([
      page.waitForResponse(
        (r) => r.url() === api && r.request().method() === "POST",
      ),
      page.locator('input[type="file"]').setInputFiles({
        name: "synthetic.json",
        mimeType: "application/json",
        buffer: Buffer.from(JSON.stringify(imported)),
      }),
    ]);
    assert.equal(response.status(), 201, await response.text());
    const value = await response.json();
    await page.waitForURL((url) => url.searchParams.get("result") === value.id);
    return value;
  };
  const importedFirst = await upload(),
    importedAgain = await upload();
  assert.equal(importedFirst.id, importedAgain.id);
  const original = await page.request.get(
    `${api}/${importedFirst.id}/original`,
  );
  assert.equal(await original.text(), imported.originalReport);
  assert.match(original.headers()["content-type"], /^text\/plain/);
  assert.equal(original.headers()["x-content-type-options"], "nosniff");
  assert.match(original.headers()["content-disposition"], /^attachment;/);
  await page.goto(base + path);
  await page
    .getByLabel("Filter group", { exact: true })
    .selectOption("performance");
  await page
    .getByLabel("Filter outcome", { exact: true })
    .selectOption("inconclusive");
  await page
    .locator('a[data-kind="performance"][data-outcome="inconclusive"]')
    .first()
    .waitFor();
  assert.equal(
    await page
      .locator(
        'a[data-kind]:not([data-kind="performance"][data-outcome="inconclusive"])',
      )
      .count(),
    0,
  );
  await page.waitForLoadState("networkidle");
  const settled = requests.length;
  await page.waitForTimeout(300);
  assert.equal(requests.length, settled, "request loop");
  assert.deepEqual(errors, []);
  console.log(
    "Specialized results: six forms, six typed assessments, retest, import replay/conflict, original text, filters and 1440/768/360px passed (synthetic fixtures).",
  );
} finally {
  await browser.close();
}
