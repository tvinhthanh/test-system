// The compatibility matrix (QĐ-19 tier 2): every engine stays behind the
// guarded proxy — loopback by IP, by name and over IPv6 never answers — and
// a case runs the same way on each engine and device.
import { after, before, test } from "node:test";
import assert from "node:assert/strict";
import http from "node:http";
import { DEVICES, deviceOptions, enabledEngines, ENGINES, launchOptions } from "../src/engines.mjs";
import { startProxy } from "../src/proxy.mjs";
import { Session } from "../src/session.mjs";

const page = `<!doctype html><html lang="en"><head><title>Shop</title>
<meta name="viewport" content="width=device-width, initial-scale=1"></head>
<body><main><h1>Shop</h1><label>Search <input id="q"></label><button id="go">Search</button><p id="out" role="status"></p>
<p id="w"></p>
<script>
document.getElementById("w").textContent = "width " + window.innerWidth + (matchMedia("(pointer: coarse)").matches ? " touch" : "");
document.getElementById("go").onclick = () => { document.getElementById("out").textContent = "Results for " + document.getElementById("q").value; };
</script></main></body></html>`;
let site, base, port, sink;
const browsers = {};
const step = (s) => ({ do: "", by: "", role: "", name: "", value: "", say: "", expected: "", ...s });

before(async () => {
  site = http.createServer((req, res) => res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }).end(page));
  await new Promise((r) => site.listen(0, "0.0.0.0", r));
  port = site.address().port;
  base = `http://127.0.0.1:${port}`;
  sink = await startProxy({ denyAll: true });
  for (const name of Object.keys(ENGINES)) browsers[name] = await ENGINES[name].launch(launchOptions(name, sink.url));
});

after(async () => {
  for (const b of Object.values(browsers)) await b.close();
  await sink?.close();
  site?.close();
});

for (const name of Object.keys(ENGINES)) {
  test(`${name}: loopback stays closed by IP, by name and over IPv6`, async () => {
    // No allow list: the guard refuses loopback, whatever the engine.
    const s = await Session.open(browsers[name], { base: `http://localhost:${port}`, policy: {}, engine: name });
    try {
      for (const host of ["127.0.0.1", "localhost", "[::1]"]) {
        await s.page.goto(`http://${host}:${port}/`, { timeout: 8_000 }).catch(() => null);
        // An error page may still be settling: read once it has.
        let html = "";
        for (let i = 0; i < 10 && !html; i++) {
          await s.page.waitForLoadState("load", { timeout: 2_000 }).catch(() => {});
          html = await s.page.content().catch(() => "");
        }
        assert.ok(!html.includes("Results for"), `${name} reached ${host} around the proxy`);
      }
    } finally {
      await s.close();
    }
  });

  test(`${name}: a case runs on phone and desktop`, async () => {
    for (const device of ["desktop", "phone"]) {
      const s = await Session.open(browsers[name], { base, policy: { allow: ["127.0.0.1"] }, engine: name, device });
      try {
        const { ok, results } = await s.run(
          [
            step({ do: "goto", value: "/" }),
            step({ do: "fill", by: "label", name: "Search", value: "shoes" }),
            step({ do: "click", by: "role", role: "button", name: "Search" }),
            step({ do: "expect_text", by: "role", role: "status", value: "Results for shoes" }),
            step({ do: "check_accessibility" }),
          ],
          {},
        );
        assert.ok(ok, `${name}/${device}: ${JSON.stringify(results)}`);
        const width = await s.page.locator("#w").innerText();
        if (device === "phone") assert.match(width, /^width (3\d\d|4\d\d) touch$/, `${name} phone: ${width}`);
        else assert.equal(width, "width 1366");
      } finally {
        await s.close();
      }
    }
  });
}

test("engines and devices", () => {
  assert.deepEqual(enabledEngines(""), ["chromium", "firefox", "webkit"]);
  assert.deepEqual(enabledEngines("Chromium, webkit"), ["chromium", "webkit"]);
  assert.throws(() => enabledEngines("chromium,edge"), /unknown browser edge/);
  for (const name of Object.keys(ENGINES))
    for (const d of DEVICES) assert.ok(deviceOptions(name, d).viewport.width > 300, `${name}/${d}`);
  assert.equal(deviceOptions("firefox", "phone").isMobile, undefined, "Firefox has no mobile emulation");
  assert.equal(deviceOptions("webkit", "phone").isMobile, true);
  assert.equal(deviceOptions("chromium", "tablet").defaultBrowserType, undefined);
  assert.throws(() => deviceOptions("chromium", "watch"), /unknown device/);
});
