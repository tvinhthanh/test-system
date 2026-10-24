// Test steps and sessions (QĐ-16) on real Chromium against a fixture chat
// site that needs signing in. Like check.test.mjs, the guard lets exactly
// 127.0.0.1 through.
import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { chromium } from "playwright";
import { runCheck } from "../src/check.mjs";
import { startProxy } from "../src/proxy.mjs";
import { Session } from "../src/session.mjs";
import { invalidStep, siteOf, substitute } from "../src/steps.mjs";

const html = (title, body) =>
  `<!doctype html><html lang="en"><head><title>${title}</title></head><body>${body}</body></html>`;
const pages = {
  "/login": html("Sign in", `<main><h1>Sign in</h1>
    <form method="post" action="/login">
      <label>Email <input name="email" type="email"></label>
      <label>Password <input name="password" type="password"></label>
      <button type="submit">Sign in</button>
    </form>
    <div role="alert" id="err"></div>
    <script>if (location.search.includes("bad")) document.getElementById("err").textContent = "Wrong email or password";</script></main>`),
  "/message": html("Chat", `<main><h1>Chat</h1>
    <p>To <strong id="to"></strong></p>
    <ul role="log" aria-label="Messages"></ul>
    <label>Message <textarea id="m"></textarea></label>
    <button id="send">Send</button>
    <a href="/inbox">Inbox</a> <a href="/logout">Sign out</a> <a href="http://127.0.0.2:1/elsewhere">Elsewhere</a>
    <script>
      document.getElementById("to").textContent = new URLSearchParams(location.search).get("to");
      document.getElementById("send").onclick = () => {
        const v = document.getElementById("m").value.trim();
        if (!v) return;
        const li = document.createElement("li"); li.textContent = v;
        document.querySelector("[role=log]").append(li);
        document.getElementById("m").value = "";
      };
    </script></main>`),
  "/inbox": html("Inbox", `<main><h1>Inbox</h1><a href="/message?to=bob">Bob</a></main>`),
};
let site;
let base;
let browser;
let sink;
const policy = { allow: ["127.0.0.1"] };
const step = (s) => ({ do: "", by: "", role: "", name: "", value: "", say: "", expected: "", ...s });
const login = {
  url: "/login",
  steps: [
    step({ do: "fill", by: "label", name: "Email", value: "{{username}}" }),
    step({ do: "fill", by: "label", name: "Password", value: "{{password}}" }),
    step({ do: "click", by: "role", role: "button", name: "Sign in" }),
  ],
};

