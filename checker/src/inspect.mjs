// Check steps (QĐ-19): a case can stop and check the page it is on —
// accessibility (axe-core), speed (the browser's own timings) and what a
// browser can see of security (headers, cookies, plain-HTTP requests). They
// sit between the action steps, so a page is checked in the state a user
// reaches: signed in, after a search, with a dialog open.
//
// Each check returns {ok, error, note}: error is the one line that says why
// it failed, note the details a tester reads (also when it passed). The
// value of the step tunes the check; the server validates it first
// (backend/internal/autotest/checks.go) and so does invalidCheckValue here.
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";

const require = createRequire(import.meta.url);
export const axeSource = readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");

export const CHECK_ACTIONS = ["check_accessibility", "check_performance", "check_security"];

const IMPACTS = ["minor", "moderate", "serious", "critical"];
// The "good" thresholds of Core Web Vitals (web.dev), TTFB and FCP too.
export const DEFAULT_BUDGET = { lcp: 2500, cls: 0.1, ttfb: 800 };
const METRICS = { lcp: "Largest content", fcp: "First content", ttfb: "First byte", load: "Page load", cls: "Layout shift" };
const NOTE = 1000;

const cut = (s, n) => {
  const t = String(s ?? "").replace(/\s+/g, " ").trim();
  return t.length > n ? t.slice(0, n - 1) + "…" : t;
};
const ms = (n) => (n >= 1000 ? `${(n / 1000).toFixed(1)} s` : `${Math.round(n)} ms`);
const show = (key, v) => (key === "cls" ? String(v) : ms(v));

/** "lcp=2500 cls=0.1" → {lcp: 2500, cls: 0.1}; "" → the default budget. Throws on anything else. */
export function parseBudget(value) {
  const text = String(value ?? "").trim();
  if (!text) return { ...DEFAULT_BUDGET };
  const out = {};
  for (const part of text.split(/[\s,;]+/).filter(Boolean)) {
    const m = /^([a-z]+)\s*(?:=|<=|<)\s*([0-9]+(?:\.[0-9]+)?)$/i.exec(part);
    if (!m) throw new Error(`budget “${part}” is not metric=number (lcp, fcp, ttfb, load in ms; cls)`);
    const key = m[1].toLowerCase();
    if (!Object.hasOwn(METRICS, key)) throw new Error(`unknown metric “${m[1]}”: lcp, fcp, ttfb, load or cls`);
    const n = Number(m[2]);
    if (key === "cls" ? n > 10 : n < 1 || n > 120_000) throw new Error(`${key} budget out of range`);
    out[key] = n;
  }
  return out;
}

/** The first problem with a check step's value, or "". */
export function invalidCheckValue(action, value) {
  const v = String(value ?? "").trim();
  switch (action) {
    case "check_accessibility":
      return v === "" || IMPACTS.includes(v) ? "" : `check_accessibility value is "" or one of ${IMPACTS.join(", ")}`;
    case "check_performance":
      try {
        parseBudget(v);
        return "";
      } catch (err) {
        return err.message;
      }
    case "check_security":
      return v === "" || v === "strict" ? "" : `check_security value is "" or "strict"`;
  }
  return "";
}

// ------------------------------------------------------------ accessibility

/**
 * Runs axe-core (WCAG 2.0/2.1/2.2 A and AA) on the page. Fails on a
 * problem at `value` impact or worse (default serious).
 */
