// The vocabulary of a runnable case's steps (QĐ-16), shared by the
// repository (decoding) and the editor (which fields a step shows). The
// server checks every step again (internal/autotest/steps.go) and the
// checker once more before it runs one (checker/src/steps.mjs).

export const stepActions = [
  "goto",
  "click",
  "fill",
  "select",
  "check",
  "uncheck",
  "press",
  "hover",
  "wait",
  "expect_visible",
  "expect_hidden",
  "expect_text",
  "expect_url",
  "expect_value",
  "expect_title",
] as const;
export type StepAction = (typeof stepActions)[number];
export const locators = [
  "",
  "role",
  "label",
  "placeholder",
  "text",
  "testid",
  "css",
] as const;
export type Locator = (typeof locators)[number];

export interface AutoStep {
  do: StepAction;
  by: Locator;
  role: string;
  name: string;
  value: string;
  say: string;
  expected: string;
}

// The case's steps in words — the same wording as the server's
// (internal/autotest/steps.go Describe), for a step nobody described.
export function describeStep(s: AutoStep): string {
  const el = element(s);
  switch (s.do) {
    case "goto":
      return `Open ${s.value}`;
    case "click":
      return `Click ${el}`;
    case "fill":
      return s.value ? `Type “${s.value}” into ${el}` : `Clear ${el}`;
    case "select":
      return `Choose “${s.value}” in ${el}`;
    case "check":
      return `Tick ${el}`;
    case "uncheck":
      return `Untick ${el}`;
    case "press":
      return s.by ? `Press ${s.value} in ${el}` : `Press ${s.value}`;
    case "hover":
      return `Point at ${el}`;
    case "wait":
      return `Wait ${s.value} ms`;
    case "expect_visible":
      return `See ${el}`;
    case "expect_hidden":
      return `No longer see ${el}`;
    case "expect_text":
      return s.by
        ? `See “${s.value}” in ${el}`
        : `See the text “${s.value}” on the page`;
    case "expect_url":
      return `The address contains “${s.value}”`;
    case "expect_value":
      return `${el.charAt(0).toUpperCase()}${el.slice(1)} holds “${s.value}”`;
    case "expect_title":
      return `The page title contains “${s.value}”`;
  }
}
function element(s: AutoStep) {
  switch (s.by) {
    case "role":
      return s.name ? `${s.role} “${s.name}”` : `a ${s.role}`;
    case "label":
    case "placeholder":
      return `the field “${s.name}”`;
    case "text":
      return `“${s.name}”`;
    case "testid":
      return `the element [${s.name}]`;
    case "css":
      return `the element ${s.name}`;
  }
  return "the page";
}

// Which actions need an element, and which a value — the server checks
// the same (steps.go Validate); the editor uses it to show the right fields.
export const needsElement = new Set<StepAction>([
  "click",
  "fill",
  "select",
  "check",
  "uncheck",
  "hover",
  "expect_visible",
  "expect_hidden",
  "expect_value",
]);
export const mayHaveElement = new Set<StepAction>(["press", "expect_text"]);
export const takesValue = new Set<StepAction>([
  "goto",
  "fill",
  "select",
  "press",
  "wait",
  "expect_text",
  "expect_url",
  "expect_value",
  "expect_title",
]);
export const blankStep = (): AutoStep => ({
  do: "click",
  by: "role",
  role: "button",
  name: "",
  value: "",
  say: "",
  expected: "",
});
// The roles offered first in the editor (the server accepts every ARIA role).
export const commonRoles = [
  "button",
  "link",
  "textbox",
  "searchbox",
  "checkbox",
  "radio",
  "combobox",
  "option",
  "switch",
  "tab",
  "menuitem",
  "heading",
  "listitem",
  "dialog",
  "alert",
  "status",
  "log",
  "img",
  "navigation",
  "main",
  "region",
  "table",
  "row",
  "cell",
] as const;

// A runnable case being written or reviewed (the editor's form).
export interface CaseForm {
  title: string;
  priority: string;
  testType: string;
  target: string;
  account: string;
  inputs: Record<string, string>;
  steps: AutoStep[];
}
