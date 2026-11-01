import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, type Server } from "node:http";
import { mkdtempSync, readFileSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createHash } from "node:crypto";
import AtpReporter, { Client, EventQueue, stripHar } from "../src/index";

// The machine's own path style: path.relative in the reporter is native, so
// a Windows literal would only strip on Windows.
const REPO = resolve("/repo");

// A fake platform: enough of §7.2 to see what the reporter sends. Every
// request is recorded; `down` makes it refuse connections to model an
// outage.
class FakeServer {
  readonly requests: { method: string; path: string; body: any; headers: Record<string, string> }[] = [];
  readonly objects = new Map<string, Buffer>();
  down = false;
  /** Where declared artifacts are told to upload; default this server. */
  uploadBase = "";
  private srv!: Server;
  url = "";

  async start(): Promise<void> {
    this.srv = createServer((req, res) => {
      const chunks: Buffer[] = [];
      req.on("data", (c) => chunks.push(c));
      req.on("end", () => {
        if (this.down) {
          req.socket.destroy();
          return;
        }
        const raw = Buffer.concat(chunks);
        const path = req.url ?? "";
        const json = (status: number, body: unknown) => {
          res.writeHead(status, { "Content-Type": "application/json" });
          res.end(JSON.stringify(body));
        };
        if (req.method === "PUT" && path.startsWith("/upload/")) {
          this.objects.set(path.slice("/upload/".length), raw);
          res.writeHead(200, { ETag: '"' + createHash("md5").update(raw).digest("hex") + '"' });
          res.end();
          return;
        }
        let body: any = {};
        try {
          body = raw.length ? JSON.parse(raw.toString("utf8")) : {};
        } catch {}
        this.requests.push({ method: req.method ?? "", path, body, headers: req.headers as Record<string, string> });
        if (path === "/v1/runs") return json(201, { runId: "run-1", limits: { maxEventsPerBatch: 500, maxBatchBytes: 5 << 20, artifactBytes: { screenshot: 5 << 20, log: 100 } }, identityMap: {} });
        if (path.endsWith("/events")) {
          const ids: Record<string, string> = {};
          for (const e of body.events ?? []) ids[e.clientTestId] = "st_" + e.clientTestId.slice(3, 9);
          return json(200, { accepted: body.events?.length ?? 0, duplicates: 0, identityMap: ids });
        }
        if (path.endsWith("/finish")) return json(200, { runId: "run-1", status: "failed", flakyCount: 1, finishedAt: body.finishedAt });
        if (path === "/v1/artifacts") {
          if (body.sizeBytes > (body.kind === "log" ? 100 : 5 << 20)) return json(413, { error: { code: "ARTIFACT_TOO_LARGE", message: "cap", details: {} } });
          const id = "art-" + this.requests.length;
          return json(201, { artifactId: id, status: "pending", expiresAt: "", linked: false, upload: { method: "PUT", url: (this.uploadBase || this.url) + "/upload/" + id, headers: { "Content-Type": body.contentType }, expiresAt: "" } });
        }
        if (path.endsWith("/complete")) return json(200, { artifactId: path.split("/")[3], status: "verified", sizeBytes: 1 });
        json(404, { error: { code: "NOT_FOUND", message: path } });
      });
    });
    await new Promise<void>((r) => this.srv.listen(0, "127.0.0.1", r));
    const addr = this.srv.address() as { port: number };
    this.url = `http://127.0.0.1:${addr.port}`;
  }
  stop(): Promise<void> {
    return new Promise((r) => this.srv.close(() => r()));
  }
}

// Minimal stand-ins for Playwright's TestCase/TestResult — only what the
// reporter reads.
function fakeTest(file: string, titles: string[], project = "chromium"): any {
  return {
    title: titles[titles.length - 1],
    location: { file, line: 1, column: 1 },
    titlePath: () => ["", project, "tests/x.spec.ts", ...titles],
    parent: { project: () => ({ name: project }) },
    outcome: () => "expected",
  };
}

function fakeResult(status: string, retry = 0, attachments: any[] = [], error?: any): any {
  return { status, retry, duration: 120, startTime: new Date("2026-09-16T03:00:00Z"), workerIndex: 0, attachments, error };
}

