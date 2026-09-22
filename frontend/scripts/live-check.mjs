// Live integration: the real Vue app against the real Go backend, no route
// mocking. Every /v1 call the UI makes is recorded with its real status and
// printed at the end; any console error or page error fails the run. This
// is GATES.md T0 layer 3 ("dashboard thật, không mock") as a repeatable
// command instead of a one-off script.
//
// Prerequisites (see backend/GATES.md "Tái lập" or backend/deploy/README.md):
//   - api + worker running with a tenant, a member user and at least one
//     finished run with a failure (e.g. `npm run example` in reporter/playwright)
//   - the UI served in live mode with /v1 forwarded to the api:
//       `npm run dev` (Vite proxy → 127.0.0.1:8080) or the Caddy of deploy/compose.yml
//
//   ATP_UI=http://127.0.0.1:5173 ATP_EMAIL=alice@example.com ATP_PASSWORD='…' \
//     node scripts/live-check.mjs <installed-playwright-path>
//
// The playwright package path argument works like the other scripts here:
// Playwright is not a dependency of the app, the host's install is borrowed.
import { createRequire } from "node:module";
import { mkdirSync } from "node:fs";
import { resolve } from "node:path";
const require = createRequire(import.meta.url);
const pwPath = process.argv[2] ? resolve(process.argv[2]) : "playwright"; // relative to the cwd, like a CLI argument
const { chromium } = require(pwPath);

const base = (process.env.ATP_UI || "http://127.0.0.1:5173").replace(/\/$/, "");
const email = process.env.ATP_EMAIL;
const password = process.env.ATP_PASSWORD;
if (!email || !password) {
  console.error(
    "ATP_EMAIL and ATP_PASSWORD are required (a member of at least one project)",
  );
  process.exit(2);
}
mkdirSync("test-results", { recursive: true });

// ATP_BROWSER_CHANNEL: "chrome" (default, the host's Chrome), or "bundled" for
// Playwright's own Chromium (CI: `npx playwright install chromium`).
const channel = process.env.ATP_BROWSER_CHANNEL || "chrome";
const browser = await chromium.launch({
  ...(channel === "bundled" ? {} : { channel }),
  headless: true,
});
const page = await browser.newPage({
  viewport: { width: 1440, height: 960 },
  ignoreHTTPSErrors: true, // Caddy's internal CA when ATP_UI is https://localhost
});
const calls = [];
const errors = [];
page.on("response", (res) => {
  const u = new URL(res.url());
  if (u.pathname.startsWith("/v1/"))
    calls.push(
      `${res.request().method()} ${u.pathname}${u.search} → ${res.status()}`,
    );
});
page.on("pageerror", (e) => errors.push("pageerror: " + e.message));
page.on("console", (m) => {
  // Chrome logs its own line for every non-2xx resource; the deliberate
  // wrong-password 401 below is the one such line allowed, and the /v1
  // call list at the end asserts no other non-2xx happened.
  if (m.type() === "error" && !/Failed to load resource:.*401/.test(m.text()))
    errors.push("console: " + m.text());
});

const emailField =
  "input[type=email], input[name=email], input[autocomplete=username]";
// A valid 1×1 PNG (the server sniffs evidence bytes, so a bare header is
// refused; the browser must also decode it for the thumbnail check).
const tinyPng = () =>
  Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==",
    "base64",
  );
const v1 =
  (re, status = 200) =>
  (r) =>
    re.test(new URL(r.url()).pathname + new URL(r.url()).search) &&
    r.status() === status;

const timings = [];
const step = async (name, fn) => {
  const started = performance.now();
  try {
    await fn();
    const ms = Math.round(performance.now() - started);
    timings.push([name, ms]);
    console.log(`OK   ${name}  (${ms} ms)`);
  } catch (e) {
    console.log("FAIL " + name + ": " + String(e.message || e).split("\n")[0]);
    await page.screenshot({
      path: "test-results/live-fail.png",
      fullPage: true,
    });
    await browser.close();
    report();
    process.exit(1);
  }
};

const report = () => {
  console.log("\n/v1 calls made by the real UI:");
  for (const c of calls) console.log("  " + c);
  if (errors.length) {
    console.log("\nbrowser errors:");
    for (const e of errors) console.log("  " + e);
  }
};

let project = process.env.ATP_PROJECT || "";

await step(
  "open the app: anonymous bootstrap shows the sign-in form",
  async () => {
    await page.goto(base + "/", { waitUntil: "networkidle" });
    await page.waitForSelector(emailField, { timeout: 10000 });
  },
);

