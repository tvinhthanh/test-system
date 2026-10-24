// Executable test steps (QĐ-16). A case is a list of steps from a fixed set
// of actions — written by a tester in the dashboard or drafted by a model —
// and every step is data interpreted here with Playwright's own locators.
// Nothing a model writes ever runs as a program on the server.
//
// A step (the same shape everywhere: API, database, model output):
//   { do, by, role, name, value, say, expected }
//   do        one of ACTIONS
//   by        how to find the element: role | label | placeholder | text |
//             testid | css, or "" for steps without one
//   role      the ARIA role when by = role ("button", "textbox", …)
//   name      the accessible name, label, placeholder, text, test id or CSS
//   value     what to type / pick / press, the address, the text expected,
//             or milliseconds to wait
//   say, expected   human words, shown in reports; not used to run
// {{name}} in name or value is a variable: the case's inputs, the account's
// {{username}} and {{password}}, and {{random}} / {{timestamp}}.

export const ACTIONS = [
  "goto", "click", "fill", "select", "check", "uncheck", "press", "hover", "wait",
  "expect_visible", "expect_hidden", "expect_text", "expect_url", "expect_value", "expect_title",
];
export const BY = ["role", "label", "placeholder", "text", "testid", "css"];
// The roles getByRole is asked for — the ARIA roles a page shows a user.
export const ROLES = [
  "alert", "alertdialog", "article", "banner", "button", "cell", "checkbox", "columnheader", "combobox",
  "complementary", "contentinfo", "dialog", "form", "grid", "gridcell", "heading", "img", "link", "list",
  "listbox", "listitem", "log", "main", "menu", "menubar", "menuitem", "menuitemcheckbox", "menuitemradio",
  "navigation", "option", "paragraph", "progressbar", "radio", "radiogroup", "region", "row", "rowheader",
  "search", "searchbox", "separator", "slider", "spinbutton", "status", "switch", "tab", "table", "tablist",
  "tabpanel", "textbox", "toolbar", "tooltip", "tree", "treeitem",
];
const TARGET = new Set(["click", "fill", "select", "check", "uncheck", "hover", "expect_visible", "expect_hidden", "expect_value"]);
const OPTIONAL_TARGET = new Set(["press", "expect_text"]);
const VALUE = new Set(["goto", "fill", "select", "press", "wait", "expect_text", "expect_url", "expect_value", "expect_title"]);

export const MAX_STEPS = 40;
export const ACTION_MS = 10_000;
export const NAV_MS = 30_000;
const SETTLE_MS = 1_500;
const MAX_WAIT_MS = 10_000;

/** Why a step did not pass. kind: input | locator | assert | navigation | page */
export class StepError extends Error {
  constructor(kind, message) {
    super(message);
    this.kind = kind;
  }
}

const VAR = /\{\{\s*([A-Za-z_][A-Za-z0-9_]{0,39})\s*\}\}/g;

/** Replaces {{name}} with vars[name]; an unknown name is an input error. */
export function substitute(text, vars) {
  return String(text ?? "").replace(VAR, (_, name) => {
    if (!Object.hasOwn(vars, name)) throw new StepError("input", `unknown variable {{${name}}}`);
    return String(vars[name]);
  });
}

/** The first problem with a step's shape, or "" — the server checks too. */
export function invalidStep(s) {
  if (!s || typeof s !== "object") return "a step must be an object";
  for (const k of ["do", "by", "role", "name", "value", "say", "expected"])
    if (typeof (s[k] ?? "") !== "string") return `${k} must be a string`;
  if (!ACTIONS.includes(s.do)) return `unknown action ${JSON.stringify(s.do)}`;
  const by = s.by ?? "";
  if (by && !BY.includes(by)) return `unknown locator ${JSON.stringify(by)}`;
  if (TARGET.has(s.do) && !by) return `${s.do} needs an element`;
  if (!TARGET.has(s.do) && !OPTIONAL_TARGET.has(s.do) && by) return `${s.do} takes no element`;
  if (by === "role" && !ROLES.includes(s.role)) return `unknown role ${JSON.stringify(s.role)}`;
  if (by && by !== "role" && !s.name) return `${by} needs a value`;
  if (VALUE.has(s.do) && s.do !== "fill" && s.do !== "expect_value" && !s.value) return `${s.do} needs a value`;
  if (s.do === "wait" && !(Number(s.value) > 0 && Number(s.value) <= MAX_WAIT_MS)) return `wait is 1 to ${MAX_WAIT_MS} ms`;
  for (const k of ["name", "value"]) if ((s[k] ?? "").length > 2000) return `${k} is over 2000 characters`;
  return "";
}

