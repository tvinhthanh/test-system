// Browser engines and devices for auto-test sessions (QĐ-19 tier 2): a case
// runs once per browser × device the run asks for — the compatibility
// matrix. Every engine runs behind the same guarded proxy; what each needs
// so that nothing goes around it is set here and proved in
// test/engines.test.mjs (loopback by IP and by name, IPv6 included).
import { chromium, devices, firefox, webkit } from "playwright";

export const ENGINES = { chromium, firefox, webkit };
export const DEVICES = ["desktop", "phone", "tablet"];

/** The engines this checker offers: CHECKER_BROWSERS, default all three. */
export function enabledEngines(env = process.env.CHECKER_BROWSERS) {
  const names = String(env || "chromium,firefox,webkit")
    .split(/[\s,]+/)
    .filter(Boolean)
    .map((n) => n.toLowerCase());
  const bad = names.find((n) => !Object.hasOwn(ENGINES, n));
  if (bad) throw new Error(`CHECKER_BROWSERS: unknown browser ${bad} (chromium, firefox, webkit)`);
  return names.length ? [...new Set(names)] : ["chromium"];
}

/**
 * Launch options for an engine, given the proxy that refuses everything
 * (a context brings its own guarded proxy).
 */
export function launchOptions(name, sinkURL) {
  switch (name) {
    case "chromium":
      return {
        // "<-loopback>": loopback goes through the proxy too.
        proxy: { server: sinkURL, bypass: "<-loopback>" },
        // WebRTC would open UDP around the proxy.
        args: ["--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--disable-quic"],
      };
    case "firefox":
      return {
        proxy: { server: sinkURL },
        firefoxUserPrefs: {
          // Firefox skips the proxy for localhost unless told not to.
          "network.proxy.allow_hijacking_localhost": true,
          "network.proxy.no_proxies_on": "",
          "media.peerconnection.enabled": false,
          "network.http.http3.enable": false,
        },
      };
    case "webkit":
      return { proxy: { server: sinkURL, bypass: "<-loopback>" } };
  }
  throw new Error(`unknown browser ${name}`);
}

/** The context's proxy option for an engine. */
export function contextProxy(name, proxyURL) {
  return name === "firefox" ? { server: proxyURL } : { server: proxyURL, bypass: "<-loopback>" };
}

const DESKTOP = { viewport: { width: 1366, height: 768 } };
// Real device profiles from Playwright. Firefox has no mobile emulation
// (isMobile): it gets the screen size and touch only.
const PROFILES = {
  phone: { chromium: devices["Pixel 7"], webkit: devices["iPhone 13"], firefox: { viewport: { width: 390, height: 844 }, hasTouch: true } },
  tablet: { chromium: devices["Galaxy Tab S4"], webkit: devices["iPad (gen 7)"], firefox: { viewport: { width: 820, height: 1180 }, hasTouch: true } },
};

/** Context options for a device on an engine (viewport, touch, user agent). */
export function deviceOptions(name, device) {
  if (device === "desktop") return { ...DESKTOP };
  const p = PROFILES[device]?.[name];
  if (!p) throw new Error(`unknown device ${device}`);
  const { defaultBrowserType, ...opts } = p;
  return opts;
}