await step(
  "wrong password → 401 and a generic message (no account enumeration)",
  async () => {
    await page.fill(emailField, email);
    await page.fill("input[type=password]", "definitely not the password");
    const [res] = await Promise.all([
      page.waitForResponse(
        (r) =>
          r.url().includes("/v1/session") && r.request().method() === "POST",
      ),
      page.click("button[type=submit]"),
    ]);
    if (res.status() !== 401)
      throw new Error("expected 401, got " + res.status());
    await page.waitForTimeout(300);
    const text = await page.textContent("body");
    if (!/incorrect|invalid|wrong|sign in/i.test(text))
      throw new Error("no error shown: " + text.slice(0, 200));
    if (/not found|no such|unknown user/i.test(text))
      throw new Error("message enumerates accounts: " + text.slice(0, 200));
  },
);

await step("sign in with the real account → projects load", async () => {
  await page.fill("input[type=password]", password);
  await Promise.all([
    page.waitForResponse(v1(/^\/v1\/projects/), { timeout: 10000 }),
    page.click("button[type=submit]"),
  ]);
  await page.waitForTimeout(500);
});

await step("projects page renders at least one project link", async () => {
  const links = page.locator("a[href*='/p/']");
  await links.first().waitFor({ timeout: 10000 });
  const href = await links.first().getAttribute("href");
  const m = href.match(/\/p\/([^/]+)/);
  if (!m) throw new Error("no project id in " + href);
  if (!project) project = m[1];
});

await step("runs page renders real runs with a status", async () => {
  await page.goto(base + "/p/" + project + "/runs");
  await page.waitForResponse(v1(/^\/v1\/projects\/[^/]+\/runs\?/), {
    timeout: 10000,
  });
  await page
    .locator("a.run-link")
    .first()
    .waitFor({ state: "attached", timeout: 10000 });
  const text = await page.textContent("body");
  if (!/passed|failed|error|running|cancelled/i.test(text))
    throw new Error("no run status rendered: " + text.slice(0, 300));
  await page.screenshot({ path: "test-results/live-runs.png", fullPage: true });
});

await step(
  "run search that matches nothing shows the empty state, not an error",
  async () => {
    await page.goto(
      base + "/p/" + project + "/runs?q=not-a-real-branch-" + Date.now(),
    );
    await page.waitForResponse(v1(/^\/v1\/projects\/[^/]+\/runs\?.*q=/), {
      timeout: 10000,
    });
    await page.waitForTimeout(300);
    const text = await page.textContent("body");
    if (!/no matching runs|no runs/i.test(text))
      throw new Error("empty state not shown: " + text.slice(0, 300));
  },
);

await step("open a run → results page (failures first)", async () => {
  await page.goto(base + "/p/" + project + "/runs");
  await page
    .locator("a.run-link")
    .first()
    .waitFor({ state: "attached", timeout: 10000 });
  const href = await page.locator("a.run-link").first().getAttribute("href");
  await page.goto(base + href);
  await page.waitForResponse(v1(/\/results\?/), { timeout: 10000 });
  await page.locator(".test-row").first().waitFor({ timeout: 10000 });
});

await step(
  "open a result → detail with attempts; triage from the UI is accepted",
  async () => {
    await page.locator(".test-row.clickable, .test-row").first().click();
    await page.waitForResponse(v1(/^\/v1\/projects\/[^/]+\/results\/[^/?]+$/), {
      timeout: 10000,
    });
    await page.waitForTimeout(500);
    const text = await page.textContent("body");
    if (!/attempt/i.test(text))
      throw new Error("detail not rendered: " + text.slice(0, 300));
    // Artifacts of the attempt (§7.7 / §11.5): when the run carried any, the
    // link must serve bytes with the server's disposition and nosniff
    // headers, through the same session cookie.
    // Linking is eventual (the worker sweep, ≤ 1 min after upload), so a run
    // finished seconds ago may legitimately show none yet.
    const artifactLinks = page.locator(".artifact-list a");
    await artifactLinks
      .first()
      .waitFor({ state: "attached", timeout: 2000 })
      .catch(() => {});
    if (await artifactLinks.count()) {
      const href = await artifactLinks.first().getAttribute("href");
      const res = await page.request.get(base + href);
      if (res.status() !== 200)
        throw new Error("artifact content → " + res.status());
      const h = res.headers();
      if (
        !/^(inline|attachment); filename=/.test(
          h["content-disposition"] || "",
        ) ||
        h["x-content-type-options"] !== "nosniff"
      )
        throw new Error("artifact headers: " + JSON.stringify(h));
      console.log(
        `     artifact bytes served: ${h["content-type"]}; ${h["content-disposition"]}`,
      );
    } else {
      console.log("     (no artifacts on this attempt — link check skipped)");
    }
    const btn = page
      .locator("button:has-text('flaky'), button:has-text('Flaky')")
      .first();
    if (!(await btn.count())) return; // a passed result has no triage actions
    const [res] = await Promise.all([
      page.waitForResponse((r) => /triage-events$/.test(r.url()), {
        timeout: 10000,
      }),
      btn.click(),
    ]);
    if (res.status() !== 201 && res.status() !== 200)
      throw new Error("triage POST → " + res.status());
    await page.waitForTimeout(500);
  },
);

