// Which addresses a URL check may reach (QĐ-14, §11.6 API7 SSRF). The
// checker opens URLs that users type, from inside the server's network, so
// every connection the browser makes goes through proxy.mjs, which resolves
// the name once, asks addressAllowed() about every address it got, and
// connects to the address it checked — no second lookup to rebind.
//
// Never reachable: loopback, "this host", link-local (cloud metadata lives
// at 169.254.169.254), multicast, reserved. Private ranges (RFC 1918, CGNAT,
// IPv6 ULA, …) only with URLCHECK_ALLOW_PRIVATE=on — for a staging site on
// the office LAN — and then the checker still runs on a Docker network of
// its own (compose.yml), so Postgres and the api are not among them.
import { BlockList, isIP } from "node:net";

const never = new BlockList();
never.addSubnet("0.0.0.0", 8, "ipv4");
never.addSubnet("127.0.0.0", 8, "ipv4");
never.addSubnet("169.254.0.0", 16, "ipv4");
never.addSubnet("224.0.0.0", 4, "ipv4");
never.addSubnet("240.0.0.0", 4, "ipv4");
never.addAddress("::", "ipv6");
never.addAddress("::1", "ipv6");
never.addSubnet("fe80::", 10, "ipv6");
never.addSubnet("ff00::", 8, "ipv6");

const private_ = new BlockList();
private_.addSubnet("10.0.0.0", 8, "ipv4");
private_.addSubnet("172.16.0.0", 12, "ipv4");
private_.addSubnet("192.168.0.0", 16, "ipv4");
private_.addSubnet("100.64.0.0", 10, "ipv4");
private_.addSubnet("192.0.0.0", 24, "ipv4");
private_.addSubnet("198.18.0.0", 15, "ipv4");
private_.addSubnet("fc00::", 7, "ipv6");
private_.addSubnet("64:ff9b::", 96, "ipv6");

// ::ffff:10.0.0.1 is 10.0.0.1 to the kernel; judge it as that.
function unmap(ip) {
  const m = /^::ffff:(\d+\.\d+\.\d+\.\d+)$/i.exec(ip);
  return m ? m[1] : ip;
}

/**
 * @param {string} ip a literal address (as dns.lookup returns it)
 * @param {{allowPrivate?: boolean, allow?: string[]}} policy allow: exact
 *   addresses let through regardless — tests only, for their local fixture
 * @returns {string} "" when allowed, else why not
 */
export function blockedReason(ip, policy = {}) {
  const addr = unmap(ip);
  const family = isIP(addr);
  if (!family) return "not an IP address";
  if (policy.allow?.includes(addr)) return "";
  const type = family === 4 ? "ipv4" : "ipv6";
  if (never.check(addr, type)) return `${addr} is a loopback, link-local or reserved address`;
  if (!policy.allowPrivate && private_.check(addr, type))
    return `${addr} is a private network address (the server allows public sites only)`;
  return "";
}