/**
 * The Playwright locator a step names. Names match the way a person reads
 * them: an exact match wins over one that merely contains the words (a
 * field labelled "Message" over a list labelled "Messages"), and of several
 * equal matches the first on the page — see resolve().
 */
export function locate(page, s, vars, exact = false) {
  const name = substitute(s.name, vars);
  switch (s.by) {
    case "role":
      return page.getByRole(s.role, name ? { name, exact } : {});
    case "label":
      return page.getByLabel(name, { exact });
    case "placeholder":
      return page.getByPlaceholder(name, { exact });
    case "text":
      return page.getByText(name, { exact });
    case "testid":
      return page.getByTestId(name);
    case "css":
      return page.locator(name);
  }
  return null;
}

// Waits (up to ACTION_MS) until something matches, then prefers the exact
// match. Nothing matching is left to the action, which reports it.
async function resolve(page, s, vars) {
  const loose = locate(page, s, vars);
  if (s.by === "css" || s.by === "testid" || (s.by === "role" && !s.name)) return loose.first();
  await loose.first().waitFor({ state: "attached", timeout: ACTION_MS }).catch(() => {});
  const exact = locate(page, s, vars, true);
  return (await exact.count().catch(() => 0)) > 0 ? exact.first() : loose.first();
}

const firstLine = (err) => String(err?.message ?? err).split("\n")[0].replace(/\s+/g, " ").slice(0, 300);
const settle = (page) => page.waitForLoadState("networkidle", { timeout: SETTLE_MS }).catch(() => {});

