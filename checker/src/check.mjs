// One URL check (QĐ-14): what a tester looks at first on a page, done by a
// headless Chromium in a few seconds. One page, not a crawl. Every check
// says passed / warning / failed with a one-line summary and up to 20
// details; the report's verdict is the worst of them. The page's text is
// untrusted: details are plain strings, cut to 300 characters.
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import tls from "node:tls";
import { Blocked, resolveAllowed, startProxy } from "./proxy.mjs";

const require = createRequire(import.meta.url);
const axeSource = readFileSync(require.resolve("axe-core/axe.min.js"), "utf8");

const NAV_TIMEOUT_MS = 30_000;
const SETTLE_MS = 3_000;
const LINK_LIMIT = 40;
const LINK_TIMEOUT_MS = 8_000;
const DETAILS = 20;
const DESKTOP = { width: 1366, height: 768 };
const PHONE = { width: 390, height: 844 };
const USER_AGENT_SUFFIX = " ATP-URL-Check/1";

const cut = (s, n = 300) => {
  const t = String(s ?? "").replace(/\s+/g, " ").trim();
  return t.length > n ? t.slice(0, n - 1) + "…" : t;
};
const rank = { passed: 0, not_run: 0, warning: 1, failed: 2 };
const check = (key, title, status, summary, details = []) => ({
  key,
  title,
  status,
  summary: cut(summary, 300),
  details: details.slice(0, DETAILS).map((d) => cut(d)),
});
const ms = (n) => (n >= 1000 ? `${(n / 1000).toFixed(1)} s` : `${Math.round(n)} ms`);

/**
 * @param {import("playwright").Browser} browser
 * @param {string} url http(s) only — the caller has checked
 * @param {{allowPrivate?: boolean, allow?: string[]}} policy see guard.mjs
 */
