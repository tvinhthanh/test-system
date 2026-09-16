import { createRequire } from "node:module";
import assert from "node:assert/strict";
const require = createRequire(import.meta.url);
const { chromium } = require(process.argv[2] || "playwright");
const browser = await chromium.launch({ channel: "chrome", headless: true });
const page = await browser.newPage({ viewport: { width: 1440, height: 960 } });
const requests = [],
  crashes = [];
let denial = 0,
  authenticated = true;
const run = {
  id: "run-1",
  branch: "main",
  commitSha: "abc123",
  status: "passed",
  environmentName: "Staging",
  startedAt: "2026-09-14T09:00:00Z",
  durationMs: 1000,
  totalCount: 1,
  passedCount: 0,
  failedCount: 0,
  flakyCount: 1,
};
page.on("pageerror", (error) => crashes.push(error.message));
await page.route("**/v1/**", async (route) => {
  const request = route.request(),
    url = new URL(request.url());
  requests.push({
    path: url.pathname,
    query: url.search,
    method: request.method(),
  });
  let data,
    status = 200;
  if (url.pathname === "/v1/session") {
    if (request.method() === "POST") {
      assert.equal(request.headers()["x-csrf-token"], "test-csrf");
      if (request.postDataJSON().password === "wrong") status = 401;
      else authenticated = true;
    }
    data = {
      user: authenticated
        ? { id: "a", email: "a@example.test", name: "A" }
        : null,
      csrfToken: "test-csrf",
    };
  } else if (url.pathname === "/v1/projects")
    data = {
      items: [{ id: "one", name: "Project One", workspaceName: "Workspace" }],
      nextCursor: null,
    };
  else if (denial) {
    status = denial;
    if (status === 401) authenticated = false;
    data = {
      error: {
        code: status === 401 ? "UNAUTHENTICATED" : "FORBIDDEN",
        details: {},
      },
    };
  } else if (url.pathname.endsWith("/results"))
    data = { items: [], nextCursor: null };
  else if (url.pathname.endsWith("/run-1")) data = run;
  else data = { items: [run], nextCursor: null };
  await route.fulfill({
    status,
    contentType: "application/json",
    headers: { "X-Request-Id": "browser-contract-test" },
    body: JSON.stringify(data),
  });
});
try {
  await page.goto("http://127.0.0.1:5174");
  await page.getByRole("link", { name: "Project One" }).waitFor();
  assert.equal(requests.length, 2, "one session plus one projects request");
  await page.getByRole("link", { name: "Project One" }).click();
  await page.locator("tbody tr").waitFor();
  assert.equal(requests.length, 3, "one runs request, no N+1");
  await page.getByLabel("Search branch, commit or run").fill("c");
  await page.getByLabel("Search branch, commit or run").fill("ch");
  await page.getByLabel("Search branch, commit or run").fill("checkout");
  await page.waitForURL(/q=checkout/);
  await page.locator("tbody tr").waitFor();
  assert.equal(requests.length, 4, "one debounced filter request");
  await page.waitForTimeout(500);
  assert.equal(requests.length, 4, "no idle request loop");
  await page.locator(".run-link").click();
  await page
    .getByRole("heading", { name: "No results received yet" })
    .waitFor();
  assert.equal(requests.length, 6, "one summary and one results request");
  await page.goBack();
  await page.locator("tbody tr").waitFor();
  assert.match(page.url(), /q=checkout/);
  denial = 403;
  await page.getByRole("button", { name: "Refresh runs", exact: true }).click();
  await page
    .getByText("You do not have permission to access this resource.")
    .waitFor();
  assert.equal(
    await page.locator("tbody tr").count(),
    0,
    "403 cannot display stale rows or fixtures",
  );
  denial = 401;
  await page.getByRole("button", { name: "Try again", exact: true }).click();
  await page.getByRole("heading", { name: "Sign in", exact: true }).waitFor();
  await page.getByLabel("Email", { exact: true }).fill("a@example.test");
  await page.getByLabel("Password", { exact: true }).fill("wrong");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByText("Email or password is incorrect.").waitFor();
  assert.equal(
    await page.getByLabel("Email", { exact: true }).inputValue(),
    "a@example.test",
  );
  assert.equal(
    await page.getByLabel("Password", { exact: true }).inputValue(),
    "wrong",
  );
  assert.deepEqual(crashes, []);
  console.log(
    "API browser PASS: 1 bootstrap, 1 projects, 1 runs; 1 debounced filter; 2 detail requests; no idle loop; URL back restoration; 403 clears rows; 401 redirects; CSRF and generic login error verified. Mocked HTTP contracts, not Go integration.",
  );
} catch (e) {
  console.error(requests, await page.locator("body").innerText());
  throw e;
} finally {
  await browser.close();
}
