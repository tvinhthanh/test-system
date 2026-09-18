import { defineConfig } from "@playwright/test";

// Example project used by `npm run example` — three browser-less tests plus
// one real Chromium test that fails on purpose, so the run carries every
// artifact kind the dashboard handles client-side (§4.4: video + trace):
// screenshot, video and trace.zip are retained on failure. Needs a browser:
// `npx playwright install chromium`.
export default defineConfig({
  testDir: "./tests",
  retries: 1,
  workers: 1,
  use: {
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  reporter: [["list"], ["../dist/src/index.js", { environment: "local-example", debug: true }]],
});