export async function runCheck(browser, url, policy) {
  const started = Date.now();
  const proxy = await startProxy(policy);
  const contexts = [];
  const newContext = async (opts) => {
    const ctx = await browser.newContext({
      ...opts,
      proxy: { server: proxy.url, bypass: "<-loopback>" },
      ignoreHTTPSErrors: true, // the certificate is its own check below
      serviceWorkers: "block",
      acceptDownloads: false,
      bypassCSP: true, // axe-core is injected into the page
      userAgent: (await defaultUA(browser)) + USER_AGENT_SUFFIX,
    });
    contexts.push(ctx);
    return ctx;
  };
  try {
    const desktop = await newContext({ viewport: DESKTOP });
    const page = await desktop.newPage();
    const consoleErrors = [];
    const pageErrors = [];
    const badResponses = [];
    const failedRequests = [];
    let mainRequest = null;
    page.on("request", (r) => {
      if (!mainRequest && r.isNavigationRequest() && r.frame() === page.mainFrame()) mainRequest = r;
    });
    page.on("console", (m) => {
      if (m.type() === "error" && consoleErrors.length < 50) consoleErrors.push(m.text());
    });
    page.on("pageerror", (e) => {
      if (pageErrors.length < 50) pageErrors.push(e.message);
    });
    page.on("response", (r) => {
      // A guard refusal of plain HTTP: reported under "blocked", not broken.
      if (r.headers()["x-atp-blocked"]) return;
      if (r.status() >= 400 && r.request() !== mainRequest && badResponses.length < 50)
        badResponses.push(`${r.status()} ${r.url()}`);
    });
    page.on("requestfailed", (r) => {
      if (r === mainRequest) return;
      if (failedRequests.length < 50) failedRequests.push({ url: r.url(), why: r.failure()?.errorText ?? "failed" });
    });

    const checks = [];
    let response = null;
    let navError = "";
    try {
      response = await page.goto(url, { waitUntil: "load", timeout: NAV_TIMEOUT_MS });
      await page.waitForLoadState("networkidle", { timeout: SETTLE_MS }).catch(() => {});
    } catch (err) {
      navError = err.message.split("\n")[0];
    }
    const blockedSoFar = proxy.blocked();
    const refused = !!response?.headers()["x-atp-blocked"];
    if (refused) response = null;
    const status = response?.status() ?? 0;
    const finalUrl = page.url();
    const loadMs = Date.now() - started;
    if (!response || status >= 400) {
      const why = blockedSoFar.length && !response
        ? `Not checked: ${blockedSoFar[0].slice(blockedSoFar[0].indexOf(": ") + 2)}`
        : navError
          ? `Did not load: ${navError}`
          : `The server answered ${status}`;
      checks.push(check("load", "Page loads", "failed", why, blockedSoFar));
      return finish({ url, finalUrl, status, title: "", checks, metrics: {}, blocked: proxy.blocked(), started, shots: {} });
    }
    const redirects = [];
    for (let r = response.request().redirectedFrom(); r; r = r.redirectedFrom()) redirects.unshift(r.url());
    checks.push(
      check("load", "Page loads", "passed", `${status} ${response.statusText()} in ${ms(loadMs)}`,
        redirects.length ? [`Redirected from ${redirects.join(" → ")}`] : []),
    );

    checks.push(await certificateCheck(finalUrl, policy));

    checks.push(
      pageErrors.length
        ? check("console", "No JavaScript errors", "failed",
            `${pageErrors.length} uncaught error${pageErrors.length > 1 ? "s" : ""}`, [...pageErrors, ...consoleErrors])
        : consoleErrors.length
          ? check("console", "No JavaScript errors", "warning",
              `${consoleErrors.length} error message${consoleErrors.length > 1 ? "s" : ""} in the console`, consoleErrors)
          : check("console", "No JavaScript errors", "passed", "Nothing in the console"),
    );

    // Guard refusals are reported under "blocked": a refused HTTPS request
    // fails as a tunnel error, a refused cross-origin HTTP one as a CORS
    // failure on the 403 — both are recognised by URL or error, not counted.
    const refusedUrls = proxy.blocked().map((b) => b.slice(0, b.indexOf(": ")));
    const broken = [
      ...badResponses,
      ...failedRequests
        .filter((f) => !/ERR_TUNNEL_CONNECTION_FAILED|ERR_ABORTED/.test(f.why) && !refusedUrls.includes(f.url))
        .map((f) => `${f.why} ${f.url}`),
    ];
    checks.push(
      broken.length
        ? check("requests", "Every resource loads", "failed",
            `${broken.length} image, script or other request failed`, broken)
        : check("requests", "Every resource loads", "passed", "All requests answered"),
    );

    const facts = await page.evaluate(collectFacts).catch(() => null);
    checks.push(await linkCheck(desktop, facts?.links ?? []));
    checks.push(await accessibilityCheck(page));
    checks.push(speedCheck(facts));
    checks.push(basicsCheck(facts));

    const desktopShot = await page.screenshot({ type: "jpeg", quality: 60 }).catch(() => null);
    const phone = await phoneCheck(newContext, url);
    checks.push(phone.check);

    return finish({
      url,
      finalUrl,
      status,
      title: facts?.title ?? "",
      checks,
      metrics: facts?.metrics ?? {},
      blocked: proxy.blocked(),
      started,
      shots: { desktop: desktopShot, mobile: phone.shot },
    });
  } finally {
    await Promise.allSettled(contexts.map((c) => c.close()));
    await proxy.close();
  }
}

let ua = "";
async function defaultUA(browser) {
  if (!ua) {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();
    // Some sites turn "HeadlessChrome" away; the suffix still names the checker.
    ua = (await page.evaluate(() => navigator.userAgent)).replace("HeadlessChrome", "Chrome");
    await ctx.close();
  }
  return ua;
}

function finish({ url, finalUrl, status, title, checks, metrics, blocked, started, shots }) {
  const verdict = checks.reduce((worst, c) => (rank[c.status] > rank[worst] ? c.status : worst), "passed");
  return {
    report: {
      url,
      finalUrl,
      status,
      title: cut(title, 200),
      verdict: verdict === "not_run" ? "passed" : verdict,
      checks,
      metrics,
      blocked: blocked.slice(0, DETAILS).map((b) => cut(b)),
      durationMs: Date.now() - started,
    },
    shots,
  };
}