await step(
  "test intelligence (identities), flaky view and one identity's history",
  async () => {
    await page.goto(base + "/p/" + project + "/tests");
    await page.waitForResponse(v1(/\/identities\?/), { timeout: 10000 });
    await page
      .locator("a[href*='/tests/']")
      .first()
      .waitFor({ state: "attached", timeout: 10000 });
    await page.goto(base + "/p/" + project + "/flaky");
    await page.waitForResponse(v1(/\/identities\?.*view=flaky/), {
      timeout: 10000,
    });
    await page.waitForTimeout(300);
    await page.goto(base + "/p/" + project + "/tests");
    await page
      .locator("a[href*='/history']")
      .first()
      .waitFor({ state: "attached", timeout: 10000 });
    const href = await page
      .locator("a[href*='/history']")
      .first()
      .getAttribute("href");
    await page.goto(base + href);
    await page.waitForResponse(v1(/\/identities\/[^/]+\/runs\?/), {
      timeout: 10000,
    });
    await page.waitForTimeout(300);
  },
);

await step(
  "review queue and identity fingerprints (§9.4 tools over HTTP)",
  async () => {
    await page.goto(base + "/p/" + project + "/review");
    await Promise.race([
      page.locator(".review-item").first().waitFor({ timeout: 10000 }),
      page.locator(".empty-state h2").waitFor({ timeout: 10000 }),
    ]);
    await page.goto(base + "/p/" + project + "/tests");
    await page
      .locator("a[href*='/history']")
      .first()
      .waitFor({ state: "attached", timeout: 10000 });
    const href = await page
      .locator("a[href*='/history']")
      .first()
      .getAttribute("href");
    await page.goto(base + href);
    await page
      .locator(".alias-table tbody tr, .tools")
      .first()
      .waitFor({ timeout: 10000 });
  },
);

await step(
  "engineering overview: four tiles, the weekly category chart and its table",
  async () => {
    await page.goto(base + "/p/" + project + "/overview");
    await page.locator(".stat-tile").nth(3).waitFor({ timeout: 10000 });
    // Either a chart with a legend or the empty state — never an error.
    const chart = await page.locator(".trend-chart .column").count();
    const empty = await page.locator(".trend .empty-state").count();
    if (!chart && !empty)
      throw new Error("neither chart columns nor the empty state rendered");
    if (chart) {
      await page.locator(".trend-chart .column").first().hover();
      await page.locator(".chart-tooltip").waitFor({ timeout: 3000 });
      await page.getByRole("button", { name: "Show table" }).click();
      await page
        .locator(".trend-table tbody tr")
        .first()
        .waitFor({ timeout: 3000 });
    }
    await Promise.all([
      page.waitForResponse(v1(/\/metrics\/triage\?days=90/), {
        timeout: 10000,
      }),
      page.getByRole("button", { name: "Last 90 days" }).click(),
    ]);
    await page.screenshot({
      path: "test-results/live-overview.png",
      fullPage: true,
    });
  },
);

await step(
  "test management: create a case, link it to a real identity, open a release and evaluate the gate (§6.8)",
  async () => {
    const stamp = Date.now().toString(36);
    await page.goto(base + "/p/" + project + "/cases");
    await page.getByRole("button", { name: "New case" }).first().click();
    await page.fill("#new-title", "Live check case " + stamp);
    await page.locator("form:has(#new-title) input[type=checkbox]").check();
    await Promise.all([
      page.waitForResponse(v1(/\/test-cases$/, 201), { timeout: 10000 }),
      page.getByRole("button", { name: "Create", exact: true }).click(),
    ]);
    // The detail page opens; link the first active identity of the project.
    await page.locator("#case-title").waitFor({ timeout: 10000 });
    await page.getByRole("button", { name: "Link a test" }).click();
    const candidate = page.locator(".bg-card button:has(.status)").first();
    await candidate.waitFor({ timeout: 10000 });
    await Promise.all([
      page.waitForResponse(v1(/\/links$/, 201), { timeout: 10000 }),
      candidate.click(),
    ]);
    await page.locator(".link-list li").first().waitFor({ timeout: 10000 });
    // A release, evaluated against the newest finished run: five rules.
    await page.goto(base + "/p/" + project + "/releases");
    await page.getByRole("button", { name: "New release" }).first().click();
    await page.fill("#rel-version", "live-" + stamp);
    await Promise.all([
      page.waitForResponse(v1(/\/releases$/, 201), { timeout: 10000 }),
      page.getByRole("button", { name: "Create", exact: true }).click(),
    ]);
    await Promise.all([
      page.waitForResponse(v1(/\/evaluate$/), { timeout: 10000 }),
      page.getByRole("button", { name: "Evaluate gate" }).first().click(),
    ]);
    await page.locator(".gate-rules li").nth(4).waitFor({ timeout: 10000 });
    const verdict = await page
      .locator(".gate-badge")
      .first()
      .getAttribute("data-verdict");
    if (!["approved", "warn", "blocked"].includes(verdict))
      throw new Error("gate verdict " + verdict + " (a finished run exists)");
    await page.screenshot({
      path: "test-results/live-release.png",
      fullPage: true,
    });
  },
);

