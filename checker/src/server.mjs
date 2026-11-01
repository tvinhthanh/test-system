// The checker service (QĐ-14, QĐ-15, QĐ-16). Only the ATP worker talks to
// it, over the compose network; it is not published on the host.
//
//   POST /check {url, login?}       the URL check report, two JPEG screenshots
//                                   (base64) and the page's same-site links
//   POST /outline {url}             the page's structure, for drafting cases
//   POST /sessions {base, login?}   opens a browser session for test steps
//   POST /sessions/:id/steps        {steps, vars, look?, shot?} runs them
//   POST /sessions/:id/look         {shot?} the page as a model reads it
//   DELETE /sessions/:id
//
// login = {url, steps, vars}: the sign-in page, the steps that sign in and
// the variables they use ({{username}}, {{password}}). The worker sends the
// password per call; nothing is kept once a session closes.
//
// A check or a session that does not fit (CHECKER_CONCURRENCY checks,
// CHECKER_SESSIONS sessions) is a 503 the worker retries. A session idle for
// two minutes, or open for fifteen, is closed.
//
//   PORT=8090  URLCHECK_ALLOW_PRIVATE=off|on  CHECKER_CONCURRENCY=2  CHECKER_SESSIONS=4
import http from "node:http";
import { chromium } from "playwright";
import { readOutline, runCheck } from "./check.mjs";
import { startProxy } from "./proxy.mjs";
import { Session } from "./session.mjs";
import { invalidStep, MAX_STEPS } from "./steps.mjs";

const port = Number(process.env.PORT || 8090);
const concurrency = Math.max(1, Number(process.env.CHECKER_CONCURRENCY || 2));
const maxSessions = Math.max(1, Number(process.env.CHECKER_SESSIONS || 4));
const allowPrivate = (process.env.URLCHECK_ALLOW_PRIVATE || "off") === "on";
const policy = { allowPrivate };
const MAX_BODY = 256 * 1024;
const IDLE_MS = 120_000;
const LIFETIME_MS = 15 * 60_000;

const log = (msg, fields = {}) =>
  console.log(JSON.stringify({ time: new Date().toISOString(), msg, ...fields }));

// The launch-level proxy refuses everything: a check's context brings its
// own guarded proxy, and anything without one reaches nothing.
const sink = await startProxy({ denyAll: true });
let browser = null;
async function getBrowser() {
  if (browser?.isConnected()) return browser;
  browser = await chromium.launch({
    proxy: { server: sink.url, bypass: "<-loopback>" },
    // WebRTC would open UDP around the proxy.
    args: ["--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--disable-quic"],
  });
  browser.on("disconnected", () => log("browser disconnected"));
  return browser;
}

let running = 0;
/** @type {Map<string, Session>} */
const sessions = new Map();

const send = (res, status, body) => {
  const data = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) });
  res.end(data);
};
const firstLine = (err) => String(err?.message ?? err).split("\n")[0].slice(0, 300);

function httpURL(raw) {
  const url = new URL(raw);
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("scheme");
  if (url.username || url.password) throw new Error("credentials");
  return url;
}

// login as the worker sends it; "" when it is well formed.
function invalidLogin(login) {
  if (login == null) return "";
  if (typeof login !== "object" || !Array.isArray(login.steps) || login.steps.length > MAX_STEPS) return "login: {url, steps, vars}";
  if (login.url) {
    try {
      httpURL(new URL(login.url, "http://x").href);
    } catch {
      return "login.url must be an http(s) address";
    }
  }
  if (login.vars != null && typeof login.vars !== "object") return "login.vars must be an object";
  for (const s of login.steps) {
    const bad = invalidStep(s);
    if (bad) return `login step: ${bad}`;
  }
  return "";
}

async function readJSON(req) {
  let raw = "";
  for await (const chunk of req) {
    raw += chunk;
    if (raw.length > MAX_BODY) throw Object.assign(new Error("body too large"), { status: 413 });
  }
  try {
    return raw ? JSON.parse(raw) : {};
  } catch {
    throw Object.assign(new Error("body must be JSON"), { status: 400 });
  }
}

async function handleCheck(req, res, path) {
  const body = await readJSON(req);
  let url;
  try {
    url = httpURL(body.url);
  } catch {
    return send(res, 400, { error: "url must be an http(s) address without credentials" });
  }
  const badLogin = path === "/check" ? invalidLogin(body.login) : "";
  if (badLogin) return send(res, 400, { error: badLogin });
  if (running >= concurrency) return send(res, 503, { error: "busy" });
  running++;
  const started = Date.now();
  try {
    if (path === "/outline") {
      const outline = await readOutline(await getBrowser(), url.href, policy);
      log("outlined", { host: url.host, ms: Date.now() - started });
      return send(res, 200, { outline });
    }
    const { report, shots, links } = await runCheck(await getBrowser(), url.href, policy, { login: body.login ?? undefined });
    log("checked", { host: url.host, verdict: report.verdict, ms: Date.now() - started });
    send(res, 200, {
      report,
      links: links ?? [],
      screenshots: {
        desktop: shots.desktop ? shots.desktop.toString("base64") : "",
        mobile: shots.mobile ? shots.mobile.toString("base64") : "",
      },
    });
  } catch (err) {
    log(`${path.slice(1)} failed`, { host: url.host, err: firstLine(err) });
    if (path === "/outline") send(res, 422, { error: firstLine(err) });
    else send(res, 500, { error: "the check could not run" });
  } finally {
    running--;
  }
}