// Runs in the page. Plain data only. LCP and layout shifts are only handed
// out through a buffered PerformanceObserver, which answers asynchronously
// (or never, when there were none).
async function collectFacts() {
  const observed = (type) =>
    new Promise((resolve) => {
      try {
        const po = new PerformanceObserver((list) => {
          po.disconnect();
          resolve(list.getEntries());
        });
        po.observe({ type, buffered: true });
        setTimeout(() => resolve([]), 200);
      } catch {
        resolve([]);
      }
    });
  const nav = performance.getEntriesByType("navigation")[0];
  const [lcps, shifts] = await Promise.all([observed("largest-contentful-paint"), observed("layout-shift")]);
  const lcp = lcps.at(-1);
  let cls = 0;
  for (const e of shifts) if (!e.hadRecentInput) cls += e.value;
  const resources = performance.getEntriesByType("resource");
  const links = [];
  const seen = new Set();
  for (const a of document.querySelectorAll("a[href]")) {
    let u;
    try {
      u = new URL(a.getAttribute("href"), document.baseURI);
    } catch {
      continue;
    }
    if (u.protocol !== "http:" && u.protocol !== "https:") continue;
    u.hash = "";
    if (u.href === location.href.split("#")[0] || seen.has(u.href)) continue;
    seen.add(u.href);
    links.push(u.href);
  }
  return {
    title: document.title,
    lang: document.documentElement.getAttribute("lang") ?? "",
    description: document.querySelector('meta[name="description"]')?.getAttribute("content") ?? "",
    viewport: document.querySelector('meta[name="viewport"]')?.getAttribute("content") ?? "",
    h1: document.querySelectorAll("h1").length,
    links,
    metrics: {
      ttfbMs: nav ? Math.round(nav.responseStart) : null,
      domContentLoadedMs: nav ? Math.round(nav.domContentLoadedEventEnd) : null,
      loadMs: nav ? Math.round(nav.loadEventEnd) : null,
      lcpMs: lcp ? Math.round(lcp.startTime) : null,
      cls: Math.round(cls * 1000) / 1000,
      requests: resources.length + 1,
      transferBytes: resources.reduce((n, r) => n + (r.transferSize || 0), nav?.transferSize || 0),
    },
  };
}

async function certificateCheck(finalUrl, policy) {
  const u = new URL(finalUrl);
  if (u.protocol !== "https:")
    return check("https", "HTTPS", "warning", "The page is served over plain HTTP — passwords and cookies travel unencrypted");
  try {
    const ip = await resolveAllowed(u.hostname, policy);
    const cert = await new Promise((resolve, reject) => {
      const socket = tls.connect({ host: ip, port: Number(u.port) || 443, servername: u.hostname, rejectUnauthorized: false, timeout: 8000 }, () => {
        const c = socket.getPeerCertificate();
        resolve({ authorized: socket.authorized, error: socket.authorizationError, validTo: c?.valid_to, issuer: c?.issuer?.O ?? c?.issuer?.CN });
        socket.end();
      });
      socket.on("timeout", () => socket.destroy(new Error("TLS handshake timed out")));
      socket.on("error", reject);
    });
    if (!cert.authorized)
      return check("https", "HTTPS", "failed", `Browsers will warn: the certificate is not trusted (${cert.error})`);
    const days = Math.floor((Date.parse(cert.validTo) - Date.now()) / 86_400_000);
    return days < 14
      ? check("https", "HTTPS", "warning", `The certificate expires in ${days} day${days === 1 ? "" : "s"}`, [`Issuer: ${cert.issuer}`])
      : check("https", "HTTPS", "passed", `Trusted certificate, valid ${days} more days`, [`Issuer: ${cert.issuer}`]);
  } catch (err) {
    return check("https", "HTTPS", err instanceof Blocked ? "not_run" : "failed", `Could not check the certificate: ${err.message}`);
  }
}

async function linkCheck(context, links) {
  const title = "Links work";
  if (!links.length) return check("links", title, "passed", "No links on the page");
  const sample = links.slice(0, LINK_LIMIT);
  const broken = [];
  const slow = [];
  let next = 0;
  const one = async () => {
    while (next < sample.length) {
      const href = sample[next++];
      try {
        let r = await context.request.head(href, { timeout: LINK_TIMEOUT_MS, maxRedirects: 5, failOnStatusCode: false });
        const refused = () => !!r.headers()["x-atp-blocked"]; // the guard's: listed under "blocked"
        if (refused()) continue;
        if (r.status() === 405 || r.status() === 501 || r.status() === 403)
          r = await context.request.get(href, { timeout: LINK_TIMEOUT_MS, maxRedirects: 5, failOnStatusCode: false });
        if (refused()) continue;
        if (r.status() >= 400) broken.push(`${r.status()} ${href}`);
      } catch (err) {
        const msg = err.message.split("\n")[0];
        if (/403|Forbidden|tunnel/i.test(msg)) continue; // refused by the guard: listed under "blocked"
        (/timeout/i.test(msg) ? slow : broken).push(`${/timeout/i.test(msg) ? "no answer in 8 s" : msg} ${href}`);
      }
    }
  };
  await Promise.all(Array.from({ length: 6 }, one));
  const more = links.length > sample.length ? ` (first ${sample.length} of ${links.length} checked)` : "";
  if (broken.length) return check("links", title, "failed", `${broken.length} broken link${broken.length > 1 ? "s" : ""}${more}`, [...broken, ...slow]);
  if (slow.length) return check("links", title, "warning", `${slow.length} link${slow.length > 1 ? "s" : ""} did not answer${more}`, slow);
  return check("links", title, "passed", sample.length > 1 ? `All ${sample.length} links answer${more}` : `The one link answers`);
}

