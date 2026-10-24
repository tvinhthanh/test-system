// The ten kinds of testing the start flow offers (QĐ-15) — the case
// taxonomy's usage tags and purposes (taxonomy.ts) — with what each means
// to a tester, how to find matching cases in the library, whether a URL
// check covers part of it, and a starter set of manual cases for a project
// whose library has none yet. Starter cases are generic on purpose: the
// tester edits them to the product after adding them.
import type { Priority } from "./enums";
import type { CaseFilter, Step } from "./testmgmtRepository";
import type { Taxonomy } from "./taxonomy";

export type TestTypeId =
  | "smoke"
  | "regression"
  | "e2e"
  | "functional"
  | "performance"
  | "security"
  | "usability"
  | "accessibility"
  | "compatibility"
  | "recovery";

export interface StarterCase {
  title: string;
  priority: Priority;
  preconditions: string;
  steps: Step[];
}

export interface TestType {
  id: TestTypeId;
  label: string;
  summary: string;
  // How the library is searched for this kind (API filter).
  filter: CaseFilter;
  // A URL check answers part of it automatically.
  urlCheck: boolean;
  starters: StarterCase[];
}

const s = (action: string, expected: string): Step => ({ action, expected });

export const testTypes: TestType[] = [
  {
    id: "smoke",
    label: "Smoke",
    summary: "Is this build usable at all? A short pass over the main flows.",
    filter: { tag: "smoke" },
    urlCheck: true,
    starters: [
      {
        title: "Home page loads without errors",
        priority: "p0",
        preconditions: "",
        steps: [
          s(
            "Open the site's address",
            "The home page shows within a few seconds, with no error message",
          ),
          s(
            "Look over the header, main content and footer",
            "Nothing is missing, broken or overlapping",
          ),
        ],
      },
      {
        title: "Sign in with a valid account",
        priority: "p0",
        preconditions: "A test account exists",
        steps: [
          s("Open the sign-in page", "The sign-in form shows"),
          s(
            "Enter the account's email and password and submit",
            "You are signed in and see your name or account menu",
          ),
        ],
      },
      {
        title: "Main navigation reaches every section",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Click each item of the main menu in turn",
            "Each opens its page without an error",
          ),
          s(
            "Use the browser's back button after each",
            "You return to the previous page",
          ),
        ],
      },
      {
        title: "Main flow completes once",
        priority: "p0",
        preconditions: "Signed in, with the data the flow needs",
        steps: [
          s(
            "Start the product's main task (order, booking, form …)",
            "The first step opens",
          ),
          s(
            "Complete every step with valid data",
            "A confirmation shows and the result is saved",
          ),
        ],
      },
      {
        title: "Sign out",
        priority: "p1",
        preconditions: "Signed in",
        steps: [
          s("Choose Sign out", "You are signed out and see the public page"),
          s(
            "Press the browser's back button",
            "Signed-in pages are not shown again",
          ),
        ],
      },
    ],
  },
  {
    id: "regression",
    label: "Regression",
    summary:
      "Did the change break what used to work? Main flows again, end to end.",
    filter: { tag: "regression" },
    urlCheck: true,
    starters: [
      {
        title: "Changed feature still does its main job",
        priority: "p0",
        preconditions: "Know which features this release changed",
        steps: [
          s(
            "Use the changed feature the way most users do",
            "It behaves as before, or as the change describes",
          ),
          s(
            "Repeat with the edge data it handled before (empty, long, special characters)",
            "No new errors",
          ),
        ],
      },
      {
        title: "Features next to the change are unaffected",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Use the pages and flows that share data or screens with the change",
            "They work as in the last release",
          ),
        ],
      },
      {
        title: "Previously fixed bugs stay fixed",
        priority: "p1",
        preconditions: "A list of bugs fixed in the last releases",
        steps: [
          s("Repeat the steps of each fixed bug", "None of them comes back"),
        ],
      },
      {
        title: "Saved data from before the release still opens",
        priority: "p1",
        preconditions: "Records created before the update",
        steps: [
          s(
            "Open records created before the release",
            "They show completely and correctly",
          ),
          s("Edit and save one", "The change is saved"),
        ],
      },
    ],
  },
  {
    id: "e2e",
    label: "End to end",
    summary:
      "Complete user journeys, from the user's goal to the result they see.",
    filter: { tag: "e2e" },
    urlCheck: false,
    starters: [
      {
        title: "New user signs up and completes a first task",
        priority: "p0",
        preconditions: "An email address not yet registered",
        steps: [
          s(
            "Sign up with a new email",
            "The account is created (and a confirmation mail arrives, if the product sends one)",
          ),
          s(
            "Complete the first task the product is for",
            "The result is shown and saved",
          ),
          s("Sign out and sign in again", "The result is still there"),
        ],
      },
      {
        title: "Returning user finds and changes earlier work",
        priority: "p1",
        preconditions: "An account with saved data",
        steps: [
          s("Sign in and find an earlier record", "It is listed and opens"),
          s(
            "Change it and save",
            "The change shows in the list and after a reload",
          ),
        ],
      },
      {
        title: "Journey across devices",
        priority: "p2",
        preconditions: "The same account on a computer and a phone",
        steps: [
          s("Start a task on the computer", "It is saved"),
          s(
            "Open the same account on the phone",
            "The task shows and can be finished there",
          ),
        ],
      },
    ],
  },
  {
    id: "functional",
    label: "Functional",
    summary:
      "Does each feature do what it should — valid, invalid and edge input?",
    filter: { purpose: "functional" },
    urlCheck: false,
    starters: [
      {
        title: "Form accepts valid input",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Fill every field with valid values and submit",
            "It is accepted and a confirmation shows",
          ),
        ],
      },
      {
        title: "Form explains invalid input",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Leave a required field empty and submit",
            "The field is marked and a message says what is missing",
          ),
          s(
            "Enter a wrong format (email, phone, date)",
            "A message says the format expected",
          ),
        ],
      },
      {
        title: "Boundaries of each limit",
        priority: "p2",
        preconditions: "Know the limits (length, amount, quantity)",
        steps: [
          s("Enter values exactly at each limit", "They are accepted"),
          s(
            "Enter values just past each limit",
            "They are refused with a clear message",
          ),
        ],
      },
      {
        title: "Search finds and filters",
        priority: "p2",
        preconditions: "Some data to search",
        steps: [
          s("Search for an existing item by part of its name", "It is listed"),
          s(
            "Search for something that does not exist",
            "An empty result with a helpful message",
          ),
        ],
      },
    ],
  },
  {
    id: "performance",
    label: "Performance",
    summary:
      "Speed a tester can observe — page and action times, long lists, slow network.",
    filter: { purpose: "performance" },
    urlCheck: true,
    starters: [
      {
        title: "Main pages open quickly",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Open each main page with an empty cache",
            "Main content shows in under about 2.5 seconds",
          ),
        ],
      },
      {
        title: "Slow network is still usable",
        priority: "p2",
        preconditions: "Browser developer tools: network throttling",
        steps: [
          s(
            "Set the network to Slow 4G and reload the main page",
            "Content appears progressively; nothing breaks",
          ),
          s(
            "Submit the main form",
            "A busy indicator shows; the form is not submitted twice",
          ),
        ],
      },
      {
        title: "Long lists stay responsive",
        priority: "p2",
        preconditions: "An account with many records",
        steps: [
          s(
            "Open the longest list and scroll, sort and filter",
            "Each action answers within a second or two",
          ),
        ],
      },
    ],
  },
  {
    id: "security",
    label: "Security",
    summary:
      "What a tester can check without attack tools — access, other users' data, input.",
    filter: { purpose: "security" },
    urlCheck: true,
    starters: [
      {
        title: "Private pages need sign-in",
        priority: "p0",
        preconditions: "Signed out",
        steps: [
          s(
            "Open the address of a signed-in page directly",
            "You are sent to sign in, not shown the page",
          ),
        ],
      },
      {
        title: "Another user's data cannot be opened",
        priority: "p0",
        preconditions: "Two test accounts, A and B",
        steps: [
          s(
            "As A, open one of A's records and note the address",
            "The record shows",
          ),
          s(
            "Sign in as B and open that same address",
            "B is refused (not found or no access)",
          ),
        ],
      },
      {
        title: "Input is shown as text, not run",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Enter <b>bold</b> and <script>alert(1)</script> in a text field and save",
            "The text shows literally; no bold, no pop-up",
          ),
        ],
      },
      {
        title: "Session ends after sign-out and time-out",
        priority: "p1",
        preconditions: "",
        steps: [
          s("Sign out, then use the back button", "No signed-in page is shown"),
          s(
            "Leave a signed-in tab idle past the time-out and click something",
            "You are asked to sign in again",
          ),
        ],
      },
      {
        title: "Errors do not reveal internals",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Open an address that does not exist and cause a form error",
            "Friendly messages, no stack traces or database errors",
          ),
        ],
      },
    ],
  },
  {
    id: "usability",
    label: "Usability",
    summary: "Can a new user reach their goal without help?",
    filter: { purpose: "usability" },
    urlCheck: false,
    starters: [
      {
        title: "First-time user finds the main task",
        priority: "p1",
        preconditions: "Someone who has not used the product",
        steps: [
          s(
            "Ask them to do the main task without help and watch",
            "They finish without asking; note where they hesitate",
          ),
        ],
      },
      {
        title: "Every action gives feedback",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Save, delete and send something",
            "Each shows a clear confirmation or progress",
          ),
        ],
      },
      {
        title: "Mistakes can be undone or fixed",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Start deleting something important",
            "A confirmation explains what will happen",
          ),
          s(
            "Make a form error",
            "The message says how to fix it and keeps what was typed",
          ),
        ],
      },
    ],
  },
  {
    id: "accessibility",
    label: "Accessibility",
    summary:
      "WCAG 2.1 AA checks by hand — keyboard, focus, screen reader, contrast, zoom.",
    filter: { purpose: "accessibility" },
    urlCheck: true,
    starters: [
      {
        title: "Everything works with the keyboard alone",
        priority: "p0",
        preconditions: "",
        steps: [
          s(
            "Use Tab, Shift+Tab, Enter and Space to do the main task",
            "Every control is reachable and usable",
          ),
          s(
            "Open and close any menu or dialog with the keyboard",
            "Focus moves into it and back; Escape closes it",
          ),
        ],
      },
      {
        title: "Focus is always visible",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Tab through each page",
            "You can always see which element has focus",
          ),
        ],
      },
      {
        title: "Screen reader announces controls",
        priority: "p1",
        preconditions: "NVDA, VoiceOver or TalkBack on",
        steps: [
          s(
            "Move through a form",
            "Each field is announced with its label and whether it is required",
          ),
          s("Submit with an error", "The error is announced"),
        ],
      },
      {
        title: "Readable at 200 % zoom",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Zoom the browser to 200 %",
            "Text reflows; nothing is cut off or overlapping; no sideways scrolling",
          ),
        ],
      },
      {
        title: "Images and icons have text alternatives",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Check images and icon-only buttons with a screen reader or the accessibility inspector",
            "Each has a meaningful name",
          ),
        ],
      },
    ],
  },
  {
    id: "compatibility",
    label: "Compatibility",
    summary: "The same flows on other browsers, devices and screen sizes.",
    filter: { purpose: "compatibility" },
    urlCheck: true,
    starters: [
      {
        title: "Main flow in Chrome, Firefox, Safari and Edge",
        priority: "p1",
        preconditions: "The four browsers",
        steps: [
          s(
            "Do the main task in each browser",
            "It works and looks the same in all four",
          ),
        ],
      },
      {
        title: "Phone, portrait and landscape",
        priority: "p1",
        preconditions: "A phone (or the browser's device mode)",
        steps: [
          s(
            "Do the main task on the phone in portrait",
            "Everything fits; buttons are easy to tap",
          ),
          s(
            "Turn the phone to landscape",
            "The layout adapts; nothing is lost",
          ),
        ],
      },
      {
        title: "Tablet and large screen",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Open the main pages on a tablet and on a wide monitor",
            "The layout uses the space sensibly",
          ),
        ],
      },
    ],
  },
  {
    id: "recovery",
    label: "Recovery",
    summary:
      "When something goes wrong mid-flow — is anything lost or duplicated?",
    filter: { purpose: "recovery" },
    urlCheck: false,
    starters: [
      {
        title: "Network lost while saving",
        priority: "p1",
        preconditions: "Browser developer tools: offline mode",
        steps: [
          s(
            "Fill a form, switch the browser offline and submit",
            "A message says it could not be saved; the input is kept",
          ),
          s("Go back online and submit again", "It saves once"),
        ],
      },
      {
        title: "Refresh or back in the middle of a flow",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Refresh the page halfway through a multi-step flow",
            "You can continue or start again without broken data",
          ),
          s(
            "Press back after the final step",
            "The action is not done a second time",
          ),
        ],
      },
      {
        title: "Double click on submit",
        priority: "p1",
        preconditions: "",
        steps: [
          s(
            "Click the submit button twice quickly",
            "Only one record or order is created",
          ),
        ],
      },
      {
        title: "Session expires during work",
        priority: "p2",
        preconditions: "",
        steps: [
          s(
            "Let the session expire while a form is open, then submit",
            "You sign in again and the input is not lost",
          ),
        ],
      },
    ],
  },
];

export const testTypeById = (id: string) => testTypes.find((t) => t.id === id);

// The taxonomy a new case of this kind gets.
export function taxonomyFor(id: TestTypeId): Taxonomy {
  const tags = ["smoke", "regression", "e2e"] as const;
  return {
    level: "",
    purposes: (tags as readonly string[]).includes(id)
      ? []
      : [id as Taxonomy["purposes"][number]],
    tags: (tags as readonly string[]).includes(id)
      ? [id as Taxonomy["tags"][number]]
      : [],
    executionModes: ["manual"],
    performanceTypes: [],
  };
}
