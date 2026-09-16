import { test, expect } from "@playwright/test";

test.describe("Checkout", () => {
  test("accepts valid card", async ({}, testInfo) => {
    await testInfo.attach("receipt.png", { body: Buffer.from("\x89PNG-not-really-a-png"), contentType: "image/png" });
    expect(1 + 1).toBe(2);
  });

  test("declines expired card (flaky on purpose)", async ({}, testInfo) => {
    // Fails on the first attempt, passes on the retry → Playwright says "flaky".
    await testInfo.attach("attempt.log", { body: Buffer.from(`attempt ${testInfo.retry}\n`), contentType: "text/plain" });
    expect(testInfo.retry).toBeGreaterThan(0);
  });

  test("shows error on empty field", async () => {
    expect("total: 0").toContain("total: 1");
  });
});
