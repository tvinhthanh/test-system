// A browser session for running test steps (QĐ-16): one context behind its
// own guarded proxy (proxy.mjs), held open while the ATP worker sends steps
// one call at a time — so between two steps the worker can ask a model to
// repair a step that no longer finds its element. Top-level navigation
// stays on the site under test (steps.mjs siteOf): a link, a redirect or a
// page script that sends the browser elsewhere is stopped, which also keeps
// a page that talks a model into "go to …" from going anywhere.
import { randomUUID } from "node:crypto";
import { defaultUA } from "./check.mjs";
import { startProxy } from "./proxy.mjs";
import { lookAt, runStep, siteOf, StepError } from "./steps.mjs";

const DESKTOP = { width: 1366, height: 768 };
const USER_AGENT_SUFFIX = " ATP-Auto-Test/1";

export class Session {
  /** @param {import("playwright").Browser} browser */
  static async open(browser, { base, policy }) {
    const s = new Session();
    s.id = randomUUID();
    s.base = new URL(base);
    s.site = siteOf(s.base.hostname);
    s.proxy = await startProxy(policy);
    s.ctx = await browser.newContext({
      viewport: DESKTOP,
      proxy: { server: s.proxy.url, bypass: "<-loopback>" },
      ignoreHTTPSErrors: true,
      serviceWorkers: "block",
      acceptDownloads: false,
      userAgent: (await defaultUA(browser)) + USER_AGENT_SUFFIX,
    });
    s.console = [];
    s.left = [];
    s.created = s.used = Date.now();
    s.page = await s.ctx.newPage();
    // A second tab (target=_blank, window.open) is closed: steps act on one page.
    s.ctx.on("page", (p) => {
      if (p !== s.page) p.close().catch(() => {});
    });
    s.page.on("console", (m) => {
      if (m.type() === "error") s.note(`console: ${m.text()}`);
    });
    s.page.on("pageerror", (e) => s.note(`uncaught: ${e.message}`));
    // The last address on the site, to come back to after a stopped navigation.
    s.lastGood = "";
    s.page.on("framenavigated", (frame) => {
      if (frame !== s.page.mainFrame()) return;
      try {
        const u = new URL(frame.url());
        if ((u.protocol === "http:" || u.protocol === "https:") && s.allowed(u.hostname)) s.lastGood = u.href;
      } catch {}
    });
    await s.page.route("**/*", (route) => {
      const req = route.request();
      if (req.isNavigationRequest() && req.frame() === s.page.mainFrame()) {
        let host = "";
        try {
          host = new URL(req.url()).hostname;
        } catch {}
        if (!s.allowed(host)) {
          if (s.left.length < 20) s.left.push(req.url().slice(0, 300));
          return route.abort("blockedbyclient");
        }
      }
      return route.continue();
    });
    return s;
  }

  allowed(host) {
    const h = String(host).toLowerCase();
    return h === this.base.hostname || siteOf(h) === this.site;
  }

  note(line) {
    if (this.console.length < 50) this.console.push(String(line).replace(/\s+/g, " ").slice(0, 300));
  }

  /** Console lines since the last call. */
  drain() {
    const out = this.console;
    this.console = [];
    return out;
  }

  ctxFor(vars) {
    return { vars, base: this.base, allowed: (h) => this.allowed(h) };
  }

  /**
   * Runs steps in order and stops at the first that does not pass.
   * @returns {Promise<{results: {status: string, kind?: string, error?: string, ms: number}[], ok: boolean}>}
   */
  async run(steps, vars) {
    this.used = Date.now();
    const results = [];
    for (const step of steps) {
      const started = Date.now();
      const leftBefore = this.left.length;
      let failure = null;
      try {
        await runStep(this.page, step, this.ctxFor(vars));
      } catch (err) {
        failure = { kind: err instanceof StepError ? err.kind : "page", error: String(err?.message ?? err).split("\n")[0].slice(0, 300) };
      }
      // A step that sent the browser off the site (a link, a redirect, a
      // script) leaves Chromium on its error page: go back to the site and
      // say where it tried to go.
      if (this.left.length > leftBefore) {
        const where = this.left.at(-1);
        await this.comeBack();
        failure = { kind: "navigation", error: `tried to leave the site under test for ${where}; the browser stayed on ${this.page.url()}` };
      }
      if (failure) {
        results.push({ status: "failed", ...failure, ms: Date.now() - started });
        this.used = Date.now();
        return { results, ok: false };
      }
      results.push({ status: "passed", ms: Date.now() - started });
    }
    this.used = Date.now();
    return { results, ok: true };
  }

  /**
   * Signs in: opens the sign-in page, runs the steps, and calls it done
   * when no password field is left on screen.
   */
  async login({ url, steps, vars }) {
    const start = url ? [{ do: "goto", by: "", role: "", name: "", value: url, say: "", expected: "" }] : [];
    const { results, ok } = await this.run([...start, ...steps], vars);
    if (!ok) {
      const failed = results.at(-1);
      return { loggedIn: false, error: `sign-in step ${results.length - start.length} failed: ${failed.error}` };
    }
    await this.page.waitForLoadState("networkidle", { timeout: 3_000 }).catch(() => {});
    const passwords = await this.page.locator('input[type="password"]').filter({ visible: true }).count().catch(() => 0);
    if (passwords > 0) {
      const alert = await this.page.getByRole("alert").first().innerText({ timeout: 500 }).catch(() => "");
      return {
        loggedIn: false,
        error: `still on the sign-in page after the sign-in steps${alert ? ` — the page says “${alert.replace(/\s+/g, " ").slice(0, 200)}”` : ""}`,
      };
    }
    return { loggedIn: true };
  }

  async comeBack() {
    if (!this.page.url().startsWith("chrome-error:")) return;
    await this.page.goBack({ timeout: 10_000 }).catch(() => null);
    if (this.page.url().startsWith("chrome-error:") && this.lastGood)
      await this.page.goto(this.lastGood, { timeout: 30_000 }).catch(() => null);
  }

  async look({ shot = false } = {}) {
    this.used = Date.now();
    const view = await lookAt(this.page);
    if (shot) view.screenshot = await this.screenshot();
    return view;
  }

  async screenshot() {
    const b = await this.page.screenshot({ type: "jpeg", quality: 60 }).catch(() => null);
    return b ? b.toString("base64") : "";
  }

  async close() {
    await this.ctx.close().catch(() => {});
    await this.proxy.close();
  }
}
