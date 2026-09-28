package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// RealIP makes r.RemoteAddr the client's address when the request came
// through a trusted reverse proxy, so everything keyed by client IP (the
// login, MFA, invitation and reset limits of §11.2) counts per client and
// not once for the whole proxy.
//
// Without it, behind Caddy every request carries Caddy's address and "5
// failures per IP" becomes 5 failures for everyone — anyone could lock every
// user out (found in the 28-09 security review).
//
// Only a trusted peer's X-Forwarded-For is read, from the right: the first
// address that is not itself a trusted proxy is the client. Entries to its
// left were written by the client and are ignored, so the header cannot be
// used to pick an address. With no trusted prefixes this changes nothing.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a.Unmap()) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			peer, perr := netip.ParseAddr(host)
			if err == nil && perr == nil && isTrusted(peer) {
				hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break // a malformed hop: stop at what the proxies vouch for
					}
					if !isTrusted(a) {
						r2 := r.Clone(r.Context())
						r2.RemoteAddr = net.JoinHostPort(a.Unmap().String(), "0")
						r = r2
						break
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ParsePrefixes reads a comma-separated list of CIDRs or single addresses.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("%q: not an address or CIDR", f)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("%q: not an address or CIDR", f)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
