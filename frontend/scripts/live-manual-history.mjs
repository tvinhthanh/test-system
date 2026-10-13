// Run against the real app/API with ATP_UI, ATP_EMAIL and ATP_PASSWORD.
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { chromium } from "playwright-core";

const base = process.env.ATP_UI || "http://127.0.0.1:5173";
const email = process.env.ATP_EMAIL;
const password = process.env.ATP_PASSWORD;
assert(email && password, "ATP_EMAIL and ATP_PASSWORD are required");
const channel = process.env.ATP_BROWSER_CHANNEL || "chrome";
const browser = await chromium.launch({
  headless: true,
  ...(channel === "bundled" ? {} : { channel }),
});
const page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
mkdirSync("test-results", { recursive: true });
try {
  await page.goto(base);
  await page.locator("input[type=email]").fill(email);
  await page.locator("input[type=password]").fill(password);
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
  const mutate = async (path, data, method = "POST") => {
    const response = await page.request.fetch(path, {
      method,
      data,
      headers: { "X-CSRF-Token": session.csrfToken, Origin: base },
    });
    assert(response.ok(), `HTTP ${response.status()} ${await response.text()}`);
    return response.json();
  };
  const stamp = Date.now();
  const testCase = await mutate(`${scope}/test-cases`, {
    title: `History check ${stamp}`,
    steps: [{ action: "Original action", expected: "Original receipt" }],
    expected: "Paid",
    preconditions: "Original signed-in setup",
    testData: "Original account=42",
  });
  const plan = await mutate(`${scope}/test-plans`, {
    name: `History plan ${stamp}`,
    caseIds: [],
  });
  await mutate(`${scope}/test-cases`, { title: `Outside plan ${stamp}` });
  await page.goto(`${base}/p/${project}/plans/${plan.id}`);
  await page.getByText("No cases in this plan", { exact: true }).waitFor();
  assert.equal(await page.locator(".execution-list > li").count(), 0);
  await page.getByText("Plan scope (0)", { exact: true }).click();
  await page
    .getByLabel("Search cases to add", { exact: true })
    .fill(testCase.title);
  await Promise.all([
    page.waitForResponse(
      (response) => response.url().includes("/test-cases?") && response.ok(),
    ),
    page.getByRole("button", { name: "Search cases", exact: true }).click(),
  ]);
  await page.locator(`#scope-${testCase.id}`).check();
  await Promise.all([
    page.waitForResponse(
      (response) =>
        response.url().endsWith(`/test-plans/${plan.id}`) &&
        response.request().method() === "PATCH" &&
        response.ok(),
    ),
    page.getByRole("button", { name: "Save scope", exact: true }).click(),
  ]);
  const row = page
    .locator(".execution-list > li")
    .filter({ hasText: testCase.title });
  await row
    .getByLabel(`Assignee email for ${testCase.title}`, { exact: true })
    .fill(email);
  await Promise.all([
    page.waitForResponse(
      (response) =>
        response.url().endsWith(`/test-plans/${plan.id}`) &&
        response.request().method() === "PATCH" &&
        response.ok(),
    ),
    row.getByRole("button", { name: "Assign", exact: true }).click(),
  ]);
  const [startedResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.url().endsWith(`/test-plans/${plan.id}`) &&
        response.request().method() === "PATCH" &&
        response.ok(),
    ),
    row.getByRole("button", { name: "Start", exact: true }).click(),
  ]);
  const started = await startedResponse.json();
  assert.equal(started.cases.length, 1);
  assert.equal(started.cases[0].assigneeEmail, email);
  assert.equal(started.cases[0].workStatus, "in_progress");
  assert.equal((started.executions ?? []).length, 0);
  await row.getByText("Record execution", { exact: true }).click();
  await row.locator("select").selectOption("failed");
  await row.getByPlaceholder("Actual result").fill("Receipt missing");
  await row
    .getByLabel("Bug URL", { exact: true })
    .fill("https://bugs.example/123");
  const [firstResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        /\/executions$/.test(response.url()) && response.status() === 201,
    ),
    row.getByRole("button", { name: "failed", exact: true }).click(),
  ]);
  const first = await firstResponse.json();
  assert.equal(first.caseSnapshot.title, testCase.title);
  assert.equal(first.stepResults[0].actual, "Receipt missing");
  assert.equal(first.caseSnapshot.preconditions, "Original signed-in setup");
  assert.equal(first.caseSnapshot.testData, "Original account=42");
  await row.locator(".evidence-attach:not([disabled])").waitFor();
  await Promise.all([
    page.waitForResponse(
      (response) =>
        /\/evidence$/.test(response.url()) && response.status() === 201,
    ),
    row.locator("input[type=file]").setInputFiles({
      name: "original-evidence.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("Original failure proof"),
    }),
  ]);
  await page.goto(`${base}/p/${project}/cases/${testCase.id}`);
  await page
    .getByLabel("Preconditions", { exact: true })
    .fill("New signed-in setup");
  await page.getByLabel("Test data", { exact: true }).fill("New account=99");
  await page.locator("#case-title").fill(`Renamed history ${stamp}`);
  await page.getByPlaceholder("Action", { exact: true }).fill("Changed action");
  await page
    .getByPlaceholder("Expected", { exact: true })
    .fill("Changed receipt");
  const [changedResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        response.url().endsWith(`/test-cases/${testCase.id}`) &&
        response.request().method() === "PATCH" &&
        response.ok(),
    ),
    page.getByRole("button", { name: "Save changes", exact: true }).click(),
  ]);
  const changed = await changedResponse.json();
  assert.equal(changed.preconditions, "New signed-in setup");
  assert.equal(changed.audit[0].actor, email);
  assert(changed.audit[0].changedFields.includes("preconditions"));
  await page
    .getByRole("region", { name: "Case change history" })
    .getByText("Updated", { exact: true })
    .waitFor();
  for (const [name, width, height] of [
    ["desktop", 1440, 960],
    ["tablet", 768, 960],
    ["mobile", 360, 844],
  ]) {
    await page.setViewportSize({ width, height });
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      `${name}: case editor horizontal overflow`,
    );
    await page.screenshot({
      path: `test-results/case-context-${name}.png`,
      fullPage: true,
    });
  }
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${base}/p/${project}/plans/${plan.id}`);
  const renamed = page
    .locator(".execution-list > li")
    .filter({ hasText: `Renamed history ${stamp}` });
  await renamed.getByText("Record execution", { exact: true }).click();
  await renamed.getByLabel("Retest latest execution", { exact: true }).check();
  await renamed.locator("select").selectOption("passed");
  await renamed.getByPlaceholder("Actual result").fill("Receipt shown");
  const [secondResponse] = await Promise.all([
    page.waitForResponse(
      (response) =>
        /\/executions$/.test(response.url()) && response.status() === 201,
    ),
    renamed.getByRole("button", { name: "passed", exact: true }).click(),
  ]);
  const second = await secondResponse.json();
  assert.equal(second.retestOf, first.id);
  assert.match(second.caseSnapshot.title, /^Renamed history/);
  await renamed.getByText("Execution history (2)", { exact: true }).click();
  await renamed.getByText("Case at execution", { exact: true }).last().click();
  await renamed.getByText("1. Original action", { exact: true }).waitFor();
  const history = await (
    await page.request.get(`${scope}/test-plans/${plan.id}`)
  ).json();
  assert.equal(history.executionHistory.length, 2);
  assert.equal(
    history.executionHistory[1].caseSnapshot.steps[0].action,
    "Original action",
  );
  assert.equal(history.executionHistory[1].evidence.length, 1);
  assert.equal(
    history.executionHistory[1].caseSnapshot.preconditions,
    "Original signed-in setup",
  );
  assert.equal(
    history.executionHistory[0].caseSnapshot.preconditions,
    "New signed-in setup",
  );
  assert.equal(
    history.executionHistory[1].caseSnapshot.testData,
    "Original account=42",
  );
  const proof = await page.request.get(
    `${scope}/artifacts/${history.executionHistory[1].evidence[0].id}/content`,
  );
  assert.equal(await proof.text(), "Original failure proof");
  assert.equal(history.executions[0].id, second.id);
  assert.equal(history.cases.length, 1);
  assert.equal(history.cases[0].workStatus, "completed");
  for (const [name, width, height] of [
    ["desktop", 1440, 960],
    ["mobile", 390, 844],
  ]) {
    await page.setViewportSize({ width, height });
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      `${name}: horizontal overflow`,
    );
    await page.screenshot({
      path: `test-results/manual-history-${name}.png`,
      fullPage: true,
    });
  }
  assert.deepEqual(errors, []);
  console.log(
    "PASS: real login, fixed scope, assignment, workflow, case context editor/audit, immutable context snapshot, retest, old evidence, desktop/mobile",
  );
} finally {
  await browser.close();
}