function fakeConfig(): any {
  return { rootDir: REPO, version: "1.63.0" };
}

test("a run streams begin → events → artifacts → finish, and never throws", async () => {
  const srv = new FakeServer();
  await srv.start();
  const spool = mkdtempSync(join(tmpdir(), "atp-spool-"));
  const logs: string[] = [];
  const origWarn = console.warn;
  const origLog = console.log;
  console.warn = (m: string) => logs.push(m);
  console.log = (m: string) => logs.push(m);
  try {
    const client = new Client({ url: srv.url, token: "atp_ing_t", sleep: async () => {} });
    const r = new AtpReporter({ url: srv.url, token: "atp_ing_t", environment: "staging", spoolDir: spool, _client: client });
    r.onBegin(fakeConfig(), {} as any);
    const t1 = fakeTest(join(REPO, "tests", "checkout", "payment.spec.ts"), ["Checkout", "declines expired card"]);
    const t2 = fakeTest(join(REPO, "tests", "checkout", "payment.spec.ts"), ["Checkout", "accepts valid card"]);
    t2.outcome = () => "flaky";
    const err = { message: "expected 402", stack: "at payment.spec.ts:12", snippet: "expect(x)" };
    r.onTestEnd(t1, fakeResult("passed"));
    r.onTestEnd(t2, fakeResult("failed", 0, [{ name: "screenshot", contentType: "image/png", body: Buffer.from("PNGDATA") }], err));
    r.onTestEnd(t2, fakeResult("passed", 1, [{ name: "trace", contentType: "application/zip", body: Buffer.from("PK\x03\x04") }]));
    r.onTestEnd(t2, fakeResult("timedOut", 2, [{ name: "run.log", contentType: "text/plain", body: Buffer.alloc(200) }]));
    await r.onEnd({ status: "failed" } as any);

    const paths = srv.requests.map((q) => q.method + " " + q.path);
    assert.equal(paths[0], "POST /v1/runs");
    const run = srv.requests[0].body;
    assert.equal(run.environmentKey, "staging");
    assert.equal(run.framework.name, "playwright");
    assert.equal(run.reporter.protocolVersion, "1.1");
    const pkg = JSON.parse(readFileSync(join(__dirname, "..", "..", "package.json"), "utf8"));
    assert.equal(run.reporter.version, pkg.version, "the run names the version package.json ships");
    assert.match(run.localRunId, /^[0-9a-f-]{36}$/);
    assert.equal(srv.requests[0].headers["x-protocol-version"], "1.1");

    const events = srv.requests.filter((q) => q.path.endsWith("/events")).flatMap((q) => q.body.events);
    assert.equal(events.length, 4);
    assert.deepEqual(events.map((e: any) => e.sequence), [1, 2, 3, 4]);
    assert.deepEqual(events.map((e: any) => e.attempt), [0, 0, 1, 2]);
    assert.deepEqual(events.map((e: any) => e.status), ["passed", "failed", "passed", "timed_out"]);
    assert.deepEqual(events.map((e: any) => e.outcome), ["expected", "flaky", "flaky", "flaky"]);
    assert.equal(events[0].identity.filePath, "tests/checkout/payment.spec.ts"); // repo root stripped, slashes normalised
    assert.deepEqual(events[0].identity.titlePath, ["Checkout", "declines expired card"]);
    assert.equal(events[0].identity.paramSignature, "project=chromium");
    assert.match(events[0].clientTestId, /^ct_[0-9a-f]{32}$/);
    assert.equal(events[1].clientTestId, events[2].clientTestId, "retries share the clientTestId");
    assert.equal(events[1].occurrenceIndex, 0);
    assert.deepEqual(events[1].error, err);
    assert.equal(events[1].startedAt, "2026-09-16T03:00:00.000Z");
    assert.equal(events[1].finishedAt, "2026-09-16T03:00:00.120Z");

    const declares = srv.requests.filter((q) => q.path === "/v1/artifacts").map((q) => q.body);
    // The 200-byte log is over the cap the server announced (100): refused
    // client-side, never declared (§10.4), logged.
    assert.deepEqual(declares.map((d) => [d.kind, d.attempt, d.contentType]).sort(), [["screenshot", 0, "image/png"], ["trace", 1, "application/zip"]]);
    assert.equal(declares[0].clientTestId, events[1].clientTestId);
    assert.equal(declares[0].sha256.length, 64);
    assert.equal(srv.objects.size, 2);
    assert.equal(srv.requests.filter((q) => q.path.endsWith("/complete")).length, 2);
    assert.equal(paths[paths.length - 1].startsWith("POST /v1/runs/") && paths[paths.length - 1].endsWith("/finish"), true);
    assert.ok(logs.some((l) => l.includes("exceeds the log cap")), "cap refusal is logged, not thrown: " + logs.join(" | "));
    assert.ok(logs.some((l) => l.includes("run run-1: failed")), logs.join("\n"));
    assert.equal(readdirSync(spool).length, 0, "nothing spooled on a healthy server");
  } finally {
    console.warn = origWarn;
    console.log = origLog;
    await srv.stop();
  }
});

