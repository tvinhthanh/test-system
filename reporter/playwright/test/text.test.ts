import { test } from "node:test";
import assert from "node:assert/strict";
import { stripAnsi } from "../src/text";

const ESC = "";

test("stripAnsi removes Playwright's colour codes and keeps the message", () => {
  const coloured =
    `Error: ${ESC}[2mexpect(${ESC}[22m${ESC}[31mreceived${ESC}[39m${ESC}[2m).${ESC}[22mtoContain\n\n` +
    `Expected: ${ESC}[32m"total: 1"${ESC}[39m\nReceived: ${ESC}[31m"total: 0"${ESC}[39m`;
  assert.equal(
    stripAnsi(coloured),
    'Error: expect(received).toContain\n\nExpected: "total: 1"\nReceived: "total: 0"',
  );
  assert.equal(stripAnsi("plain text\n  at file.ts:1:2"), "plain text\n  at file.ts:1:2");
  assert.equal(stripAnsi(`${ESC}[1;31mbold red${ESC}[0m`), "bold red");
});
