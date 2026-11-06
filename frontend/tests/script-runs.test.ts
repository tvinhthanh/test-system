import { expect, it } from "vitest";
import { ApiError } from "../src/core/api/client";
import { buildScriptMultipart } from "../src/features/autotest/scriptForm";
import { explainScriptError } from "../src/features/autotest/scriptErrors";

const base = {
  archive: new File(["archive"], "tests.tar.gz", { type: "application/gzip" }),
  kind: "playwright" as const,
  environmentId: "env-1",
  entry: "playwright.config.ts",
  project: "chromium",
  grep: "checkout",
  vus: "",
  seconds: "",
  timeoutSeconds: "300",
};
it("builds the script multipart body and includes runner-specific fields", () => {
  const result = buildScriptMultipart(base);
  expect(result.error).toBe("");
  expect(
    result.data && Object.fromEntries(result.data.entries()),
  ).toMatchObject({
    kind: "playwright",
    environmentId: "env-1",
    entry: "playwright.config.ts",
    project: "chromium",
    grep: "checkout",
    timeoutSeconds: "300",
  });
  const k6 = buildScriptMultipart({
    ...base,
    kind: "k6",
    entry: "load.js",
    vus: "20",
    seconds: "60",
    project: "ignored",
    grep: "ignored",
  });
  expect(k6.data?.get("vus")).toBe("20");
  expect(k6.data?.get("seconds")).toBe("60");
  expect(k6.data?.has("project")).toBe(false);
});
it("rejects invalid archives and k6 bounds", () => {
  expect(
    buildScriptMultipart({ ...base, archive: new File(["x"], "tests.zip") })
      .error,
  ).toMatch(/\.tar\.gz/);
  expect(
    buildScriptMultipart({
      ...base,
      kind: "k6",
      entry: "",
      vus: "10",
      seconds: "30",
    }).error,
  ).toMatch(/entry/i);
  expect(
    buildScriptMultipart({
      ...base,
      kind: "k6",
      entry: "load.js",
      vus: "201",
      seconds: "30",
    }).error,
  ).toMatch(/between 1 and 200/);
});
it("uses stable script error codes and server detail for bad archives", () => {
  for (const [code, phrase] of [
    ["SCRIPTS_OFF", "off"],
    ["OWNERSHIP_NOT_PROVED", "ownership"],
    ["PAYLOAD_TOO_LARGE", "10 MB"],
    ["TOO_MANY_SCRIPT_RUNS", "one script"],
  ] as const) {
    const error = explainScriptError(
      new ApiError(409, code, "fallback"),
    ) as ApiError;
    expect(error.message.toLowerCase()).toContain(phrase.toLowerCase());
  }
  expect(
    (
      explainScriptError(
        new ApiError(
          400,
          "INVALID_PAYLOAD",
          "bad",
          undefined,
          "archive contains a symlink",
        ),
      ) as ApiError
    ).message,
  ).toBe("archive contains a symlink");
});
