import { test, expect } from "@playwright/test";

// A real browser test that fails, so Playwright retains its trace, video and
// screenshot and the reporter uploads all three (kinds trace / video /
// screenshot). No network: the page is set from a string.
test.describe("Receipt", () => {
  test("shows the order total (fails on purpose)", async ({ page }) => {
    await page.setContent(`
      <main>
        <h1>Receipt</h1>
        <p id="total">total: 0</p>
        <button id="pay">Pay</button>
      </main>`);
    await page.click("#pay");
    await expect(page.locator("#total")).toHaveText("total: 1", { timeout: 500 });
  });
});
