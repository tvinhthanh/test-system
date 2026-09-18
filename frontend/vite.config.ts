import { createReadStream, existsSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { defineConfig, type Plugin } from "vitest/config";
import vue from "@vitejs/plugin-vue";

// Serves Playwright's trace viewer at /trace/ from node_modules during
// `vite dev`, the way Caddy serves dist/trace/ in production
// (scripts/copy-trace-viewer.mjs). Same origin as the api proxy, so the
// viewer's service worker can fetch a trace artifact with the session cookie.
function traceViewer(): Plugin {
  const require = createRequire(import.meta.url);
  const root = resolve(
    dirname(require.resolve("playwright-core/package.json")),
    "lib/vite/traceViewer",
  );
  const types: Record<string, string> = {
    ".html": "text/html; charset=utf-8",
    ".js": "text/javascript; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".svg": "image/svg+xml",
    ".ttf": "font/ttf",
    ".webmanifest": "application/manifest+json",
    ".json": "application/json",
    ".png": "image/png",
  };
  return {
    name: "atp-trace-viewer",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const url = new URL(req.url ?? "/", "http://localhost");
        if (!url.pathname.startsWith("/trace/")) return next();
        const rel = normalize(
          decodeURIComponent(
            url.pathname.slice("/trace/".length) || "index.html",
          ),
        );
        const file = join(root, rel);
        if (
          !file.startsWith(root) ||
          !existsSync(file) ||
          statSync(file).isDirectory()
        ) {
          res.statusCode = 404;
          return res.end("not found");
        }
        res.setHeader(
          "Content-Type",
          types[extname(file)] ?? "application/octet-stream",
        );
        res.setHeader("Cache-Control", "no-cache");
        createReadStream(file).pipe(res);
      });
    },
  };
}

export default defineConfig(({ command, mode }) => {
  if (command === "build" && mode === "fixture")
    throw new Error("Fixture mode is development only.");
  return {
    plugins: [vue(), traceViewer()],
    // ATP_API points the dev proxy at another api (a soak's, a staging box) for
    // scripts/live-check.mjs; default is the local `go run ./cmd/api`.
    server: {
      port: 5173,
      proxy: { "/v1": process.env.ATP_API || "http://127.0.0.1:8080" },
    },
    build: { manifest: true, chunkSizeWarningLimit: 200 },
    test: {
      environment: "jsdom",
      include: ["tests/**/*.test.ts"],
      restoreMocks: true,
    },
  };
});
