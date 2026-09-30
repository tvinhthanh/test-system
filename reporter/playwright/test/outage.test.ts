import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer as createTcpServer, type Server as TcpServer, type Socket } from "node:net";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

// §7.5 / §14.3 "Reporter mất mạng toàn bộ", on the real binary: the
// customer's `playwright test` runs with this reporter pointed at a
// platform that is not there, and must end with the same exit code, every
// event in the spool, and only a bounded delay. Two kinds of outage: a
// closed port (fails fast) and a server that accepts and never answers
// (every request waits out the 30 s timeout — the one that hurts).
//
// Slow (≈ 1–2 min): skipped unless ATP_OUTAGE_TEST=1.

const PKG = resolve(__dirname, "../..");
const REPORTER = resolve(PKG, "dist/src/index.js");
const CLI = require.resolve("@playwright/test/cli", { paths: [PKG] });
const TESTS = 4;
const TEST_MS = 3000;

function project(): { dir: string; spool: string } {
  // Inside the package so the spec files resolve @playwright/test.
  const root = join(PKG, "test-results");
  mkdirSync(root, { recursive: true });
  const dir = mkdtempSync(join(root, "outage-"));
  const spool = join(dir, "spool");
  const specs = Array.from({ length: TESTS }, (_, i) => `test("t${i}", async () => { await new Promise((r) => setTimeout(r, ${TEST_MS})); expect(${i}).toBe(${i}); });`).join("\n");
  writeFileSync(join(dir, "a.spec.js"), `const { test, expect } = require("@playwright/test");\n${specs}\n`);
  writeFileSync(
    join(dir, "playwright.config.js"),
    `module.exports = { testDir: __dirname, testMatch: "*.spec.js", workers: 1, outputDir: ${JSON.stringify(join(dir, "out"))},
       reporter: [["dot"], [${JSON.stringify(REPORTER)}, { spoolDir: ${JSON.stringify(spool)}, environment: "outage" }]] };\n`,
  );
  return { dir, spool };
}

async function runPlaywright(url: string | undefined): Promise<{ code: number | null; ms: number; out: string; spool: string }> {
  const { dir, spool } = project();
  const env: NodeJS.ProcessEnv = { ...process.env, ATP_TOKEN: "atp_ing_outage" };
  delete env.ATP_URL;
  if (url) env.ATP_URL = url;
  const started = Date.now();
  return new Promise((done) => {
    const p = spawn(process.execPath, [CLI, "test", "--config", join(dir, "playwright.config.js")], { cwd: dir, env });
    let out = "";
    p.stdout.on("data", (d) => (out += d));
    p.stderr.on("data", (d) => (out += d));
    const killer = setTimeout(() => p.kill(), 15 * 60_000);
    p.on("close", (code) => {
      clearTimeout(killer);
      done({ code, ms: Date.now() - started, out, spool });
    });
  });
}

function spooled(dir: string): unknown[] {
  let files: string[] = [];
  try {
    files = readdirSync(dir);
  } catch {
    return [];
  }
  return files.flatMap((f) => readFileSync(join(dir, f), "utf8").trim().split("\n").filter(Boolean).map((l) => JSON.parse(l)));
}

/** Accepts connections and never answers — a hung load balancer. */
async function hangingServer(): Promise<{ url: string; close: () => Promise<void> }> {
  const held: Socket[] = [];
  const srv: TcpServer = createTcpServer((s) => {
    held.push(s);
    s.on("error", () => undefined);
  });
  await new Promise<void>((r) => srv.listen(0, "127.0.0.1", r));
  const port = (srv.address() as { port: number }).port;
  return {
    url: `http://127.0.0.1:${port}`,
    close: () => new Promise((r) => { for (const s of held) s.destroy(); srv.close(() => r()); }),
  };
}

/** A port nobody listens on. */
async function closedPort(): Promise<string> {
  const srv = createTcpServer();
  await new Promise<void>((r) => srv.listen(0, "127.0.0.1", r));
  const port = (srv.address() as { port: number }).port;
  await new Promise<void>((r) => srv.close(() => r()));
  return `http://127.0.0.1:${port}`;
}

const enabled = process.env.ATP_OUTAGE_TEST === "1";

test("outage on the real binary: same exit code, every event spooled, bounded delay", { skip: !enabled && "set ATP_OUTAGE_TEST=1" }, async (t) => {
  const baseline = await runPlaywright(undefined);
  assert.equal(baseline.code, 0, baseline.out);
  t.diagnostic(`baseline (no platform configured): ${baseline.ms} ms`);

  const refused = await runPlaywright(await closedPort());
  t.diagnostic(`closed port: ${refused.ms} ms (+${refused.ms - baseline.ms} ms)`);
  assert.equal(refused.code, baseline.code, refused.out);
  assert.equal(spooled(refused.spool).length, TESTS, refused.out);

  const hang = await hangingServer();
  try {
    const hung = await runPlaywright(hang.url);
    t.diagnostic(`hung server: ${hung.ms} ms (+${hung.ms - baseline.ms} ms)`);
    assert.equal(hung.code, baseline.code, hung.out);
    assert.equal(spooled(hung.spool).length, TESTS, hung.out);
    // Bounded: the reporter gives the platform at most ReporterOptions
    // flushTimeoutMs (30 s) after the last test, plus process teardown.
    assert.ok(hung.ms - baseline.ms < 45_000, `hung server added ${hung.ms - baseline.ms} ms\n${hung.out}`);
  } finally {
    await hang.close();
  }
  // A closed port fails in milliseconds; one retry cycle (1+2+4+8 s) at most.
  assert.ok(refused.ms - baseline.ms < 20_000, `closed port added ${refused.ms - baseline.ms} ms\n${refused.out}`);
});
