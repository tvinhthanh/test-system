// Playwright colours its assertion messages with ANSI escape sequences when
// it believes it is writing to a terminal; the same strings arrive in
// TestResult.error. The dashboard renders error text as plain text, so the
// codes would show up literally ("[2m", "[31m") — strip them here, at the
// source, so every consumer of the stored message sees clean text.
// eslint-disable-next-line no-control-regex
const ansi = /\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b[@-Z\\-_]|\x9b[0-9;?]*[ -/]*[@-~]/g;

export function stripAnsi(s: string): string {
  return s.replace(ansi, "");
}
