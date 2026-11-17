// Check steps (QĐ-19) on real Chromium against a fixture site: one page
// that does well and one that does badly on each check.
import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { chromium } from "playwright";
import { invalidCheckValue, parseBudget, securityFindings } from "../src/inspect.mjs";
import { startProxy } from "../src/proxy.mjs";
import { Session } from "../src/session.mjs";
import { invalidStep } from "../src/steps.mjs";

const html = (title, body) => `<!doctype html><html lang="en"><head><title>${title}</title></head><body><main>${body}</main></body></html>`;
const good = {
  "Content-Security-Policy": "default-src 'self'; frame-ancestors 'none'",
  "X-Content-Type-Options": "nosniff",
};
const pages = {
  "/good": [good, html("Good", `<h1>Good</h1><label>Name <input></label><button>Save</button>`)],
  "/bad": [
    { Server: "Apache/2.4.1", "Set-Cookie": "session_id=abc; Path=/" },
    html("Bad", `<h1>Bad</h1><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw="><input placeholder="no label"><div style="color:#bbb;background:#fff">faint</div><label>Password <input type="password"></label>`),
  ],
  "/slow": [good, html("Slow", `<h1>Slow</h1><img src="/slow.gif" alt="slow">`)],
};
let site, base, browser, sink;
const policy = { allow: ["127.0.0.1"] };
const step = (s) => ({ do: "", by: "", role: "", name: "", value: "", say: "", expected: "", ...s });

before(async () => {
  site = http.createServer(async (req, res) => {
    const path = new URL(req.url, "http://x").pathname;
    if (path === "/slow.gif") {
      await new Promise((r) => setTimeout(r, 1200));
      return res.writeHead(200, { "Content-Type": "image/gif" }).end(Buffer.from("R0lGODlhAQABAAAAACw=", "base64"));
    }
    const p = pages[path];
    if (!p) return res.writeHead(404).end("not here");
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8", ...p[0] }).end(p[1]);
  });
  await new Promise((r) => site.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${site.address().port}`;
  sink = await startProxy({ denyAll: true });
  browser = await chromium.launch({ proxy: { server: sink.url, bypass: "<-loopback>" } });
});

after(async () => {
  await browser?.close();
  await sink?.close();
  site?.close();
});

async function on(path, steps) {
  const s = await Session.open(browser, { base, policy });
  try {
    const opened = await s.run([step({ do: "goto", value: path })], {});
    assert.ok(opened.ok, JSON.stringify(opened.results));
    return await s.run(steps, {});
  } finally {
    await s.close();
  }
}

test("check_accessibility passes a labelled page and names the problems of one that is not", async () => {
  const fine = await on("/good", [step({ do: "check_accessibility" })]);
  assert.ok(fine.ok, JSON.stringify(fine.results));
  assert.match(fine.results[0].note, /No WCAG A\/AA problem|No problem at serious/);
  const bad = await on("/bad", [step({ do: "check_accessibility" })]);
  assert.equal(bad.ok, false);
  assert.equal(bad.results[0].kind, "assert");
  assert.match(bad.results[0].error, /image-alt|label/);
  assert.match(bad.results[0].note, /critical|serious/);
  // A floor above every problem found lets it pass, with the problems in the note.
  const lenient = await on("/bad", [step({ do: "check_accessibility", value: "critical" })]);
  assert.equal(lenient.ok, !/critical/.test(bad.results[0].note), JSON.stringify(lenient.results));
});

test("check_performance compares the page's timings with a budget", async () => {
  const fine = await on("/good", [step({ do: "check_performance" })]);
  assert.ok(fine.ok, JSON.stringify(fine.results));
  assert.match(fine.results[0].note, /First byte \d+ ms ≤ 800 ms/);
  const slow = await on("/slow", [step({ do: "check_performance", value: "load=500" })]);
  assert.equal(slow.ok, false, JSON.stringify(slow.results));
  assert.match(slow.results[0].error, /over budget: Page load/);
});

test("check_security fails on a password over plain HTTP and lists the rest", async () => {
  const fine = await on("/good", [step({ do: "check_security" })]);
  assert.ok(fine.ok, JSON.stringify(fine.results));
  assert.match(fine.results[0].note, /plain HTTP/); // the fixture is http: medium, not failing by default
  const strict = await on("/good", [step({ do: "check_security", value: "strict" })]);
  assert.equal(strict.ok, false);
  const bad = await on("/bad", [step({ do: "check_security" })]);
  assert.equal(bad.ok, false);
  assert.match(bad.results[0].error, /password field on a page served over plain HTTP/);
  assert.match(bad.results[0].note, /Apache\/2\.4\.1/);
  assert.match(bad.results[0].note, /session_id .*no HttpOnly/);
  assert.match(bad.results[0].note, /no Content-Security-Policy/);
});

test("security findings on HTTPS: HSTS, mixed content, cookies, CORS", () => {
  const doc = { url: "https://app.example.com/", headers: { "access-control-allow-origin": "*", "access-control-allow-credentials": "true" } };
  const f = securityFindings({ doc, cookies: [{ name: "auth", secure: false, httpOnly: true, sameSite: "None" }], plain: ["script http://cdn.example.com/a.js"], passwordField: true });
  const text = f.map((x) => `${x.level}: ${x.text}`).join("\n");
  assert.match(text, /medium: no Strict-Transport-Security/);
  assert.match(text, /high: mixed content: script http:/);
  assert.match(text, /high: CORS allows any origin with credentials/);
  assert.match(text, /medium: cookie auth is not Secure/);
  assert.match(text, /medium: cookie auth is SameSite=None without Secure/);
  assert.doesNotMatch(text, /plain HTTP/);
});

test("check step values", () => {
  assert.deepEqual(parseBudget(""), { lcp: 2500, cls: 0.1, ttfb: 800 });
  assert.deepEqual(parseBudget("lcp<=2000, cls=0.05;load<4000"), { lcp: 2000, cls: 0.05, load: 4000 });
  assert.throws(() => parseBudget("speed=1"), /unknown metric/);
  assert.throws(() => parseBudget("lcp=fast"), /not metric=number/);
  assert.equal(invalidCheckValue("check_accessibility", "moderate"), "");
  assert.match(invalidCheckValue("check_accessibility", "AA"), /one of minor/);
  assert.equal(invalidCheckValue("check_security", "strict"), "");
  assert.equal(invalidStep(step({ do: "check_accessibility" })), "");
  assert.match(invalidStep(step({ do: "check_performance", by: "css", name: "main" })), /takes no element/);
  assert.match(invalidStep(step({ do: "check_performance", value: "ttfb=0" })), /out of range/);
});
