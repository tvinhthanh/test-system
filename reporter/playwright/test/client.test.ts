import { test } from "node:test";
import assert from "node:assert/strict";
import { ApiError, Client, NetworkError } from "../src/client";

type Step = { status: number; body?: unknown; headers?: Record<string, string>; hang?: boolean; fail?: boolean };

/** A scripted fetch: each call consumes the next step. */
function scripted(steps: Step[]) {
  const calls: { url: string; init: RequestInit }[] = [];
  const f = (async (url: string | URL | Request, init?: RequestInit) => {
    const step = steps.shift();
    if (!step) throw new Error("no more scripted responses");
    calls.push({ url: String(url), init: init ?? {} });
    if (step.fail) throw new TypeError("fetch failed");
    if (step.hang) {
      await new Promise((_, reject) => init?.signal?.addEventListener("abort", () => reject(Object.assign(new Error("aborted"), { name: "AbortError" }))));
    }
    return new Response(step.body === undefined ? "" : JSON.stringify(step.body), { status: step.status, headers: step.headers });
  }) as unknown as typeof fetch;
  return { fetch: f, calls };
}

function client(fetchImpl: typeof globalThis.fetch, sleeps: number[], extra: Partial<ConstructorParameters<typeof Client>[0]> = {}) {
  return new Client({ url: "http://api.test/", token: "atp_ing_x", fetch: fetchImpl, sleep: async (ms) => { sleeps.push(ms); }, ...extra });
}

test("5xx retries with 1/2/4/8 s backoff, then succeeds", async () => {
  const s = scripted([{ status: 503 }, { status: 502 }, { status: 500 }, { status: 201, body: { runId: "r1", limits: {}, identityMap: {} } }]);
  const sleeps: number[] = [];
  const res = await client(s.fetch, sleeps).createRun({});
  assert.equal(res.runId, "r1");
  assert.deepEqual(sleeps, [1000, 2000, 4000]);
  assert.equal(s.calls.length, 4);
  assert.equal(s.calls[0].url, "http://api.test/v1/runs");
  assert.equal((s.calls[0].init.headers as Record<string, string>)["Authorization"], "Bearer atp_ing_x");
});

test("gives up after 5 attempts", async () => {
  const s = scripted(Array(5).fill({ status: 500, body: { error: { code: "INTERNAL", message: "boom" } } }));
  const sleeps: number[] = [];
  await assert.rejects(client(s.fetch, sleeps).finishRun("x", "now"), (e: any) => e instanceof ApiError && e.status === 500 && e.code === "INTERNAL");
  assert.equal(s.calls.length, 5);
  assert.deepEqual(sleeps, [1000, 2000, 4000, 8000]);
});

test("4xx is not retried (§7.5)", async () => {
  const s = scripted([{ status: 400, body: { error: { code: "INVALID_PAYLOAD", message: "bad", details: { field: "x" } } } }, { status: 200 }]);
  const sleeps: number[] = [];
  await assert.rejects(client(s.fetch, sleeps).sendEvents("x", []), (e: any) => e instanceof ApiError && e.code === "INVALID_PAYLOAD" && e.details?.field === "x" && !e.retryable);
  assert.equal(s.calls.length, 1);
  assert.deepEqual(sleeps, []);
});

test("429 is retried and honours Retry-After", async () => {
  const s = scripted([{ status: 429, headers: { "Retry-After": "3" }, body: { error: { code: "RATE_LIMITED" } } }, { status: 200, body: { accepted: 1, duplicates: 0, identityMap: {} } }]);
  const sleeps: number[] = [];
  const res = await client(s.fetch, sleeps).sendEvents("x", [{}]);
  assert.equal(res.accepted, 1);
  assert.deepEqual(sleeps, [3000]);
});

test("timeouts and connection failures are retryable network errors", async () => {
  const s = scripted([{ status: 0, hang: true }, { status: 0, fail: true }, { status: 200, body: { runId: "r", status: "passed", flakyCount: 0, finishedAt: "" } }]);
  const sleeps: number[] = [];
  const res = await client(s.fetch, sleeps, { timeoutMs: 20 }).finishRun("x", "now");
  assert.equal(res.runId, "r");
  assert.equal(s.calls.length, 3);
  const only = scripted([{ status: 0, fail: true }]);
  await assert.rejects(client(only.fetch, [], { maxAttempts: 1 }).finishRun("x", "now"), (e: any) => e instanceof NetworkError);
});

test("presigned PUT sends the pinned headers and returns the ETag", async () => {
  const s = scripted([{ status: 200, headers: { ETag: '"abc"' } }]);
  const res = await client(s.fetch, []).put("http://bucket.test/upload?token=1", new Uint8Array([1, 2, 3]), { "Content-Type": "image/png" });
  assert.equal(res.etag, '"abc"');
  assert.equal(s.calls[0].init.method, "PUT");
  assert.equal((s.calls[0].init.headers as Record<string, string>)["Content-Type"], "image/png");
  assert.equal((s.calls[0].init.headers as Record<string, string>)["Authorization"], undefined);
});

test("once a request uses up its retries, the platform is down for the run: later calls fail without a request", async () => {
  const s = scripted([...Array(5).fill({ status: 0, fail: true }), { status: 200, body: {} }]);
  const c = client(s.fetch, []);
  await assert.rejects(c.createRun({}), (e: any) => e instanceof NetworkError);
  assert.equal(c.unavailable, true);
  await assert.rejects(c.sendEvents("x", [{}]), (e: any) => e instanceof NetworkError && /unavailable earlier/.test(e.message));
  assert.equal(s.calls.length, 5, "no sixth request after the breaker opened");
});

test("a 4xx does not open the breaker; a presigned PUT failing does not either", async () => {
  const s = scripted([{ status: 401, body: { error: { code: "UNAUTHORIZED" } } }, ...Array(5).fill({ status: 503 }), { status: 200, body: { accepted: 1, duplicates: 0, identityMap: {} } }]);
  const c = client(s.fetch, []);
  await assert.rejects(c.createRun({}), (e: any) => e instanceof ApiError && e.status === 401);
  await assert.rejects(c.put("http://store.test/o", new Uint8Array(1), {}), (e: any) => e instanceof ApiError && e.status === 503);
  assert.equal(c.unavailable, false);
  assert.equal((await c.sendEvents("x", [{}])).accepted, 1);
});