async function accessibilityCheck(page) {
  const title = "Accessibility (axe-core, WCAG A/AA)";
  try {
    await page.evaluate(axeSource);
    const violations = await page.evaluate(async () => {
      // eslint-disable-next-line no-undef
      const r = await axe.run(document, { runOnly: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"], resultTypes: ["violations"] });
      return r.violations.map((v) => ({ id: v.id, impact: v.impact, help: v.help, nodes: v.nodes.length, target: v.nodes[0]?.target?.join(" ") ?? "" }));
    });
    const serious = violations.filter((v) => v.impact === "critical" || v.impact === "serious");
    const details = violations
      .sort((a, b) => (b.impact === "critical") - (a.impact === "critical") || (b.impact === "serious") - (a.impact === "serious"))
      .map((v) => `${v.impact}: ${v.help} (${v.id}, ${v.nodes} element${v.nodes > 1 ? "s" : ""}, e.g. ${v.target})`);
    if (serious.length) return check("accessibility", title, "failed", `${serious.length} serious or critical problem${serious.length > 1 ? "s" : ""}`, details);
    if (violations.length) return check("accessibility", title, "warning", `${violations.length} minor or moderate problem${violations.length > 1 ? "s" : ""}`, details);
    return check("accessibility", title, "passed", "No WCAG A/AA problems found automatically", ["Automatic checks find about a third of problems; keyboard and screen-reader testing still matter."]);
  } catch (err) {
    return check("accessibility", title, "not_run", `Could not run: ${err.message.split("\n")[0]}`);
  }
}

function speedCheck(facts) {
  const title = "Speed";
  const m = facts?.metrics;
  if (!m) return check("speed", title, "not_run", "No timing from the page");
  const details = [
    m.ttfbMs != null && `First byte from the server: ${ms(m.ttfbMs)}`,
    m.lcpMs != null && `Largest content visible: ${ms(m.lcpMs)}`,
    m.loadMs != null && `Fully loaded: ${ms(m.loadMs)}`,
    `Layout shift: ${m.cls}`,
    `${m.requests} requests, ${(m.transferBytes / 1024 / 1024).toFixed(1)} MB transferred`,
  ].filter(Boolean);
  const paint = m.lcpMs ?? m.loadMs ?? 0;
  if (paint > 4000 || m.cls > 0.25)
    return check("speed", title, "failed", paint > 4000 ? `Main content appears after ${ms(paint)} (good: under 2.5 s)` : `The layout jumps while loading (shift ${m.cls}, good: under 0.1)`, details);
  if (paint > 2500 || m.cls > 0.1)
    return check("speed", title, "warning", paint > 2500 ? `Main content appears after ${ms(paint)} (good: under 2.5 s)` : `The layout shifts while loading (${m.cls}, good: under 0.1)`, details);
  return check("speed", title, "passed", `Main content appears after ${ms(paint)}`, details);
}

function basicsCheck(facts) {
  const title = "Page basics";
  if (!facts) return check("basics", title, "not_run", "Could not read the page");
  const missing = [];
  if (!facts.title.trim()) missing.push("No <title> — tabs and search results show the address instead");
  if (!facts.lang) missing.push('No lang on <html> — screen readers guess the language');
  if (!facts.description.trim()) missing.push("No meta description — search engines make one up");
  if (facts.h1 === 0) missing.push("No <h1> heading");
  return missing.length
    ? check("basics", title, "warning", `${missing.length} thing${missing.length > 1 ? "s" : ""} missing`, missing)
    : check("basics", title, "passed", `“${cut(facts.title, 80)}”`);
}

async function phoneCheck(newContext, url) {
  const title = "Fits a phone screen";
  try {
    const ctx = await newContext({ viewport: PHONE, deviceScaleFactor: 2, isMobile: true, hasTouch: true });
    const page = await ctx.newPage();
    await page.goto(url, { waitUntil: "load", timeout: NAV_TIMEOUT_MS });
    await page.waitForLoadState("networkidle", { timeout: SETTLE_MS }).catch(() => {});
    const fit = await page.evaluate(() => ({
      overflow: document.documentElement.scrollWidth - window.innerWidth,
      viewport: !!document.querySelector('meta[name="viewport"]'),
    }));
    const shot = await page.screenshot({ type: "jpeg", quality: 60 }).catch(() => null);
    const problems = [];
    if (!fit.viewport) problems.push("No viewport meta tag — phones show the desktop layout zoomed out");
    if (fit.overflow > 1) problems.push(`The page is ${fit.overflow}px wider than a ${PHONE.width}px phone screen — it scrolls sideways`);
    return {
      shot,
      check: problems.length
        ? check("mobile", title, "warning", problems[0], problems)
        : check("mobile", title, "passed", `No sideways scrolling at ${PHONE.width}px`),
    };
  } catch (err) {
    return { shot: null, check: check("mobile", title, "not_run", `Could not open on a phone screen: ${err.message.split("\n")[0]}`) };
  }
}

/**
 * The structure of one page, for drafting test cases (QĐ-15): title,
 * headings, same-site links, forms with their fields, buttons and the start
 * of the visible text. Opened behind the same guarded proxy as a check. The
 * page is untrusted: everything is plain, cut text — the caller tells the
 * model so.
 */
export async function readOutline(browser, url, policy) {
  const proxy = await startProxy(policy);
  const ctx = await browser.newContext({
    viewport: DESKTOP,
    proxy: { server: proxy.url, bypass: "<-loopback>" },
    ignoreHTTPSErrors: true,
    serviceWorkers: "block",
    acceptDownloads: false,
    userAgent: (await defaultUA(browser)) + USER_AGENT_SUFFIX,
  });
  try {
    const page = await ctx.newPage();
    const response = await page.goto(url, { waitUntil: "load", timeout: NAV_TIMEOUT_MS });
    if (!response || response.headers()["x-atp-blocked"])
      throw new Error(proxy.blocked()[0] ?? "the page did not load");
    await page.waitForLoadState("networkidle", { timeout: SETTLE_MS }).catch(() => {});
    const outline = await page.evaluate(() => {
      const t = (s, n = 120) => (s ?? "").replace(/\s+/g, " ").trim().slice(0, n);
      const label = (el) =>
        t(
          (el.id && document.querySelector(`label[for="${CSS.escape(el.id)}"]`)?.textContent) ||
            el.closest("label")?.textContent ||
            el.getAttribute("aria-label") ||
            el.getAttribute("placeholder") ||
            el.getAttribute("name"),
          80,
        );
      const seen = new Set();
      const links = [];
      for (const a of document.querySelectorAll("a[href]")) {
        let u;
        try {
          u = new URL(a.getAttribute("href"), document.baseURI);
        } catch {
          continue;
        }
        if (u.origin !== location.origin || seen.has(u.pathname) || links.length >= 60) continue;
        seen.add(u.pathname);
        const text = t(a.textContent || a.getAttribute("aria-label"), 60);
        if (text) links.push({ text, path: u.pathname });
      }
      const forms = [...document.querySelectorAll("form")].slice(0, 10).map((f) => ({
        name: t(f.getAttribute("aria-label") || f.getAttribute("name") || f.id, 60),
        fields: [...f.querySelectorAll("input, select, textarea")]
          .filter((el) => !["hidden", "submit", "button"].includes(el.type))
          .slice(0, 20)
          .map((el) => ({ label: label(el), type: el.type || el.tagName.toLowerCase(), required: el.required })),
        submit: t(f.querySelector('button[type="submit"], input[type="submit"], button')?.textContent || f.querySelector('input[type="submit"]')?.value, 60),
      }));
      return {
        title: t(document.title, 200),
        lang: document.documentElement.lang || "",
        headings: [...document.querySelectorAll("h1, h2, h3")].slice(0, 40).map((h) => `${h.tagName.toLowerCase()}: ${t(h.textContent)}`),
        links,
        forms,
        buttons: [...new Set([...document.querySelectorAll('button, [role="button"]')].map((b) => t(b.textContent || b.getAttribute("aria-label"), 60)).filter(Boolean))].slice(0, 40),
        text: t((document.querySelector("main") ?? document.body)?.innerText, 4000),
      };
    });
    return { url: page.url(), ...outline };
  } finally {
    await ctx.close().catch(() => {});
    await proxy.close();
  }
}
