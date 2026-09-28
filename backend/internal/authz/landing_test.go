package authz

import "testing"

// Every way a Location can leave the site stays home; paths in the app pass.
func TestLandingStaysInTheApp(t *testing.T) {
	for _, in := range []string{
		"", "evil.example", "https://evil.example/", "//evil.example", "/\\evil.example", "/x\\y",
		"/\t/evil.example", "/\n/evil.example", "/\r/evil.example", "/\x00/evil", "/\x7f/evil",
		"/v1/projects", "http:/evil.example", "/" + string(make([]byte, 600)),
	} {
		if got := landing(in); got != "/" {
			t.Errorf("landing(%q) = %q, want /", in, got)
		}
	}
	for _, ok := range []string{"/", "/p/abc/runs", "/p/abc/runs?status=failed", "/team/workspaces/w1"} {
		if got := landing(ok); got != ok {
			t.Errorf("landing(%q) = %q", ok, got)
		}
	}
}
