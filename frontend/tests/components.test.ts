import { mount } from "@vue/test-utils";
import { it, expect } from "vitest";
import StatusBadge from "../src/shared/StatusBadge.vue";
import TechnicalBlock from "../src/shared/TechnicalBlock.vue";

it("passed runs with flaky tests never show clean green", () => {
  const wrapper = mount(StatusBadge, { props: { status: "passed", flaky: 2 } });
  expect(wrapper.text()).toBe("Passed with flaky tests (2)");
  expect(wrapper.classes()).toContain("warning");
});
it("renders report payload as literal selectable text, not executable HTML", () => {
  const payload = '<img src=x onerror="alert(1)">';
  const wrapper = mount(TechnicalBlock, {
    props: { title: "Stack", text: payload },
  });
  expect(wrapper.find("img").exists()).toBe(false);
  expect(wrapper.find("pre").text()).toBe(payload);
  expect(wrapper.get("button").attributes("aria-label")).toBe("Copy Stack");
});