test("interleaved retries keep the occurrence of their original TestCase", async () => {
  const srv = new FakeServer();
  await srv.start();
  const spool = mkdtempSync(join(tmpdir(), "atp-spool-"));
  try {
    const client = new Client({ url: srv.url, token: "atp_ing_t", sleep: async () => {} });
    const r = new AtpReporter({ url: srv.url, token: "atp_ing_t", spoolDir: spool, _client: client });
    r.onBegin(fakeConfig(), {} as any);

    const a = fakeTest(join(REPO, "tests", "same.spec.ts"), ["same title"]);
    const b = fakeTest(join(REPO, "tests", "same.spec.ts"), ["same title"]);
    r.onTestEnd(a, fakeResult("failed", 0));
    r.onTestEnd(b, fakeResult("failed", 0));
    r.onTestEnd(b, fakeResult("passed", 1));
    r.onTestEnd(a, fakeResult("passed", 1));
    await r.onEnd({ status: "passed" } as any);

    const events = srv.requests.filter((q) => q.path.endsWith("/events")).flatMap((q) => q.body.events);
    assert.deepEqual(events.map((e: any) => e.occurrenceIndex), [0, 1, 1, 0]);
    assert.equal(new Set(events.map((e: any) => e.clientTestId)).size, 1, "same fingerprint is still preserved");
  } finally {
    await srv.stop();
  }
});

test("total outage: events are spooled, the run is not failed", async () => {
  const srv = new FakeServer();
  await srv.start();
  srv.down = true;
  const spool = mkdtempSync(join(tmpdir(), "atp-spool-"));
  const warnings: string[] = [];
  const origWarn = console.warn;
  console.warn = (m: string) => warnings.push(m);
  try {
    const client = new Client({ url: srv.url, token: "atp_ing_t", sleep: async () => {}, timeoutMs: 200 });
    const r = new AtpReporter({ url: srv.url, token: "atp_ing_t", spoolDir: spool, _client: client });
    r.onBegin(fakeConfig(), {} as any);
    r.onTestEnd(fakeTest(join(REPO, "a.spec.ts"), ["a"]), fakeResult("passed"));
    r.onTestEnd(fakeTest(join(REPO, "b.spec.ts"), ["b"]), fakeResult("failed"));
    await r.onEnd({ status: "failed" } as any); // must resolve, not reject
    const files = readdirSync(spool);
    assert.equal(files.length, 1);
    const lines = readFileSync(join(spool, files[0]), "utf8").trim().split("\n").map((l) => JSON.parse(l));
    assert.deepEqual(lines.map((e) => e.sequence), [1, 2]);
    assert.ok(warnings.some((w) => w.includes("spooled")), warnings.join("\n"));
  } finally {
    console.warn = origWarn;
    await srv.stop();
  }
});