before(async () => {
  site = http.createServer(async (req, res) => {
    const url = new URL(req.url, "http://x");
    const signedIn = (req.headers.cookie ?? "").includes("sid=ok");
    if (req.method === "POST" && url.pathname === "/login") {
      let body = "";
      for await (const c of req) body += c;
      const f = new URLSearchParams(body);
      if (f.get("email") === "tester@example.com" && f.get("password") === "s3cret!")
        return res.writeHead(303, { Location: "/inbox", "Set-Cookie": "sid=ok; Path=/" }).end();
      return res.writeHead(303, { Location: "/login?bad=1" }).end();
    }
    if (url.pathname === "/logout") return res.writeHead(303, { Location: "/login", "Set-Cookie": "sid=; Path=/; Max-Age=0" }).end();
    if ((url.pathname === "/message" || url.pathname === "/inbox") && !signedIn)
      return res.writeHead(303, { Location: "/login" }).end();
    const body = pages[url.pathname];
    if (!body) return res.writeHead(404).end("not here");
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }).end(body);
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

const vars = { username: "tester@example.com", password: "s3cret!", to: "bob", text: "Xin chào 👋" };

test("signs in, then a chat case runs step by step", async () => {
  const s = await Session.open(browser, { base, policy });
  try {
    assert.deepEqual(await s.login({ ...login, vars }), { loggedIn: true });
    const { ok, results } = await s.run(
      [
        step({ do: "goto", value: "/message?to={{to}}" }),
        step({ do: "expect_text", by: "text", name: "bob", value: "bob" }),
        step({ do: "fill", by: "label", name: "Message", value: "{{text}}" }),
        step({ do: "click", by: "role", role: "button", name: "Send" }),
        step({ do: "expect_text", by: "role", role: "log", name: "Messages", value: "{{text}}" }),
        step({ do: "expect_value", by: "label", name: "Message", value: "" }),
        step({ do: "expect_title", value: "Chat" }),
        step({ do: "expect_url", value: "to=bob" }),
      ],
      vars,
    );
    assert.ok(ok, JSON.stringify(results));
    assert.equal(results.length, 8);
    const view = await s.look({ shot: true });
    assert.match(view.tree, /button "Send"/);
    assert.match(view.tree, /Xin chào/);
    assert.ok(view.screenshot.length > 1000);
  } finally {
    await s.close();
  }
});

test("a wrong password is reported with what the page says", async () => {
  const s = await Session.open(browser, { base, policy });
  try {
    const r = await s.login({ ...login, vars: { ...vars, password: "nope" } });
    assert.equal(r.loggedIn, false);
    assert.match(r.error, /still on the sign-in page/);
    assert.match(r.error, /Wrong email or password/);
  } finally {
    await s.close();
  }
});

test("a missing element is a locator failure, a wrong expectation an assert failure", async () => {
  const s = await Session.open(browser, { base, policy });
  try {
    await s.login({ ...login, vars });
    await s.run([step({ do: "goto", value: "/message?to=bob" })], vars);
    const missing = await s.run([step({ do: "click", by: "role", role: "button", name: "Gửi" })], vars);
    assert.equal(missing.ok, false);
    assert.equal(missing.results[0].kind, "locator");
    const wrong = await s.run([step({ do: "expect_text", value: "never on this page" })], vars);
    assert.equal(wrong.results[0].kind, "assert");
    assert.match(wrong.results[0].error, /never on this page/);
    const unknown = await s.run([step({ do: "fill", by: "label", name: "Message", value: "{{nope}}" })], vars);
    assert.equal(unknown.results[0].kind, "input");
    assert.match(unknown.results[0].error, /unknown variable \{\{nope\}\}/);
  } finally {
    await s.close();
  }
});

test("the browser stays on the site under test", async () => {
  const s = await Session.open(browser, { base, policy });
  try {
    await s.login({ ...login, vars });
    const away = await s.run([step({ do: "goto", value: "http://127.0.0.2:1/x" })], vars);
    assert.equal(away.results[0].kind, "navigation");
    await s.run([step({ do: "goto", value: "/message?to=bob" })], vars);
    const link = await s.run([step({ do: "click", by: "role", role: "link", name: "Elsewhere" })], vars);
    // The navigation the click starts is stopped, named, and the browser is
    // back on the site for the next step.
    assert.equal(link.ok, false);
    assert.equal(link.results[0].kind, "navigation");
    assert.match(link.results[0].error, /127\.0\.0\.2/);
    assert.equal(s.page.url(), `${base}/message?to=bob`);
    assert.ok((await s.run([step({ do: "expect_title", value: "Chat" })], vars)).ok);
  } finally {
    await s.close();
  }
});

test("a URL check can sign in first, and lists the page's same-site links", async () => {
  const { report, links } = await runCheck(browser, `${base}/message?to=bob`, policy, { login: { ...login, vars } });
  assert.equal(report.title, "Chat");
  assert.ok(links.includes(`${base}/inbox`), JSON.stringify(links));
  assert.ok(!links.some((l) => l.includes("127.0.0.2")));
  const linkCheck = report.checks.find((c) => c.key === "links");
  assert.ok(!JSON.stringify(linkCheck).includes("/logout"), "a signed-in link check never opens sign-out links");
  const failed = await runCheck(browser, `${base}/message?to=bob`, policy, { login: { ...login, vars: { ...vars, password: "x" } } });
  assert.equal(failed.report.verdict, "failed");
  assert.match(failed.report.checks[0].summary, /Could not sign in/);
});

test("step shapes and variables", () => {
  assert.equal(invalidStep(step({ do: "click", by: "role", role: "button", name: "Send" })), "");
  assert.match(invalidStep(step({ do: "click" })), /needs an element/);
  assert.match(invalidStep(step({ do: "goto", by: "css", name: "a" })), /takes no element/);
  assert.match(invalidStep(step({ do: "click", by: "role", role: "nope" })), /unknown role/);
  assert.match(invalidStep(step({ do: "wait", value: "60000" })), /wait is/);
  assert.match(invalidStep(step({ do: "eval" })), /unknown action/);
  assert.equal(invalidStep(step({ do: "fill", by: "label", name: "Message", value: "" })), "");
  assert.equal(substitute("Hi {{ name }}!", { name: "An" }), "Hi An!");
  assert.throws(() => substitute("{{x}}", {}), /unknown variable/);
  assert.equal(siteOf("app.example.com"), "example.com");
  assert.equal(siteOf("id.shop.example.co.uk"), "example.co.uk");
  assert.equal(siteOf("chat.example.com.vn"), "example.com.vn");
  assert.equal(siteOf("127.0.0.1"), "127.0.0.1");
});
