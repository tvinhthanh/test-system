import { computed, ref } from "vue";

// Light/dark is a selected palette (theme.css `.dark`), remembered per browser.
// `system` follows prefers-color-scheme live; index.html applies the same rule
// inline before the first paint so a dark viewer never sees a white flash.
export type ThemeChoice = "light" | "dark" | "system";
const storageKey = "atp-theme";
const choice = ref<ThemeChoice>(read());
const media = window.matchMedia?.("(prefers-color-scheme: dark)");

function read(): ThemeChoice {
  try {
    const v = localStorage.getItem(storageKey);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}
function apply() {
  const dark =
    choice.value === "dark" || (choice.value === "system" && !!media?.matches);
  document.documentElement.classList.toggle("dark", dark);
}
media?.addEventListener("change", apply);
apply();

export function useTheme() {
  const resolved = computed<"light" | "dark">(() =>
    choice.value === "system"
      ? media?.matches
        ? "dark"
        : "light"
      : choice.value,
  );
  function set(value: ThemeChoice) {
    choice.value = value;
    try {
      if (value === "system") localStorage.removeItem(storageKey);
      else localStorage.setItem(storageKey, value);
    } catch {
      // storage unavailable (private mode): the choice lives for this tab
    }
    apply();
  }
  // One button, three states: light → dark → system → light.
  function cycle() {
    set(
      choice.value === "light"
        ? "dark"
        : choice.value === "dark"
          ? "system"
          : "light",
    );
  }
  return { choice, resolved, set, cycle };
}