export async function checkAccessibility(page, value) {
  const floor = IMPACTS.indexOf(value || "serious");
  // Through the protocol, not a <script>: a page's CSP does not stop it.
  if (!(await page.evaluate(() => typeof window.axe === "object").catch(() => false))) await page.evaluate(axeSource);
  const violations = await page.evaluate(async () => {
    // eslint-disable-next-line no-undef
    const r = await axe.run(document, {
      runOnly: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"],
      resultTypes: ["violations"],
    });
    return r.violations.map((v) => ({ id: v.id, impact: v.impact || "minor", help: v.help, nodes: v.nodes.length, target: v.nodes[0]?.target?.join(" ") ?? "" }));
  });
  violations.sort((a, b) => IMPACTS.indexOf(b.impact) - IMPACTS.indexOf(a.impact) || b.nodes - a.nodes);
  const line = (v) => `${v.impact}: ${v.help} (${v.id}, ${v.nodes} element${v.nodes > 1 ? "s" : ""}, e.g. ${cut(v.target, 80)})`;
  const failing = violations.filter((v) => IMPACTS.indexOf(v.impact) >= floor);
  const note = cut(violations.map(line).join("; "), NOTE);
  if (failing.length) {
    const names = failing.slice(0, 4).map((v) => `${v.id} ×${v.nodes}`).join(", ");
    return { ok: false, error: `${failing.length} accessibility problem${failing.length > 1 ? "s" : ""} at ${IMPACTS[floor]} or worse: ${names}`, note };
  }
  return {
    ok: true,
    note: violations.length
      ? `No problem at ${IMPACTS[floor]} or worse. Below it: ${note}`
      : "No WCAG A/AA problem found automatically (automatic checks find about a third; keyboard and screen-reader testing still matter).",
  };
}

// -------------------------------------------------------------- performance

// Runs in the page: the timings of the document as it was last loaded.
async function pageTimings() {
  const observed = (type) =>
    new Promise((resolve) => {
      try {
        const po = new PerformanceObserver((list) => {
          po.disconnect();
          resolve(list.getEntries());
        });
        po.observe({ type, buffered: true });
        setTimeout(() => resolve([]), 300);
      } catch {
        resolve(null); // the browser does not report this kind
      }
    });
  const nav = performance.getEntriesByType("navigation")[0];
  const fcp = performance.getEntriesByName("first-contentful-paint")[0];
  const [lcps, shifts] = await Promise.all([observed("largest-contentful-paint"), observed("layout-shift")]);
  let cls = null;
  if (shifts) {
    cls = 0;
    for (const e of shifts) if (!e.hadRecentInput) cls += e.value;
    cls = Math.round(cls * 1000) / 1000;
  }
  return {
    ttfb: nav ? Math.round(nav.responseStart) : null,
    fcp: fcp ? Math.round(fcp.startTime) : null,
    lcp: lcps && lcps.length ? Math.round(lcps.at(-1).startTime) : null,
    load: nav && nav.loadEventEnd > 0 ? Math.round(nav.loadEventEnd) : null,
    cls,
  };
}

/**
 * Compares the page's load timings with a budget. A metric the browser does
 * not report (WebKit has no LCP or layout shift) is noted, not failed. The
 * timings are of the document's last full load: after in-app navigation
 * (no new document) they still describe the first.
 */
export async function checkPerformance(page, value) {
  const budget = parseBudget(value);
  await page.waitForLoadState("load", { timeout: 10_000 }).catch(() => {});
  const t = await page.evaluate(pageTimings);
  const parts = [];
  const over = [];
  const missing = [];
  for (const [key, limit] of Object.entries(budget)) {
    const v = t[key];
    if (v == null) {
      missing.push(key);
      continue;
    }
    const fine = v <= limit;
    parts.push(`${METRICS[key]} ${show(key, v)} ${fine ? "≤" : ">"} ${show(key, limit)}`);
    if (!fine) over.push(`${METRICS[key]} ${show(key, v)} (budget ${show(key, limit)})`);
  }
  for (const key of Object.keys(METRICS)) if (!Object.hasOwn(budget, key) && t[key] != null) parts.push(`${METRICS[key]} ${show(key, t[key])}`);
  if (missing.length) parts.push(`not reported by this browser: ${missing.join(", ")}`);
  const note = cut(parts.join(" · "), NOTE);
  if (over.length) return { ok: false, error: `over budget: ${over.join(", ")}`, note };
  if (missing.length === Object.keys(budget).length) return { ok: false, kind: "page", error: "this browser reported none of the budgeted timings", note };
  return { ok: true, note };
}

// ----------------------------------------------------------------- security

const SESSIONISH = /sess|sid|auth|token|jwt|login|remember/i;

