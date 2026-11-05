// What a script in the sandbox can and cannot do (QĐ-19 tier 4). The e2e
// run greps "sandbox", and Playwright matches --grep against the file path
// too, so every test here runs: the time-limit test lives in limit.spec.js.
const { test, expect } = require("@playwright/test");
const dns = require("node:dns").promises;
const fs = require("node:fs");
const net = require("node:net");

// Resolves true when a TCP connection to host:port opens within ms.
function opens(host, port, ms = 4000) {
  return new Promise((resolve) => {
    const s = net.connect({ host, port });
    const done = (ok) => { s.destroy(); resolve(ok); };
    s.setTimeout(ms, () => done(false));
    s.once("connect", () => done(true));
    s.once("error", () => done(false));
  });
}

test("sandbox reaches the environment by its name", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "hello sandbox" })).toBeVisible();
});

test("sandbox runs unprivileged on a read-only root", async () => {
  expect(process.getuid()).toBe(10001);
  expect(() => fs.writeFileSync("/etc/atp-escape", "x")).toThrow();
  expect(() => fs.writeFileSync("/usr/atp-escape", "x")).toThrow();
  fs.writeFileSync("/tmp/ok", "x"); // its own scratch space works
});

test("sandbox reaches nothing else", async ({ page }) => {
  // No name but the environment's resolves.
  await expect(dns.lookup("example.com")).rejects.toThrow();
  // No route out: public addresses, cloud metadata, the Docker host.
  expect(await opens("1.1.1.1", 443)).toBe(false);
  expect(await opens("169.254.169.254", 80)).toBe(false);
  expect(await opens("172.17.0.1", 2375)).toBe(false);
  // A browser can't get out either, whatever the page asks for.
  await expect(page.goto("https://example.com/", { timeout: 8000 })).rejects.toThrow();
  // The environment's address leads to the forwarder, whose own api
  // refuses anyone on a job network — with or without the token.
  const host = new URL(process.env.BASE_URL).hostname;
  const res = await fetch(`http://${host}:8091/healthz`).catch((e) => e);
  expect(res instanceof Error || res.status === 403).toBe(true);
});

test("sandbox reports a failing test as failed", async () => {
  expect(1, "this test fails on purpose").toBe(2);
});
