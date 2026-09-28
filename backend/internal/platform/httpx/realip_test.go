package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRealIPTrustsOnlyTheProxy(t *testing.T) {
	trusted, err := ParsePrefixes("172.16.0.0/12, 10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	h := RealIP(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen = r.RemoteAddr }))
	for _, tc := range []struct{ name, remote, xff, want string }{
		{"through the proxy", "172.18.0.4:5000", "203.0.113.7", "203.0.113.7:0"},
		{"client-forged hops are ignored", "172.18.0.4:5000", "1.2.3.4, 203.0.113.7", "203.0.113.7:0"},
		{"two trusted hops", "10.0.0.5:5000", "198.51.100.2, 172.18.0.4", "198.51.100.2:0"},
		{"untrusted peer keeps its address", "203.0.113.9:4000", "1.2.3.4", "203.0.113.9:4000"},
		{"trusted peer, no header", "172.18.0.4:5000", "", "172.18.0.4:5000"},
		{"malformed hop stops the walk", "172.18.0.4:5000", "1.2.3.4, garbage", "172.18.0.4:5000"},
		{"ipv6 client", "172.18.0.4:5000", "2001:db8::1", "[2001:db8::1]:0"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
		if seen != tc.want {
			t.Errorf("%s: RemoteAddr %q, want %q", tc.name, seen, tc.want)
		}
	}
	if _, err := ParsePrefixes("10.0.0.0/8, nope"); err == nil {
		t.Error("bad prefix accepted")
	}
}
