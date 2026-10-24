// The URL checker service (QĐ-14). Only the ATP worker talks to it, over the
// compose network: POST /check {url} → the report and two JPEG screenshots
// (base64). It is not published on the host and keeps no state; a check
// that does not fit (CHECKER_CONCURRENCY busy) is a 503 the worker retries.
//
//   PORT=8090  URLCHECK_ALLOW_PRIVATE=off|on  CHECKER_CONCURRENCY=2
import http from "node:http";
import { chromium } from "playwright";
import { readOutline, runCheck } from "./check.mjs";
import { startProxy } from "./proxy.mjs";

const port = Number(process.env.PORT || 8090);
const concurrency = Math.max(1, Number(process.env.CHECKER_CONCURRENCY || 2));
const allowPrivate = (process.env.URLCHECK_ALLOW_PRIVATE || "off") === "on";
const MAX_BODY = 8 * 1024;

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
const send = (res, status, body) => {
  const data = JSON.stringify(body);
  res.writeHead(status, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) });
  res.end(data);
};

const server = http.createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/healthz") return send(res, 200, { status: "ok", running });
  // /check: the report (QĐ-14). /outline: the page's structure for drafting
  // test cases (QĐ-15). Same guard, same concurrency limit.
  if (req.method !== "POST" || (req.url !== "/check" && req.url !== "/outline"))
    return send(res, 404, { error: "not found" });
  let raw = "";
  for await (const chunk of req) {
    raw += chunk;
    if (raw.length > MAX_BODY) return send(res, 413, { error: "body too large" });
  }
  let url;
  try {
    url = new URL(JSON.parse(raw).url);
    if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("scheme");
    if (url.username || url.password) throw new Error("credentials");
  } catch {
    return send(res, 400, { error: "url must be an http(s) address without credentials" });
  }
  if (running >= concurrency) return send(res, 503, { error: "busy" });
  running++;
  const started = Date.now();
  if (req.url === "/outline") {
    try {
      const outline = await readOutline(await getBrowser(), url.href, { allowPrivate });
      log("outlined", { host: url.host, ms: Date.now() - started });
      send(res, 200, { outline });
    } catch (err) {
      log("outline failed", { host: url.host, err: String(err?.message ?? err).split("\n")[0] });
      send(res, 422, { error: String(err?.message ?? err).split("\n")[0].slice(0, 300) });
    } finally {
      running--;
    }
    return;
  }
  try {
    const { report, shots } = await runCheck(await getBrowser(), url.href, { allowPrivate });
    log("checked", { host: url.host, verdict: report.verdict, ms: Date.now() - started });
    send(res, 200, {
      report,
      screenshots: {
        desktop: shots.desktop ? shots.desktop.toString("base64") : "",
        mobile: shots.mobile ? shots.mobile.toString("base64") : "",
      },
    });
  } catch (err) {
    log("check failed", { host: url.host, err: String(err?.message ?? err).split("\n")[0] });
    send(res, 500, { error: "the check could not run" });
  } finally {
    running--;
  }
});
server.requestTimeout = 180_000;
server.listen(port, "0.0.0.0", () => log("url checker listening", { port, concurrency, allowPrivate }));

for (const signal of ["SIGINT", "SIGTERM"])
  process.on(signal, async () => {
    server.close();
    await browser?.close().catch(() => {});
    await sink.close();
    process.exit(0);
  });