async function until(check, what) {
  const end = Date.now() + ACTION_MS;
  let last = "";
  for (;;) {
    try {
      const r = await check();
      if (r === true) return;
      last = r || "";
    } catch (err) {
      last = firstLine(err);
    }
    if (Date.now() > end) throw new StepError("assert", last ? `${what} — ${last}` : what);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/**
 * Runs one step on page. Resolves when it passed; throws StepError.
 * @param {import("playwright").Page} page
 * @param {object} s a step (invalidStep(s) === "")
 * @param {{vars: Record<string,string>, base: URL, allowed: (host: string) => boolean}} ctx
 */
export async function runStep(page, s, ctx) {
  const bad = invalidStep(s);
  if (bad) throw new StepError("input", bad);
  const value = substitute(s.value, ctx.vars);
  const loc = s.by ? await resolve(page, s, ctx.vars) : null;
  const act = async (fn) => {
    try {
      await fn();
    } catch (err) {
      if (err instanceof StepError) throw err;
      throw new StepError("locator", firstLine(err));
    }
    await settle(page);
  };
  switch (s.do) {
    case "goto": {
      let url;
      try {
        url = new URL(value, ctx.base);
      } catch {
        throw new StepError("input", `not an address: ${value}`);
      }
      if (url.protocol !== "http:" && url.protocol !== "https:") throw new StepError("input", "only http(s) addresses");
      if (!ctx.allowed(url.hostname)) throw new StepError("navigation", `${url.host} is not the site under test`);
      let res;
      try {
        res = await page.goto(url.href, { waitUntil: "load", timeout: NAV_MS });
      } catch (err) {
        throw new StepError("navigation", firstLine(err));
      }
      if (res?.headers()["x-atp-blocked"]) throw new StepError("navigation", `${url.host} may not be opened from this server`);
      if (res && res.status() >= 500) throw new StepError("navigation", `the server answered ${res.status()}`);
      await settle(page);
      return;
    }
    case "click":
      return act(() => loc.click({ timeout: ACTION_MS }));
    case "fill":
      return act(() => loc.fill(value, { timeout: ACTION_MS }));
    case "select":
      return act(async () => {
        try {
          await loc.selectOption({ label: value }, { timeout: ACTION_MS });
        } catch {
          await loc.selectOption(value, { timeout: ACTION_MS });
        }
      });
    case "check":
      return act(() => loc.check({ timeout: ACTION_MS }));
    case "uncheck":
      return act(() => loc.uncheck({ timeout: ACTION_MS }));
    case "press":
      return act(() => (loc ? loc.press(value, { timeout: ACTION_MS }) : page.keyboard.press(value)));
    case "hover":
      return act(() => loc.hover({ timeout: ACTION_MS }));
    case "wait":
      await page.waitForTimeout(Number(value));
      return;
    case "expect_visible":
      return until(async () => (await loc.isVisible()) || "", `${describe(s, ctx.vars)} is not visible`);
    case "expect_hidden":
      return until(async () => !(await loc.isVisible()) || "", `${describe(s, ctx.vars)} is still visible`);
    case "expect_text":
      return until(async () => {
        const text = loc ? await loc.innerText({ timeout: 1000 }) : await page.locator("body").innerText({ timeout: 1000 });
        return text.includes(value) || "";
      }, `the text “${cut(value, 120)}” is not on ${loc ? describe(s, ctx.vars) : "the page"}`);
    case "expect_url":
      return until(async () => page.url().includes(value) || `the address is ${page.url()}`, `the address does not contain “${cut(value, 200)}”`);
    case "expect_value":
      return until(async () => {
        const v = await loc.inputValue({ timeout: 1000 });
        return v === value || `it holds “${cut(v, 120)}”`;
      }, `${describe(s, ctx.vars)} does not hold “${cut(value, 120)}”`);
    case "expect_title":
      return until(async () => {
        const t = await page.title();
        return t.includes(value) || `the title is “${cut(t, 120)}”`;
      }, `the page title does not contain “${cut(value, 120)}”`);
  }
  throw new StepError("input", `unknown action ${s.do}`);
}

const cut = (s, n) => (s.length > n ? s.slice(0, n - 1) + "…" : s);

/** "button “Send”", "the field labelled “Message”" — for messages. */
export function describe(s, vars = {}) {
  let name = s.name;
  try {
    name = substitute(s.name, vars);
  } catch {}
  switch (s.by) {
    case "role":
      return name ? `${s.role} “${cut(name, 80)}”` : `a ${s.role}`;
    case "label":
      return `the field labelled “${cut(name, 80)}”`;
    case "placeholder":
      return `the field with placeholder “${cut(name, 80)}”`;
    case "text":
      return `the text “${cut(name, 80)}”`;
    case "testid":
      return `the element with test id “${cut(name, 80)}”`;
    case "css":
      return `the element ${cut(name, 80)}`;
  }
  return "the page";
}

/**
 * What a model reads to write or repair steps: the page's accessibility
 * tree — the roles and names getByRole matches — plus its address and title.
 * The page is untrusted; the caller says so to the model.
 */
export async function lookAt(page, limit = 16_000) {
  let tree = "";
  try {
    tree = await page.locator("body").ariaSnapshot({ timeout: 5_000 });
  } catch (err) {
    tree = `(the page could not be read: ${firstLine(err)})`;
  }
  if (tree.length > limit) tree = tree.slice(0, limit) + "\n… (cut)";
  return { url: page.url(), title: await page.title().catch(() => ""), tree };
}

/**
 * A site is a host and its siblings under the same registrable name
 * (app.example.com and auth.example.com; shop.example.co.uk and
 * id.example.co.uk), so a sign-in on a neighbouring host works and the
 * browser goes nowhere else.
 */
export function siteOf(host) {
  const h = host.toLowerCase().replace(/\.$/, "");
  if (/^\d+\.\d+\.\d+\.\d+$/.test(h) || h.includes(":")) return h;
  const labels = h.split(".");
  if (labels.length <= 2) return h;
  const second = labels.at(-2);
  const keep = labels.at(-1).length === 2 && /^(co|com|net|org|gov|edu|ac|or|ne|go|biz|info)$/.test(second) ? 3 : 2;
  return labels.slice(-keep).join(".");
}
