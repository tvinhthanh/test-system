import { mount } from "@vue/test-utils";
import { expect, it } from "vitest";
import StepsEditor from "../src/features/autotest/StepsEditor.vue";
import {
  checkValueError,
  describeStep,
  needsElement,
  takesValue,
  type AutoStep,
  type StepAction,
} from "../src/features/autotest/steps";

const step = (action: StepAction, value = ""): AutoStep => ({
  do: action,
  by: "",
  role: "",
  name: "",
  value,
  say: "",
  expected: "",
});

it("describes every check in readable words and keeps its value optional", () => {
  for (const action of [
    "check_accessibility",
    "check_performance",
    "check_security",
  ] as const) {
    expect(needsElement.has(action)).toBe(false);
    expect(takesValue.has(action)).toBe(true);
    expect(describeStep(step(action))).not.toContain(action);
  }
  expect(describeStep(step("check_accessibility"))).toContain(
    "serious or worse",
  );
  expect(describeStep(step("check_accessibility", "critical"))).toContain(
    "critical or worse",
  );
  expect(describeStep(step("check_performance"))).toContain("LCP ≤ 2.5 s");
  expect(
    describeStep(step("check_performance", "lcp=1800 cls=0.05")),
  ).toContain("lcp=1800 cls=0.05");
  expect(describeStep(step("check_security"))).toBe(
    "Check security headers and cookies",
  );
  expect(describeStep(step("check_security", "strict"))).toContain(
    "medium findings too",
  );
});

it("checks impact, budget, and security values against the backend rules", () => {
  for (const value of ["", "minor", "moderate", "serious", "critical"])
    expect(checkValueError("check_accessibility", value)).toBe("");
  expect(checkValueError("check_accessibility", "AA")).toMatch(/minor/);
  for (const value of [
    "",
    "lcp=2500 cls=0.1 ttfb=800",
    "FCP<1800,load<=3000;cls=0",
  ])
    expect(checkValueError("check_performance", value)).toBe("");
  for (const value of [
    "lcp=0",
    "ttfb=120001",
    "cls=11",
    "fps=60",
    "lcp=fast",
    "lcp=2 cls=",
  ])
    expect(checkValueError("check_performance", value)).not.toBe("");
  expect(checkValueError("check_security", "")).toBe("");
  expect(checkValueError("check_security", "strict")).toBe("");
  expect(checkValueError("check_security", "medium")).not.toBe("");
});

it("offers check controls without an element and clears incompatible values", async () => {
  const wrapper = mount(StepsEditor, {
    props: { id: "check", modelValue: [step("goto", "/old-path")] },
  });
  const action = wrapper.find('select[aria-label="Step 1: action"]');
  for (const name of [
    "check_accessibility",
    "check_performance",
    "check_security",
  ]) {
    expect(action.find(`option[value="${name}"]`).text()).not.toBe(name);
  }
  await action.setValue("check_accessibility");
  let value = wrapper.emitted("update:modelValue")!.at(-1)![0] as AutoStep[];
  expect(value[0]).toMatchObject({
    do: "check_accessibility",
    by: "",
    value: "",
  });
  await wrapper.setProps({ modelValue: value });
  expect(
    wrapper.find('select[aria-label="Step 1: find the element by"]').exists(),
  ).toBe(false);
  expect(
    wrapper
      .find(
        'select[aria-label="Step 1: lowest failing impact"] option[value=""]',
      )
      .text(),
  ).toBe("Serious (default)");

  await wrapper
    .find('select[aria-label="Step 1: lowest failing impact"]')
    .setValue("critical");
  value = wrapper.emitted("update:modelValue")!.at(-1)![0] as AutoStep[];
  await wrapper.setProps({ modelValue: value });
  await action.setValue("check_security");
  value = wrapper.emitted("update:modelValue")!.at(-1)![0] as AutoStep[];
  expect(value[0]).toMatchObject({ do: "check_security", by: "", value: "" });
  await wrapper.setProps({ modelValue: value });
  expect(
    wrapper
      .find(
        'select[aria-label="Step 1: security threshold"] option[value="strict"]',
      )
      .text(),
  ).toContain("Medium and high");
});

it("explains an invalid performance budget as the user types", async () => {
  const wrapper = mount(StepsEditor, {
    props: { id: "check", modelValue: [step("check_performance")] },
  });
  const budget = wrapper.find('input[aria-label="Step 1: performance budget"]');
  expect(wrapper.text()).toContain("Core Web Vitals good thresholds");
  await budget.setValue("ttfb=0");
  expect(budget.attributes("aria-invalid")).toBe("true");
  expect(wrapper.find('[role="alert"]').text()).toMatch(/out of range/);
  expect((budget.element as HTMLInputElement).validity.customError).toBe(true);
  await budget.setValue("ttfb=800");
  expect(budget.attributes("aria-invalid")).toBe("false");
  expect(wrapper.find('[role="alert"]').exists()).toBe(false);
  expect((budget.element as HTMLInputElement).validity.customError).toBe(false);
});