test("a platform that never answers: onEnd gives up after flushTimeoutMs and spools everything", async () => {
  const spool = mkdtempSync(join(tmpdir(), "atp-spool-"));
  const warnings: string[] = [];
  const origWarn = console.warn;
  console.warn = (m: string) => warnings.push(m);
  try {
    // Every request hangs until aborted. 1 s timeout × 5 tries still outlasts
    // the 300 ms budget, and the process is not held for minutes afterwards.
    const hang = ((_: unknown, init?: RequestInit) => new Promise((_r, reject) => init?.signal?.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" }))))) as unknown as typeof fetch;
    const client = new Client({ url: "http://api.test", token: "atp_ing_t", fetch: hang, timeoutMs: 1000, sleep: async () => {} });
    const r = new AtpReporter({ url: "http://api.test", token: "atp_ing_t", spoolDir: spool, _client: client, flushTimeoutMs: 300 });
    r.onBegin(fakeConfig(), {} as any);
    r.onTestEnd(fakeTest(join(REPO, "a.spec.ts"), ["a"]), fakeResult("passed"));
    r.onTestEnd(fakeTest(join(REPO, "b.spec.ts"), ["b"]), fakeResult("failed", 0, [{ name: "screenshot", contentType: "image/png", body: Buffer.from("png") }]));
    const started = Date.now();
    await r.onEnd({ status: "failed" } as any);
    const took = Date.now() - started;
    assert.ok(took < 2000, `onEnd took ${took} ms`);
    const files = readdirSync(spool);
    assert.equal(files.length, 1);
    const lines = readFileSync(join(spool, files[0]), "utf8").trim().split("\n").map((l) => JSON.parse(l));
    assert.deepEqual(lines.map((e) => e.sequence), [1, 2]);
    assert.ok(warnings.some((w) => w.includes("gave up on the platform")), warnings.join("\n"));
  } finally {
    console.warn = origWarn;
  }
});

// §10.4: an artifact never fails the run. Before 30-10-2026 onEnd waited for
// every upload before finish, so a store that refused or hung left the run
// open until the server's 6 h auto-close marked it `error`.
test("an upload URL that does not parse fails once and the run still finishes", async () => {
  const srv = new FakeServer();
  await srv.start();
  srv.uploadBase = "https://<account>.r2.cloudflarestorage.com"; // CI's .env placeholder
  const logs: string[] = [];
  const origWarn = console.warn;
  const origLog = console.log;
  console.warn = (m: string) => logs.push(m);
  console.log = (m: string) => logs.push(m);
  try {
    const client = new Client({ url: srv.url, token: "atp_ing_t", sleep: async () => {} });
    const r = new AtpReporter({ url: srv.url, token: "atp_ing_t", spoolDir: mkdtempSync(join(tmpdir(), "atp-spool-")), _client: client });
    r.onBegin(fakeConfig(), {} as any);
    r.onTestEnd(fakeTest(join(REPO, "a.spec.ts"), ["a"]), fakeResult("failed", 0, [{ name: "screenshot", contentType: "image/png", body: Buffer.from("png") }]));
    const started = Date.now();
    await r.onEnd({ status: "failed" } as any);
    assert.ok(Date.now() - started < 2000, "no retries of a URL that cannot parse");
    assert.ok(srv.requests.some((q) => q.path.endsWith("/finish")), "finish sent");
    assert.ok(logs.some((l) => l.includes("upload failed: upload URL does not parse")), logs.join("\n"));
    assert.ok(logs.some((l) => l.includes("run run-1: failed") && l.includes("artifacts 0 ok / 0 over cap / 1 failed")), logs.join("\n"));
    assert.ok(!logs.some((l) => l.includes("gave up on the platform")), logs.join("\n"));
  } finally {
    console.warn = origWarn;
    console.log = origLog;
    await srv.stop();
  }
});

test("an object store that never answers: uploads stop at the deadline and the run still finishes", async () => {
  const srv = new FakeServer();
  await srv.start();
  const logs: string[] = [];
  const origWarn = console.warn;
  const origLog = console.log;
  console.warn = (m: string) => logs.push(m);
  console.log = (m: string) => logs.push(m);
  try {
    // The platform answers; every PUT to the store hangs until aborted.
    const hangUploads = ((url: string | URL | Request, init?: RequestInit) =>
      String(url).includes("/upload/")
        ? new Promise((_r, reject) => init?.signal?.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" }))))
        : fetch(url, init)) as unknown as typeof fetch;
    // One try that hangs 1 s: longer than the uploads' share of the 600 ms budget (400 ms).
    const client = new Client({ url: srv.url, token: "atp_ing_t", fetch: hangUploads, timeoutMs: 1000, maxAttempts: 1, sleep: async () => {} });
    const r = new AtpReporter({ url: srv.url, token: "atp_ing_t", spoolDir: mkdtempSync(join(tmpdir(), "atp-spool-")), _client: client, flushTimeoutMs: 600 });
    r.onBegin(fakeConfig(), {} as any);
    r.onTestEnd(fakeTest(join(REPO, "a.spec.ts"), ["a"]), fakeResult("failed", 0, [{ name: "screenshot", contentType: "image/png", body: Buffer.from("png") }]));
    const started = Date.now();
    await r.onEnd({ status: "failed" } as any);
    assert.ok(Date.now() - started < 600, `finish came before the budget ran out (${Date.now() - started} ms)`);
    assert.ok(srv.requests.some((q) => q.path.endsWith("/finish")), "finish sent");
    assert.ok(logs.some((l) => l.includes("run run-1: failed") && l.includes("1 not uploaded in time")), logs.join("\n"));
    assert.ok(!logs.some((l) => l.includes("gave up on the platform")), logs.join("\n"));
    await new Promise((res) => setTimeout(res, 700)); // the hung PUT times out while its log line is still captured
  } finally {
    console.warn = origWarn;
    console.log = origLog;
    await srv.stop();
  }
});

test("an ATP_URL that is not an http(s) URL disables the reporter with one warning", async () => {
  const warnings: string[] = [];
  const origWarn = console.warn;
  console.warn = (m: string) => warnings.push(m);
  try {
    const r = new AtpReporter({ url: "localhost:8080", token: "atp_ing_t" });
    r.onBegin(fakeConfig(), {} as any);
    r.onTestEnd(fakeTest("a.spec.ts", ["a"]), fakeResult("passed"));
    await r.onEnd({ status: "passed" } as any);
    assert.equal(warnings.length, 1);
    assert.match(warnings[0], /not an http\(s\) URL/);
  } finally {
    console.warn = origWarn;
  }
});

test("no token: reporter disables itself with one warning", async () => {
  const warnings: string[] = [];
  const origWarn = console.warn;
  console.warn = (m: string) => warnings.push(m);
  try {
    const r = new AtpReporter({});
    delete process.env.ATP_URL;
    delete process.env.ATP_TOKEN;
    r.onBegin(fakeConfig(), {} as any);
    r.onTestEnd(fakeTest("a.spec.ts", ["a"]), fakeResult("passed"));
    await r.onEnd({ status: "passed" } as any);
    assert.equal(warnings.length, 1);
    assert.match(warnings[0], /ATP_URL/);
  } finally {
    console.warn = origWarn;
  }
});

test("queue flushes at 100 events or on the timer, in order", async () => {
  const sent: number[][] = [];
  const client = { sendEvents: async (_id: string, events: any[]) => { sent.push(events.map((e) => e.sequence)); return { accepted: events.length, duplicates: 0, identityMap: {} }; } } as unknown as Client;
  const q = new EventQueue({ localRunId: "x", client, spoolDir: tmpdir(), log: () => {}, flushEveryMs: 30 });
  for (let i = 0; i < 150; i++) q.push({ type: "test.finished" });
  await new Promise((r) => setTimeout(r, 80));
  await q.close();
  assert.deepEqual(sent.map((b) => b.length), [100, 50]);
  assert.deepEqual(sent.flat(), Array.from({ length: 150 }, (_, i) => i + 1));
});

test("HAR stripping removes Authorization and cookies before upload (§11.1)", () => {
  const har = {
    log: {
      entries: [
        {
          request: { headers: [{ name: "Authorization", value: "Bearer secret" }, { name: "Accept", value: "*/*" }, { name: "Cookie", value: "sid=1" }], cookies: [{ name: "sid", value: "1" }] },
          response: { headers: [{ name: "Set-Cookie", value: "sid=2" }, { name: "Content-Type", value: "text/html" }], cookies: [] },
        },
      ],
    },
  };
  const { json, removed } = stripHar(JSON.stringify(har));
  assert.equal(removed, 4);
  assert.ok(!json.includes("secret") && !json.includes("sid=1") && !json.includes("sid=2"));
  assert.ok(json.includes("Accept") && json.includes("Content-Type"));
});
