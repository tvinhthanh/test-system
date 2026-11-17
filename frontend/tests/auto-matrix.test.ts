import { expect, it } from "vitest";
import {
  defaultVariant,
  matrixSize,
  runVariants,
  variantLabel,
} from "../src/features/autotest/matrix";

it("defaults to one Chromium desktop run per case", () => {
  expect(defaultVariant).toBe("chromium/desktop");
  expect(matrixSize(12, [])).toMatchObject({
    variantCount: 1,
    total: 12,
    valid: true,
  });
  expect(variantLabel("")).toBe("Chromium · Desktop");
});

it("enforces both matrix limits before starting a run", () => {
  expect(runVariants).toHaveLength(9);
  expect(matrixSize(100, runVariants.slice(0, 3))).toMatchObject({
    total: 300,
    valid: true,
  });
  expect(matrixSize(101, runVariants.slice(0, 3)).valid).toBe(false);
  expect(matrixSize(1, [...runVariants, "firefox/phone"]).valid).toBe(false);
  expect(matrixSize(1, ["safari/watch"]).valid).toBe(false);
  expect(matrixSize(0, []).valid).toBe(false);
  expect(variantLabel("firefox/phone")).toBe("Firefox · Phone");
});
