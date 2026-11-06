// The time-limit run of sandbox/test/e2e.sh (grep "hangs"). It sits apart
// from sandbox.spec.js: Playwright matches --grep against the file path, so
// in a file named "sandbox" the e2e run's grep "sandbox" picked it up too and
// the whole run hung until its own limit.
const { test } = require("@playwright/test");

test("hangs past the time limit", async () => {
  test.setTimeout(0);
  await new Promise(() => {});
});
