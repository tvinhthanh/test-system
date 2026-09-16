import { test } from "node:test";
import assert from "node:assert/strict";
import { clientTestId, fingerprint, normalizeFilePath, normalizeParams, TITLE_SEP } from "../src/fingerprint";

// Vectors printed by backend/internal/identity/internal/match (Go) for the
// same signals. If these drift, the reporter's clientTestId no longer
// matches what the server computes from the raw signals.
test("fingerprints match the Go implementation byte for byte", () => {
  const cases = [
    {
      s: { framework: "playwright", filePath: "tests/checkout/payment.spec.ts", titlePath: ["Checkout", "Payment", "declines expired card"], paramSignature: "project=chromium" },
      fp: "fp1:be7ebd3473ed68d8ba1fac7424166f3e549b8aaab7f5bfa7ac8ba1449552bad6",
      ct: "ct_be7ebd3473ed68d8ba1fac7424166f3e",
    },
    {
      s: { framework: "Playwright ", filePath: "tests\\auth\\login.spec.TS", titlePath: ["  Auth  ", "Login\tflow"], paramSignature: "" },
      fp: "fp1:4e924755e13bf37bc28f31b7f921d6959223b7b01c549dd987f3e506641bf496",
      ct: "ct_4e924755e13bf37bc28f31b7f921d695",
    },
    {
      s: { framework: "playwright", filePath: "./a.spec.ts", titlePath: ["kiểm tra đơn hàng"], paramSignature: "browser=firefox&viewport=mobile" },
      fp: "fp1:48e96876ccd3242827d07d4b00635c7151a3862a5f9a376c796fa943d56dbccd",
      ct: "ct_48e96876ccd3242827d07d4b00635c71",
    },
  ];
  for (const c of cases) {
    assert.equal(fingerprint(c.s), c.fp);
    assert.equal(clientTestId(c.s), c.ct);
  }
});

test("the title separator is U+0000 (N22)", () => {
  assert.equal(TITLE_SEP.length, 1);
  assert.equal(TITLE_SEP.charCodeAt(0), 0);
  const base = { framework: "playwright", filePath: "tests/a.spec.ts", paramSignature: "" };
  assert.notEqual(fingerprint({ ...base, titlePath: ["Check", "outPayment"] }), fingerprint({ ...base, titlePath: ["Checkout", "Payment"] }));
  assert.notEqual(fingerprint({ ...base, titlePath: ["a b", "c"] }), fingerprint({ ...base, titlePath: ["a", "b c"] }));
});

test("repo root is stripped so dev and CI agree", () => {
  assert.equal(normalizeFilePath("C:\\work\\myapp\\tests\\a.spec.TS", "C:\\work\\myapp"), "tests/a.spec.ts");
  assert.equal(normalizeFilePath("/home/runner/myapp/tests/a.spec.ts", "/home/runner/myapp/"), "tests/a.spec.ts");
  assert.equal(normalizeFilePath("./tests/a.spec.ts"), "tests/a.spec.ts");
  assert.equal(normalizeParams({ viewport: "mobile", browser: "firefox" }), "browser=firefox&viewport=mobile");
});
