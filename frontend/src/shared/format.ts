export function duration(ms: number | null) {
  return ms === null
    ? "In progress"
    : ms < 60000
      ? `${(ms / 1000).toFixed(1)}s`
      : `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`;
}
const formatter = new Intl.DateTimeFormat("en-GB", {
  month: "short",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
});
export function time(value: string) {
  return formatter.format(new Date(value));
}
export function short(value: string) {
  return value.slice(0, 8);
}
export function statusLabel(value: string, flaky = 0) {
  return value === "passed" && flaky > 0
    ? `Passed with flaky tests (${flaky})`
    : value.replaceAll("_", " ");
}
export function tone(value: string, flaky = 0) {
  if (
    (value === "passed" && flaky > 0) ||
    ["flaky_pass", "flaky", "suspect"].includes(value)
  )
    return "warning";
  if (["failed", "error", "timed_out", "interrupted"].includes(value))
    return "danger";
  if (["passed", "stable"].includes(value)) return "success";
  if (["running", "queued"].includes(value)) return "info";
  return "neutral";
}