/**
 * What a browser can see of a page's security, without attacking it.
 * @param {{url: string, headers: Record<string,string>} | null} doc the main document's response
 * @param {{name: string, secure: boolean, httpOnly: boolean, sameSite: string}[]} cookies the page's cookies
 * @param {string[]} plain plain-HTTP requests and mixed-content messages from an HTTPS page
 * @param {boolean} passwordField a password field on the page
 * @returns {{level: "high"|"medium"|"low", text: string}[]}
 */
export function securityFindings({ doc, cookies, plain, passwordField }) {
  const out = [];
  const add = (level, text) => out.push({ level, text });
  const https = doc.url.startsWith("https:");
  const h = doc.headers;
  if (!https) {
    add(passwordField ? "high" : "medium", passwordField ? "a password field on a page served over plain HTTP" : "the page is served over plain HTTP");
  } else {
    if (!h["strict-transport-security"]) add("medium", "no Strict-Transport-Security header");
    for (const u of plain.slice(0, 5)) add("high", `mixed content: ${cut(u, 160)}`);
  }
  const csp = h["content-security-policy"] ?? "";
  if (!csp) add("medium", "no Content-Security-Policy header");
  else if (/script-src[^;]*'unsafe-inline'/.test(csp) && !/'nonce-|'sha(256|384|512)-|'strict-dynamic'/.test(csp))
    add("low", "the Content-Security-Policy allows inline scripts ('unsafe-inline')");
  if (!/frame-ancestors/.test(csp) && !h["x-frame-options"]) add("medium", "no clickjacking protection (CSP frame-ancestors or X-Frame-Options)");
  if ((h["x-content-type-options"] ?? "").toLowerCase() !== "nosniff") add("low", "no X-Content-Type-Options: nosniff");
  for (const name of ["server", "x-powered-by", "x-aspnet-version"]) if (/\d/.test(h[name] ?? "")) add("low", `the ${name} header tells the software version: ${cut(h[name], 80)}`);
  if (/^\s*\*\s*$/.test(h["access-control-allow-origin"] ?? "") && /true/i.test(h["access-control-allow-credentials"] ?? ""))
    add("high", "CORS allows any origin with credentials");
  for (const c of cookies.slice(0, 50)) {
    if (https && !c.secure) add(SESSIONISH.test(c.name) ? "medium" : "low", `cookie ${cut(c.name, 60)} is not Secure`);
    if (SESSIONISH.test(c.name) && !c.httpOnly) add("medium", `cookie ${cut(c.name, 60)} looks like a session cookie and is readable by scripts (no HttpOnly)`);
    if (c.sameSite === "None" && !c.secure) add("medium", `cookie ${cut(c.name, 60)} is SameSite=None without Secure`);
  }
  return out;
}

/**
 * Fails on a high finding — or a medium one too when value is "strict".
 * @param {import("playwright").Page} page
 * @param {string} value
 * @param {{doc: () => ({url: string, headers: Record<string,string>} | null), plain: () => string[]}} seen what the session saw
 */
export async function checkSecurity(page, value, seen) {
  const doc = seen.doc();
  if (!doc) return { ok: false, kind: "page", error: "no page has been loaded in this session yet", note: "" };
  const cookies = await page.context().cookies(doc.url).catch(() => []);
  const passwordField = (await page.locator('input[type="password"]').count().catch(() => 0)) > 0;
  const findings = securityFindings({ doc, cookies, plain: seen.plain(), passwordField });
  const rank = { high: 2, medium: 1, low: 0 };
  findings.sort((a, b) => rank[b.level] - rank[a.level]);
  const floor = value === "strict" ? 1 : 2;
  const failing = findings.filter((f) => rank[f.level] >= floor);
  const note = findings.length
    ? cut(findings.map((f) => `${f.level}: ${f.text}`).join("; "), NOTE)
    : "Nothing found: HTTPS with HSTS, a CSP, clickjacking protection, nosniff, safe cookies. A browser sees only part of security; this is no penetration test.";
  if (failing.length) return { ok: false, error: `${failing.length} security finding${failing.length > 1 ? "s" : ""}: ${failing.slice(0, 3).map((f) => f.text).join("; ")}`, note };
  return { ok: true, note };
}
