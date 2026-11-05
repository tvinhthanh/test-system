package sandbox

import (
	"fmt"
	"net/netip"
)

// Which addresses a sandbox's forwarder may connect to: the same rules as
// the checker's guard (checker/src/guard.mjs, §11.6 API7). Never loopback,
// "this host", link-local (cloud metadata), multicast or reserved; private
// ranges only when the operator allows them.
var (
	never = prefixes("0.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fe80::/10", "ff00::/8")
	private = prefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15",
		"fc00::/7", "64:ff9b::/96")
)

func prefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(list))
	for _, p := range list {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

// Blocked says why addr may not be reached ("" when it may). allow lets
// exact addresses through regardless — tests only, for a local fixture.
func Blocked(addr netip.Addr, allowPrivate bool, allow ...netip.Addr) string {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return "not an IP address"
	}
	for _, a := range allow {
		if a.Unmap() == addr {
			return ""
		}
	}
	for _, p := range never {
		if p.Contains(addr) {
			return fmt.Sprintf("%s is a loopback, link-local or reserved address", addr)
		}
	}
	if !allowPrivate {
		for _, p := range private {
			if p.Contains(addr) {
				return fmt.Sprintf("%s is a private network address (the server allows public sites only)", addr)
			}
		}
	}
	return ""
}
