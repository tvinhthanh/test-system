// Vendors Playwright's trace viewer into dist/trace/ after the build, so the
// dashboard can open a `trace.zip` artifact in it on our own origin
// (`/trace/index.html?trace=<same-origin content URL>`). Same-origin matters:
// the viewer's service worker fetches the trace with the session cookie, and
// nothing about a customer's trace ever leaves our host (no trace.playwright.dev).
// The viewer is third-party code we ship, pinned by the exact playwright-core
// version in package.json; it is not part of the SPA bundle and is skipped by
// the bundle gate (scripts/check-bundle.mjs).
import { cpSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";

const require = createRequire(import.meta.url);
const pkg = dirname(require.resolve("playwright-core/package.json"));
const source = resolve(pkg, "lib/vite/traceViewer");
const version = JSON.parse(
  readFileSync(resolve(pkg, "package.json"), "utf8"),
).version;
const target = resolve("dist/trace");
mkdirSync(target, { recursive: true });
cpSync(source, target, { recursive: true });
writeFileSync(resolve(target, "VERSION"), version + "\n");
console.log(`trace viewer ${version} → dist/trace/`);
