// Load tests and ownership proof (QĐ-19 tier 3) against a local fixture.
// The guard lets 127.0.0.1 through only when a test says so.
import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { invalidLoad, runLoad, verifyFile } from "../src/load.mjs";

let site, base, hits;
before(async () => {
  hits = 0;
  site = http.createServer((req, res) => {
    hits++;
    if (req.url === "/.well-known/atp-verification.txt") return res.end("atp-verification=tok123\n");
    if (req.url === "/slow") return setTimeout(() => res.end("slow"), 150);
    if (req.url === "/boom") return res.writeHead(500).end("boom");
    if (req.url === "/missing") return res.writeHead(404).end();
    if (req.url === "/away") return res.writeHead(302, { Location: "http://127.0.0.2:1/" }).end();
    res.end("ok");
  });
  await new Promise((r) => site.listen(0, "127.0.0.1", r));
  base = `http://127.0.0.1:${site.address().port}`;
});
after(() => site?.close());

const allow = { allow: ["127.0.0.1"] };

test("a load test counts answers, errors and timings, and follows no redirect", async () => {
  const r = await runLoad({ base, paths: ["/", "/slow", "/boom", "/missing", "/away"], vus: 4, seconds: 5, rampSeconds: 1 }, allow);
  assert.ok(r.requests > 20, JSON.stringify(r));
  assert.ok(r.statuses["500"] > 0 && r.statuses["404"] > 0 && r.statuses["302"] > 0, JSON.stringify(r.statuses));
  assert.equal(r.statuses["no answer"], undefined, JSON.stringify(r.errorSamples));
  assert.equal(r.errors, r.statuses["500"]);
  assert.equal(r.clientErrors, r.statuses["404"]);
  assert.ok(r.latencyMs.p95 >= 100 && r.latencyMs.p50 <= r.latencyMs.p95, JSON.stringify(r.latencyMs));
  assert.equal(r.timeline.length, 5);
  assert.ok(r.rps > 1);
});

test("the guard holds for load: no allow, no traffic", async () => {
  const before = hits;
  const r = await runLoad({ base, paths: ["/"], vus: 2, seconds: 5 }, {});
  assert.equal(r.blocked, true, JSON.stringify(r));
  assert.equal(hits, before, "a request reached loopback around the guard");
  const byName = await runLoad({ base: base.replace("127.0.0.1", "localhost"), paths: ["/"], vus: 1, seconds: 5 }, {});
  assert.equal(byName.blocked, true);
  assert.equal(hits, before);
});

test("the proof file is read through the guard", async () => {
  const ok = await verifyFile(base, allow);
  assert.equal(ok.status, 200);
  assert.match(ok.text, /atp-verification=tok123/);
  const refused = await verifyFile(base, {});
  assert.equal(refused.status, 0);
  assert.match(refused.error, /may not be opened/);
});

test("load request shapes", () => {
  const good = { base: "https://shop.example.com", paths: ["/", "/search?q=x"], vus: 10, seconds: 60, rampSeconds: 10 };
  assert.equal(invalidLoad(good), "");
  assert.match(invalidLoad({ ...good, paths: ["//evil.example/"] }), /stays on the host/);
  assert.match(invalidLoad({ ...good, paths: ["http://evil.example/"] }), /starts with \//);
  assert.match(invalidLoad({ ...good, vus: 1000 }), /vus/);
  assert.match(invalidLoad({ ...good, seconds: 3600 }), /seconds/);
  assert.match(invalidLoad({ ...good, rampSeconds: 60 }), /rampSeconds/);
  assert.match(invalidLoad({ ...good, base: "ftp://x" }), /http\(s\)/);
});
