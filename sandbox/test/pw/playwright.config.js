// A team's Playwright project as sandboxd receives it (sandbox/test/e2e.sh):
// BASE_URL is the environment's address; two engines, so the run proves
// both reach it through the forwarder.
module.exports = {
  testDir: "tests",
  timeout: 30000,
  retries: 0,
  use: { baseURL: process.env.BASE_URL },
  projects: [
    { name: "chromium", use: { browserName: "chromium" } },
    { name: "firefox", use: { browserName: "firefox" } },
  ],
};
