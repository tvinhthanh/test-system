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
const page = await browser.newPage({
  viewport: { width: 1440, height: 960 },
  ignoreHTTPSErrors: new URL(base).hostname === "localhost",
});
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
  const createViaUi = async (suffix) => {
    const [response] = await Promise.all([
      page.waitForResponse(
        (r) =>
          r.url().endsWith(suffix) &&
          r.request().method() === "POST" &&
          r.status() === 201,
      ),
      page.getByRole("button", { name: "Create", exact: true }).click(),
    ]);
    return response.json();
  };
  await page.goto(`${base}/p/${project}/requirements`);
  await page
    .getByRole("button", { name: "New requirement", exact: true })
    .first()
    .click();
  await page.locator("#req-key").fill(`REQ-${stamp}`);
  await page.locator("#req-title").fill(`Payment receipt ${stamp}`);
  await page.locator("#req-priority").selectOption("p0");
  const requirement = await createViaUi("/requirements");
  await page
    .getByLabel(`Status of ${requirement.title}`, { exact: true })
    .selectOption("active");
  await page.goto(`${base}/p/${project}/cases`);
  await page
    .getByRole("button", { name: "New case", exact: true })
    .first()
    .click();
  await page.locator("#new-title").fill(`History check ${stamp}`);
  await page.getByLabel("Level", { exact: true }).selectOption("system");
  for (const name of [
    "Purposes: Functional",
    "Purposes: Performance",
    "Usage tags: E2E",
    "Usage tags: Regression",
    "Execution modes: Manual",
    "Execution modes: Automated",
    "Performance types: Stress",
  ]) {
    await page.getByLabel(name, { exact: true }).check();
  }
  await page
    .getByLabel("Preconditions", { exact: true })
    .fill("Original signed-in setup");
  await page
    .getByLabel("Test data", { exact: true })
    .fill("Original account=42");
  let testCase = await createViaUi("/test-cases");
  assert.equal(testCase.taxonomy.level, "system");
  assert.deepEqual(testCase.taxonomy.performanceTypes, ["stress"]);
  await page.getByRole("button", { name: "Add step", exact: true }).click();
  await page
    .getByPlaceholder("Action", { exact: true })
    .fill("Original action");
  await page
    .getByPlaceholder("Expected", { exact: true })
    .fill("Original receipt");
  await page.locator("#case-expected").fill("Paid");
  const [savedCase] = await Promise.all([
    page.waitForResponse(
      (r) =>
        r.url().endsWith(`/test-cases/${testCase.id}`) &&
        r.request().method() === "PATCH" &&
        r.ok(),
    ),
    page.getByRole("button", { name: "Save changes", exact: true }).click(),
  ]);
  testCase = await savedCase.json();
  const coverage = page.locator('[aria-labelledby="req-title"]');
  await coverage.locator("select").selectOption(requirement.id);
  await Promise.all([
    page.waitForResponse(
      (r) =>
        r.url().includes(`/test-cases/${testCase.id}/requirements`) &&
        r.request().method() === "POST" &&
        r.ok(),
    ),
    coverage.getByRole("button", { name: "Add", exact: true }).click(),
  ]);
  await coverage.getByText(requirement.title, { exact: false }).waitFor();
  const coveredCase = await (
    await page.request.get(`${scope}/test-cases/${testCase.id}`)
  ).json();
  assert(coveredCase.requirements.includes(requirement.id));
  const requirementList = await (
    await page.request.get(`${scope}/requirements`)
  ).json();
  assert.equal(
    requirementList.items.find((item) => item.id === requirement.id).coveredBy,
    1,
  );
  await page.goto(`${base}/p/${project}/plans`);
  await page
    .getByRole("button", { name: "New plan", exact: true })
    .first()
    .click();
  await page.locator("#plan-name").fill(`History plan ${stamp}`);
  await page
    .getByLabel("Include current active cases", { exact: true })
    .uncheck();
  const plan = await createViaUi("/test-plans");
  await mutate(`${scope}/test-cases`, { title: `Outside plan ${stamp}` });
  await page.goto(`${base}/p/${project}/plans/${plan.id}`);
  await page.getByText("No cases in this plan", { exact: true }).waitFor();
  assert.equal(await page.locator(".execution-list > li").count(), 0);
  await page.getByText("Plan scope (0)", { exact: true }).click();
  await page
    .getByLabel("Plan filter Level", { exact: true })
    .selectOption("system");
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
  assert.equal(first.caseSnapshot.taxonomy.level, "system");
  assert.deepEqual(first.caseSnapshot.taxonomy.performanceTypes, ["stress"]);
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
  await page.getByLabel("Level", { exact: true }).selectOption("acceptance");
  await page.getByLabel("Purposes: Performance", { exact: true }).uncheck();
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
    .first()
    .waitFor();
  for (const [name, width, height] of [
    ["desktop", 1440, 960],
    ["tablet", 768, 960],
    ["mobile", 360, 844],
  ]) {
    await page.setViewportSize({ width, height });
    await page.screenshot({
      path: `test-results/case-context-${name}.png`,
      fullPage: true,
    });
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      `${name}: case editor horizontal overflow`,
    );
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
  assert.equal(second.caseSnapshot.taxonomy.level, "acceptance");
  assert.deepEqual(second.caseSnapshot.taxonomy.performanceTypes, []);
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
    history.executionHistory[1].caseSnapshot.taxonomy.level,
    "system",
  );
  assert.deepEqual(
    history.executionHistory[1].caseSnapshot.taxonomy.performanceTypes,
    ["stress"],
  );
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
  await page.setViewportSize({ width: 1440, height: 960 });
  await page.goto(`${base}/p/${project}/releases`);
  await page
    .getByRole("button", { name: "New release", exact: true })
    .first()
    .click();
  await page.locator("#rel-version").fill(`manual-acceptance-${stamp}`);
  await page.locator("#rel-branch").fill(`no-automation-${stamp}`);
  const release = await createViaUi("/releases");
  const [evaluation] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().endsWith(`/releases/${release.id}/evaluate`) && r.ok(),
    ),
    // The header and an un-evaluated gate section both offer the button.
    page
      .getByRole("region", { name: "Quality gate" })
      .getByRole("button", { name: "Evaluate gate", exact: true })
      .click(),
  ]);
  const evaluated = await evaluation.json();
  assert.equal(evaluated.gate.verdict, "no_evidence");
  assert.equal(evaluated.gate.runId ?? "", "");
  assert.equal(evaluated.gate.rules.length, 5);
  const decisionNote = `Manual retest passed in plan ${plan.id}; no automation run on release branch, hold release.`;
  await page.locator("#decision-note").fill(decisionNote);
  const [decision] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().endsWith(`/releases/${release.id}/decision`) && r.ok(),
    ),
    page.getByRole("button", { name: "Block", exact: true }).click(),
  ]);
  const blocked = await decision.json();
  assert.equal(blocked.status, "blocked");
  assert.equal(blocked.decidedBy, email);
  assert.equal(blocked.decisionNote, decisionNote);
  await page.reload();
  await page.getByText("Decided: blocked", { exact: true }).waitFor();
  await page.getByText(decisionNote, { exact: false }).waitFor();
  for (const [name, width] of [
    ["desktop", 1440],
    ["tablet", 768],
    ["mobile", 360],
  ]) {
    await page.setViewportSize({ width, height: 960 });
    assert(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      `${name}: release horizontal overflow`,
    );
    await page.screenshot({
      path: `test-results/manual-release-${name}.png`,
      fullPage: true,
    });
  }
  assert.deepEqual(errors, []);
  console.log(
    "PASS: real UI requirement/case/coverage/plan, assignment, workflow, context/audit, immutable snapshot, bug/retest/evidence, no-evidence release and persisted named decision, desktop/tablet/mobile",
  );
} finally {
  await browser.close();
}
