import { defineConfig } from "@playwright/test";

// Example project used by `npm run example` — runs three browser-less tests
// through the reporter against a local platform (ATP_URL / ATP_TOKEN).
export default defineConfig({
  testDir: "./tests",
  retries: 1,
  workers: 1,
  reporter: [["list"], ["../dist/src/index.js", { environment: "local-example", debug: true }]],
});