async function handleSessions(req, res, parts) {
  // POST /sessions
  if (parts.length === 1) {
    if (req.method !== "POST") return send(res, 405, { error: "method" });
    const body = await readJSON(req);
    let base;
    try {
      base = httpURL(body.base);
    } catch {
      return send(res, 400, { error: "base must be an http(s) address without credentials" });
    }
    const badLogin = invalidLogin(body.login);
    if (badLogin) return send(res, 400, { error: badLogin });
    if (sessions.size >= maxSessions) return send(res, 503, { error: "busy" });
    const s = await Session.open(await getBrowser(), { base: base.href, policy });
    sessions.set(s.id, s);
    log("session opened", { id: s.id, host: base.host, open: sessions.size });
    if (body.login) {
      try {
        const r = await s.login({ url: body.login.url, steps: body.login.steps, vars: body.login.vars ?? {} });
        if (!r.loggedIn) return send(res, 200, { id: s.id, loggedIn: false, error: r.error, console: s.drain() });
      } catch (err) {
        return send(res, 200, { id: s.id, loggedIn: false, error: firstLine(err), console: s.drain() });
      }
      return send(res, 200, { id: s.id, loggedIn: true, console: s.drain() });
    }
    return send(res, 200, { id: s.id });
  }
  const s = sessions.get(parts[1]);
  if (!s) return send(res, 404, { error: "no such session (closed after two idle minutes)" });
  if (parts.length === 2 && req.method === "DELETE") {
    sessions.delete(s.id);
    await s.close();
    log("session closed", { id: s.id, open: sessions.size });
    return send(res, 200, { closed: true });
  }
  if (req.method !== "POST" || parts.length !== 3) return send(res, 404, { error: "not found" });
  const body = await readJSON(req);
  if (parts[2] === "look") return send(res, 200, { ...(await s.look({ shot: !!body.shot })), console: s.drain() });
  if (parts[2] !== "steps") return send(res, 404, { error: "not found" });
  if (!Array.isArray(body.steps) || body.steps.length === 0 || body.steps.length > MAX_STEPS)
    return send(res, 400, { error: `steps: 1 to ${MAX_STEPS}` });
  if (body.vars != null && typeof body.vars !== "object") return send(res, 400, { error: "vars must be an object" });
  for (const [i, step] of body.steps.entries()) {
    const bad = invalidStep(step);
    if (bad) return send(res, 400, { error: `step ${i + 1}: ${bad}` });
  }
  const { results, ok } = await s.run(body.steps, body.vars ?? {});
  // shot: "failure" (default) | "always" | "none". On a failure the page is
  // read too, so the worker can ask a model to repair the step.
  const shot = body.shot ?? "failure";
  const out = { results, ok, url: s.page.url(), console: s.drain() };
  if (!ok || body.look) Object.assign(out, await s.look({ shot: shot === "always" || (!ok && shot === "failure") }));
  else if (shot === "always") out.screenshot = await s.screenshot();
  return send(res, 200, out);
}

const server = http.createServer(async (req, res) => {
  try {
    const path = (req.url ?? "").split("?")[0];
    if (req.method === "GET" && path === "/healthz") return send(res, 200, { status: "ok", running, sessions: sessions.size });
    if (req.method === "POST" && (path === "/check" || path === "/outline")) return await handleCheck(req, res, path);
    const parts = path.split("/").filter(Boolean);
    if (parts[0] === "sessions" && parts.length <= 3) return await handleSessions(req, res, parts);
    send(res, 404, { error: "not found" });
  } catch (err) {
    if (!res.headersSent) send(res, err.status ?? 500, { error: err.status ? err.message : "the checker failed" });
    if (!err.status) log("request failed", { err: firstLine(err) });
  }
});
server.requestTimeout = 180_000;
server.listen(port, "0.0.0.0", () => log("checker listening", { port, concurrency, maxSessions, allowPrivate }));

const reaper = setInterval(async () => {
  const now = Date.now();
  for (const [id, s] of sessions)
    if (now - s.used > IDLE_MS || now - s.created > LIFETIME_MS) {
      sessions.delete(id);
      await s.close();
      log("session expired", { id, open: sessions.size });
    }
}, 15_000);
reaper.unref();

for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, async () => {
    server.close();
    for (const s of sessions.values()) await s.close();
    await browser?.close().catch(() => {});
    await sink.close();
    process.exit(0);
  });
