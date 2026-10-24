// The browser's only way out (see guard.mjs). Chromium is launched with this
// as its HTTP proxy for every scheme and host, loopback included, so it never
// resolves a name itself: HTTPS and WebSockets arrive as CONNECT host:port,
// plain HTTP as an absolute-form request. Each is resolved here once, every
// address is judged, and the connection goes to the address that was judged.
// A refusal is a 403 the page sees as a failed request; the reason is kept
// so the report can say "blocked", not "broken".
import dns from "node:dns/promises";
import http from "node:http";
import net from "node:net";
import { blockedReason } from "./guard.mjs";

const CONNECT_TIMEOUT_MS = 10_000;

export class Blocked extends Error {}

/** Resolves host and returns the one address to connect to, or throws Blocked. */
export async function resolveAllowed(host, policy) {
  const name = host.replace(/^\[|\]$/g, "");
  if (policy.denyAll) throw new Blocked("no URL check is running");
  const addrs = net.isIP(name)
    ? [{ address: name }]
    : await dns.lookup(name, { all: true, verbatim: true }).catch(() => {
        throw new Blocked(`${name} does not resolve`);
      });
  if (addrs.length === 0) throw new Blocked(`${name} does not resolve`);
  // Every answer is judged: a name with one public and one private address
  // is refused, not raced.
  for (const a of addrs) {
    const why = blockedReason(a.address, policy);
    if (why) throw new Blocked(why);
  }
  return addrs[0].address;
}

/**
 * Starts a proxy on 127.0.0.1 (a random port). One per check, so its
 * refusals are that check's; the browser's launch-level proxy is a denyAll
 * one, so a context without its own proxy reaches nothing.
 * @param {{allowPrivate?: boolean, allow?: string[], denyAll?: boolean}} policy
 * @returns {Promise<{url: string, close: () => Promise<void>, blocked: () => string[]}>}
 */
export async function startProxy(policy) {
  const blocked = [];
  const refuse = (what, err) => {
    const entry = `${what}: ${err.message}`;
    if (blocked.length < 50 && !blocked.includes(entry)) blocked.push(entry);
  };
  const server = http.createServer(async (req, res) => {
    let target;
    try {
      target = new URL(req.url ?? "");
    } catch {
      res.writeHead(400).end();
      return;
    }
    if (target.protocol !== "http:") {
      res.writeHead(400).end();
      return;
    }
    let ip;
    try {
      ip = await resolveAllowed(target.hostname, policy);
    } catch (err) {
      refuse(target.href.slice(0, 300), err);
      res.writeHead(403, { "Content-Type": "text/plain", "X-ATP-Blocked": "1" }).end("blocked by ATP URL check");
      return;
    }
    const headers = { ...req.headers, host: target.host };
    delete headers["proxy-connection"];
    delete headers["proxy-authorization"];
    const upstream = http.request(
      {
        host: ip,
        port: Number(target.port) || 80,
        method: req.method,
        path: target.pathname + target.search,
        headers,
        setHost: false,
        timeout: CONNECT_TIMEOUT_MS,
      },
      (up) => {
        res.writeHead(up.statusCode ?? 502, up.headers);
        up.pipe(res);
      },
    );
    upstream.on("timeout", () => upstream.destroy(new Error("timeout")));
    upstream.on("error", () => {
      if (!res.headersSent) res.writeHead(502);
      res.end();
    });
    res.on("close", () => upstream.destroy());
    req.pipe(upstream);
  });

  server.on("connect", async (req, client, head) => {
    client.on("error", () => {});
    const m = /^(\[[^\]]+\]|[^:]+):(\d{1,5})$/.exec(req.url ?? "");
    if (!m) {
      client.end("HTTP/1.1 400 Bad Request\r\n\r\n");
      return;
    }
    const [, host, port] = m;
    let ip;
    try {
      ip = await resolveAllowed(host, policy);
    } catch (err) {
      refuse(`${host}:${port}`, err);
      // Some clients (Playwright's request API) surface a refused CONNECT as
      // the 403 itself; the marker tells the report it was the guard.
      client.end("HTTP/1.1 403 Forbidden\r\nX-ATP-Blocked: 1\r\nContent-Length: 0\r\n\r\n");
      return;
    }
    const upstream = net.connect({ host: ip, port: Number(port), timeout: CONNECT_TIMEOUT_MS });
    upstream.once("connect", () => {
      upstream.setTimeout(0);
      client.write("HTTP/1.1 200 Connection Established\r\n\r\n");
      if (head?.length) upstream.write(head);
      upstream.pipe(client);
      client.pipe(upstream);
    });
    upstream.on("timeout", () => upstream.destroy());
    upstream.on("error", () => client.destroy());
    client.on("close", () => upstream.destroy());
  });
  // A plain-HTTP WebSocket handshake sent to the proxy instead of tunnelled.
  server.on("upgrade", (_req, socket) => socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"));

  // A CONNECT tunnel leaves the HTTP server's bookkeeping, so close() would
  // wait for the browser's keep-alive tunnels forever: every browser-side
  // socket is tracked and destroyed instead, and each upstream dies with
  // its browser side.
  const sockets = new Set();
  server.on("connection", (s) => {
    sockets.add(s);
    s.once("close", () => sockets.delete(s));
  });

  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = /** @type {net.AddressInfo} */ (server.address());
  return {
    url: `http://127.0.0.1:${port}`,
    blocked: () => [...blocked],
    close: () =>
      new Promise((resolve) => {
        server.close(() => resolve());
        for (const s of sockets) s.destroy();
      }),
  };
}
