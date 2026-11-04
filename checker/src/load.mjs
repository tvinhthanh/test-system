// Load tests and ownership proof (QĐ-19 tier 3). A load test sends real
// traffic, so the worker only asks for one against a host whose owner
// proved it (verifyFile, or a DNS record the worker reads itself) and
// within the operator's caps; the checker caps again. Every request goes
// through a guarded proxy (proxy.mjs) like a browser's: the generator never
// resolves a name or opens a socket itself. Redirects are not followed — a
// 3xx is an answer, and the load stays on the host that was proved.
import { request } from "playwright";
import { startProxy } from "./proxy.mjs";

export const WELL_KNOWN = "/.well-known/atp-verification.txt";
export const MAX_VUS = 200;
export const MAX_SECONDS = 600;
const MAX_PATHS = 20;
const SAMPLES = 200_000;
const USER_AGENT = "ATP-Load-Test/1";

const firstLine = (err) => String(err?.message ?? err).split("\n")[0].slice(0, 200);

async function client(policy, timeout) {
  const proxy = await startProxy(policy);
  const ctx = await request.newContext({
    proxy: { server: proxy.url },
    ignoreHTTPSErrors: true,
    maxRedirects: 0,
    timeout,
    extraHTTPHeaders: { "User-Agent": USER_AGENT },
  });
  return {
    ctx,
    proxy,
    close: async () => {
      await ctx.dispose().catch(() => {});
      await proxy.close();
    },
  };
}

/**
 * Reads the proof file at the site's root: {status, text} with at most 4 KB
 * of text. The worker compares it with the token.
 */
export async function verifyFile(base, policy) {
  const url = new URL(WELL_KNOWN, base).href;
  const c = await client(policy, 10_000);
  try {
    const res = await c.ctx.get(url, { failOnStatusCode: false });
    if (res.headers()["x-atp-blocked"]) return { status: 0, text: "", error: `${new URL(url).host} may not be opened from this server` };
    const body = await res.body().catch(() => Buffer.alloc(0));
    return { status: res.status(), text: body.subarray(0, 4096).toString("utf8") };
  } catch (err) {
    return { status: 0, text: "", error: firstLine(err) };
  } finally {
    await c.close();
  }
}

/** The first problem with a load request, or "". */
export function invalidLoad({ base, paths, vus, seconds, rampSeconds = 0 }) {
  let u;
  try {
    u = new URL(base);
  } catch {
    return "base must be an http(s) address";
  }
  if ((u.protocol !== "http:" && u.protocol !== "https:") || u.username || u.password) return "base must be an http(s) address without credentials";
  if (!Array.isArray(paths) || paths.length < 1 || paths.length > MAX_PATHS) return `paths: 1 to ${MAX_PATHS}`;
  for (const p of paths) {
    if (typeof p !== "string" || !p.startsWith("/") || p.startsWith("//") || p.length > 2000) return "each path starts with / and stays on the host";
  }
  if (!Number.isInteger(vus) || vus < 1 || vus > MAX_VUS) return `vus: 1 to ${MAX_VUS}`;
  if (!Number.isInteger(seconds) || seconds < 5 || seconds > MAX_SECONDS) return `seconds: 5 to ${MAX_SECONDS}`;
  if (!Number.isInteger(rampSeconds) || rampSeconds < 0 || rampSeconds >= seconds) return "rampSeconds: 0 to less than seconds";
  return "";
}

const pct = (sorted, p) => (sorted.length ? sorted[Math.min(sorted.length - 1, Math.floor((p / 100) * sorted.length))] : 0);

/**
 * Runs `vus` virtual users for `seconds`, each sending GETs to the paths in
 * turn (started evenly over `rampSeconds`). An error is a request that got
 * no answer or a 5xx; a 4xx is counted apart (a missing page is the site's
 * answer, not its failure).
 * @returns {Promise<object>} totals, latency percentiles (ms), status counts, one entry per second
 */
export async function runLoad({ base, paths, vus, seconds, rampSeconds = 0 }, policy) {
  const origin = new URL(base);
  const c = await client(policy, 30_000);
  const started = Date.now();
  const deadline = started + seconds * 1000;
  const latencies = [];
  const statuses = {};
  const errors = [];
  const timeline = Array.from({ length: seconds }, (_, t) => ({ t, requests: 0, errors: 0, totalMs: 0 }));
  let requests = 0;
  let failed = 0;
  let clientErrors = 0;
  let blocked = false;
  const user = async (i) => {
    const delay = rampSeconds > 0 ? Math.floor((i * rampSeconds * 1000) / vus) : 0;
    if (delay) await new Promise((r) => setTimeout(r, delay));
    let n = i;
    while (Date.now() < deadline && !blocked) {
      const url = new URL(paths[n++ % paths.length], origin).href;
      const t0 = Date.now();
      let status = 0;
      try {
        const res = await c.ctx.get(url, { failOnStatusCode: false });
        if (res.headers()["x-atp-blocked"]) blocked = true;
        status = res.status();
        await res.body().catch(() => null);
        await res.dispose().catch(() => {});
      } catch (err) {
        if (errors.length < 10) errors.push(firstLine(err));
      }
      const ms = Date.now() - t0;
      const slot = timeline[Math.min(seconds - 1, Math.floor((t0 - started) / 1000))];
      requests++;
      slot.requests++;
      slot.totalMs += ms;
      if (latencies.length < SAMPLES) latencies.push(ms);
      statuses[status || "no answer"] = (statuses[status || "no answer"] ?? 0) + 1;
      if (status === 0 || status >= 500) {
        failed++;
        slot.errors++;
      } else if (status >= 400) clientErrors++;
    }
  };
  try {
    await Promise.all(Array.from({ length: vus }, (_, i) => user(i)));
  } finally {
    await c.close();
  }
  if (blocked) return { blocked: true, error: `${origin.host} may not be opened from this server` };
  const sorted = latencies.sort((a, b) => a - b);
  const elapsed = (Date.now() - started) / 1000;
  return {
    requests,
    errors: failed,
    clientErrors,
    errorRate: requests ? Math.round((failed / requests) * 10000) / 100 : 0,
    rps: Math.round((requests / elapsed) * 10) / 10,
    latencyMs: { min: sorted[0] ?? 0, p50: pct(sorted, 50), p90: pct(sorted, 90), p95: pct(sorted, 95), p99: pct(sorted, 99), max: sorted.at(-1) ?? 0 },
    statuses,
    errorSamples: errors,
    timeline: timeline.map(({ t, requests: r, errors: e, totalMs }) => ({ t, requests: r, errors: e, avgMs: r ? Math.round(totalMs / r) : 0 })),
    seconds: Math.round(elapsed),
  };
}