await step(
  "test plan: record a verdict, attach evidence (multipart → Artifact), thumbnail served by the content route (§6.8.2)",
  async () => {
    const stamp = Date.now().toString(36);
    await page.goto(base + "/p/" + project + "/plans");
    await page.getByRole("button", { name: "New plan" }).first().click();
    await page.fill("#plan-name", "Live check plan " + stamp);
    await Promise.all([
      page.waitForResponse(v1(/\/test-plans$/, 201), { timeout: 10000 }),
      page.getByRole("button", { name: "Create", exact: true }).click(),
    ]);
    await page.waitForURL(/\/plans\//, { timeout: 10000 });
    // The case created in the previous step is the newest row; the verdict
    // is one click, then the hidden picker takes a real PNG.
    const row = page
      .locator(".execution-list li", { hasText: "Live check case " })
      .first();
    await row.waitFor({ timeout: 10000 });
    await Promise.all([
      page.waitForResponse(v1(/\/executions$/, 201), { timeout: 10000 }),
      row.getByRole("button", { name: "failed", exact: true }).click(),
    ]);
    await row.locator(".evidence-attach:not([disabled])").waitFor({
      timeout: 10000,
    });
    const [upload] = await Promise.all([
      page.waitForResponse(v1(/\/evidence$/, 201), { timeout: 20000 }),
      row.locator("input[type=file]").setInputFiles({
        name: "live-evidence.png",
        mimeType: "image/png",
        buffer: tinyPng(),
      }),
    ]);
    const ct = upload.request().headers()["content-type"] || "";
    if (!ct.startsWith("multipart/form-data"))
      throw new Error("evidence was not sent as multipart: " + ct);
    const thumb = row.locator(".evidence-thumb").first();
    await thumb.waitFor({ timeout: 10000 });
    const width = await thumb.evaluate(
      (img) =>
        new Promise((ok) => {
          if (img.complete) ok(img.naturalWidth);
          else {
            img.onload = () => ok(img.naturalWidth);
            img.onerror = () => ok(-1);
          }
        }),
    );
    if (width !== 1)
      throw new Error(
        "evidence thumbnail did not decode (naturalWidth " + width + ")",
      );
    await page.screenshot({
      path: "test-results/live-plan-evidence.png",
      fullPage: true,
    });
  },
);

await step("sign out → session gone, anonymous bootstrap again", async () => {
  const btn = page
    .locator("button[aria-label='Sign out'], button:has-text('Sign out')")
    .first();
  await btn.waitFor({ timeout: 5000 });
  await Promise.all([
    page.waitForResponse((r) => /\/v1\/session\/logout$/.test(r.url()), {
      timeout: 10000,
    }),
    btn.click(),
  ]);
  await page.waitForSelector(emailField, { timeout: 10000 });
});

await page.screenshot({ path: "test-results/live-ok.png", fullPage: true });
await browser.close();
report();

const bad = calls.filter(
  (c) => !/→ (200|201|204)$/.test(c) && !/POST \/v1\/session → 401$/.test(c),
);
if (bad.length) {
  console.log(
    "\nunexpected statuses (only the deliberate wrong-password 401 is allowed):",
  );
  for (const c of bad) console.log("  " + c);
  process.exit(1);
}
if (errors.length) process.exit(1);
console.log(
  `\n${calls.length} /v1 calls, all 2xx except the deliberate 401; no browser errors.`,
);
// Wall time per step, browser included: navigation, every /v1 round trip,
// render until the rows are in the DOM. Run against a project with 10^5–10^6
// results to profile the UI at customer scale (the api side is measured by
// backend/test/load); the fixed waits inside steps are part of the number.
console.log(
  "step timings (ms, wall, includes fixed waits):\n  " +
    timings.map(([n, ms]) => `${String(ms).padStart(5)}  ${n}`).join("\n  "),
);
