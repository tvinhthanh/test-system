import { test } from "node:test";
import assert from "node:assert/strict";
import { blockedReason } from "../src/guard.mjs";
import { Blocked, resolveAllowed } from "../src/proxy.mjs";

test("never: loopback, this host, link-local (cloud metadata), multicast, reserved", () => {
  for (const ip of ["127.0.0.1", "127.9.9.9", "0.0.0.0", "169.254.169.254", "224.0.0.1", "255.255.255.255", "::1", "::", "fe80::1", "ff02::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254"]) {
    assert.notEqual(blockedReason(ip, { allowPrivate: true }), "", ip);
  }
});

test("private ranges only with allowPrivate", () => {
  for (const ip of ["10.0.0.5", "172.16.3.4", "172.31.255.255", "192.168.1.20", "100.64.0.1", "fd00::1", "::ffff:10.1.2.3"]) {
    assert.notEqual(blockedReason(ip, {}), "", ip);
    assert.equal(blockedReason(ip, { allowPrivate: true }), "", ip);
  }
});

test("public addresses pass", () => {
  for (const ip of ["93.184.216.34", "1.1.1.1", "172.32.0.1", "2606:4700:4700::1111"]) {
    assert.equal(blockedReason(ip, {}), "", ip);
  }
});

test("allow lets exact test addresses through, nothing else", () => {
  assert.equal(blockedReason("127.0.0.1", { allow: ["127.0.0.1"] }), "");
  assert.notEqual(blockedReason("127.0.0.2", { allow: ["127.0.0.1"] }), "");
});

test("names are judged by every address they resolve to", async () => {
  await assert.rejects(resolveAllowed("localhost", {}), Blocked);
  await assert.rejects(resolveAllowed("[::1]", {}), Blocked);
  await assert.rejects(resolveAllowed("does-not-exist.invalid", {}), Blocked);
  await assert.rejects(resolveAllowed("1.1.1.1", { denyAll: true }), Blocked);
  assert.equal(await resolveAllowed("1.1.1.1", {}), "1.1.1.1");
});
