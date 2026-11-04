// A browser session for running test steps (QĐ-16): one context behind its
// own guarded proxy (proxy.mjs), held open while the ATP worker sends steps
// one call at a time — so between two steps the worker can ask a model to
// repair a step that no longer finds its element. Top-level navigation
// stays on the site under test (steps.mjs siteOf): a link, a redirect or a
// page script that sends the browser elsewhere is stopped, which also keeps
// a page that talks a model into "go to …" from going anywhere.
import { randomUUID } from "node:crypto";
import { defaultUA } from "./check.mjs";
import { contextProxy, deviceOptions } from "./engines.mjs";
import { startProxy } from "./proxy.mjs";
import { lookAt, runStep, siteOf, StepError } from "./steps.mjs";

const USER_AGENT_SUFFIX = " ATP-Auto-Test/1";

export class Session {
  /**
   * @param {import("playwright").Browser} browser
   * @param {{base: string, policy: object, engine?: string, device?: string}} opts
   *   engine: the browser's engine name (chromium | firefox | webkit); device: desktop | phone | tablet
   */
  static async open(browser, { base, policy, engine = "chromium", device = "desktop" }) {
    const s = new Session();
    s.id = randomUUID();
    s.base = new URL(base);
    s.site = siteOf(s.base.hostname);
    s.engine = engine;
    s.device = device;
    s.proxy = await startProxy(policy);
    const dev = deviceOptions(engine, device);
    s.ctx = await browser.newContext({
      ...dev,
      proxy: contextProxy(engine, s.proxy.url),
      ignoreHTTPSErrors: true,
      serviceWorkers: "block",
      acceptDownloads: false,
      userAgent: (dev.userAgent ?? (await defaultUA(browser))) + USER_AGENT_SUFFIX,
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
      // Chromium blocks or upgrades mixed content and says so here only.
      if (/^Mixed Content:/.test(m.text()) && s.plain.length < 20) s.plain.push(m.text().slice(0, 300));
    });
    // What check_security reads (inspect.mjs): the main document's response
    // and plain-HTTP requests made from an HTTPS page since it loaded.
    s.doc = null;
    s.plain = [];
    s.page.on("response", (res) => {
      const req = res.request();
      if (req.frame() !== s.page.mainFrame() || !req.isNavigationRequest()) return;
      if (res.status() >= 300 && res.status() < 400) return;
      s.doc = { url: res.url(), status: res.status(), headers: res.headers() };
      s.plain = [];
    });
    s.page.on("request", (req) => {
      if (req.isNavigationRequest() && req.frame() === s.page.mainFrame()) return;
      if (req.url().startsWith("http:") && s.page.url().startsWith("https:") && s.plain.length < 20) s.plain.push(`${req.resourceType()} ${req.url().slice(0, 250)}`);
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
    return { vars, base: this.base, allowed: (h) => this.allowed(h), seen: { doc: () => this.doc, plain: () => this.plain } };
  }

  /**
   * Runs steps in order and stops at the first that does not pass. A check
   * step's details are its result's note, passed or failed.
   * @returns {Promise<{results: {status: string, kind?: string, error?: string, note?: string, ms: number}[], ok: boolean}>}
   */
  async run(steps, vars) {
    this.used = Date.now();
    const results = [];
    for (const step of steps) {
      const started = Date.now();
      const leftBefore = this.left.length;
      let failure = null;
      let note = "";
      try {
        note = (await runStep(this.page, step, this.ctxFor(vars))) || "";
      } catch (err) {
        failure = { kind: err instanceof StepError ? err.kind : "page", error: String(err?.message ?? err).split("\n")[0].slice(0, 300) };
        if (err?.note) failure.note = String(err.note).slice(0, 1000);
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
      results.push(note ? { status: "passed", note: note.slice(0, 1000), ms: Date.now() - started } : { status: "passed", ms: Date.now() - started });
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
