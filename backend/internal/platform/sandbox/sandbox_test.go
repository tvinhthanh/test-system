package sandbox

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Pack builds a .tar.gz from name → content; a name ending in "@" is a symlink.
func pack(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if strings.HasSuffix(name, "@") {
			if err := tw.WriteHeader(&tar.Header{Name: strings.TrimSuffix(name, "@"), Typeflag: tar.TypeSymlink, Linkname: body}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestCheckArchive(t *testing.T) {
	pw := pack(t, map[string]string{"playwright.config.ts": "export default {}", "tests/a.spec.ts": "test()"})
	s, err := CheckArchive(pw, KindPlaywright, "")
	if err != nil || s.Entry != "playwright.config.ts" || s.Files != 2 {
		t.Fatalf("playwright: %+v %v", s, err)
	}
	if s, err := CheckArchive(pack(t, map[string]string{"load.js": "export default function(){}"}), KindK6, "./load.js"); err != nil || s.Entry != "load.js" {
		t.Fatalf("k6: %+v %v", s, err)
	}
	for name, c := range map[string]struct {
		archive     []byte
		kind, entry string
		want        string
	}{
		"not gzip":        {[]byte("hello"), KindK6, "a.js", "not a .tar.gz"},
		"no config":       {pack(t, map[string]string{"tests/a.spec.ts": "x"}), KindPlaywright, "", "no playwright.config"},
		"k6 no entry":     {pack(t, map[string]string{"a.js": "x"}), KindK6, "", "the k6 script to run"},
		"entry missing":   {pack(t, map[string]string{"a.js": "x"}), KindK6, "b.js", "not in the archive"},
		"entry escapes":   {pack(t, map[string]string{"a.js": "x"}), KindK6, "../a.js", "relative path"},
		"absolute entry":  {pack(t, map[string]string{"a.js": "x"}), KindK6, "/etc/passwd", "relative path"},
		"dotdot in tar":   {pack(t, map[string]string{"../evil.js": "x", "a.js": "x"}), KindK6, "a.js", "inside the archive"},
		"absolute in tar": {pack(t, map[string]string{"/etc/cron.d/x": "x", "a.js": "x"}), KindK6, "a.js", "inside the archive"},
		"symlink":         {pack(t, map[string]string{"a.js": "x", "secret@": "/etc/shadow"}), KindK6, "a.js", "no links"},
		"bad kind":        {pw, "cypress", "", "kind is"},
		"empty":           {nil, KindK6, "a.js", "empty upload"},
	} {
		if _, err := CheckArchive(c.archive, c.kind, c.entry); !errors.Is(err, ErrArchive) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", name, err, c.want)
		}
	}
	// Too big unpacked: a gzip bomb stops at the limit, not at the end.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "big.bin", Mode: 0o644, Size: MaxUnpacked + 10, Typeflag: tar.TypeReg})
	_, _ = io.CopyN(tw, zeros{}, MaxUnpacked+10)
	tw.Close()
	gz.Close()
	if _, err := CheckArchive(buf.Bytes(), KindPlaywright, "big.bin"); err == nil || !strings.Contains(err.Error(), "unpacked") {
		t.Errorf("bomb: %v", err)
	}
	if _, err := CleanFilter("grep", "login; rm -rf /"); err == nil {
		t.Error("a filter with ; passed")
	}
	if v, err := CleanFilter("project", " chromium "); err != nil || v != "chromium" {
		t.Errorf("project: %q %v", v, err)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestGuard(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "127.0.0.2", "0.0.0.0", "169.254.169.254", "::1", "::ffff:127.0.0.1", "fe80::1", "224.0.0.1", "::"} {
		if Blocked(netip.MustParseAddr(ip), true) == "" {
			t.Errorf("%s allowed", ip)
		}
	}
	for _, ip := range []string{"10.0.0.5", "172.17.0.2", "192.168.1.1", "100.64.0.1", "fd00::1", "::ffff:10.1.1.1", "64:ff9b::a00:1"} {
		if Blocked(netip.MustParseAddr(ip), false) == "" {
			t.Errorf("%s allowed without allowPrivate", ip)
		}
		if Blocked(netip.MustParseAddr(ip), true) != "" {
			t.Errorf("%s refused with allowPrivate", ip)
		}
	}
	if Blocked(netip.MustParseAddr("93.184.215.14"), false) != "" {
		t.Error("a public address refused")
	}
	// Names resolve once; every address must pass.
	resolve := func(_ context.Context, _, host string) ([]netip.Addr, error) {
		switch host {
		case "rebind.example":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("169.254.169.254")}, nil
		case "mixed.example":
			return []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("93.184.215.14")}, nil
		}
		return nil, errors.New("nxdomain")
	}
	if _, err := Targets(context.Background(), resolve, "rebind.example", 443, true); err == nil || !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("rebind: %v", err)
	}
	got, err := Targets(context.Background(), resolve, "mixed.example", 443, false)
	if err != nil || len(got) != 1 || got[0].String() != "93.184.215.14:443" {
		t.Errorf("mixed: %v %v", got, err)
	}
	if _, err := Targets(context.Background(), resolve, "127.0.0.1", 80, false); err == nil {
		t.Error("a loopback literal passed")
	}
}

// The forwarder pipes to the judged address only, both ways, and Close
// cuts what is open.
func TestForwarder(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "site:"+r.Host+r.URL.Path)
	}))
	defer site.Close()
	ap := netip.MustParseAddrPort(site.Listener.Addr().String())
	loop := ap.Addr()
	targets, err := Targets(context.Background(), nil, loop.String(), ap.Port(), false, loop)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Forward("127.0.0.1:0", targets)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the request names, it reaches the site.
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", f.Addr().String())
	}}}
	resp, err := c.Get("http://metadata.google.internal/computeMetadata/v1/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "site:metadata.google.internal/computeMetadata/v1/" {
		t.Fatalf("forwarded to: %q", body)
	}
	hold, err := net.Dial("tcp", f.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_ = hold.SetReadDeadline(deadline())
	if _, err := hold.Read(make([]byte, 1)); err == nil {
		t.Error("an open connection survived Close")
	}
	if _, err := net.Dial("tcp", f.Addr().String()); err == nil {
		t.Error("still listening after Close")
	}
}

func TestK6JUnit(t *testing.T) {
	legacy := `{"metrics": {
		"http_req_duration": {"avg": 120, "p(95)": 640, "thresholds": {"p(95)<500": true, "avg<200": false}},
		"http_req_failed": {"value": 0.01, "thresholds": {"rate<0.05": false}},
		"iterations": {"count": 40}},
	  "root_group": {"name": "", "path": "", "checks": {"status is 200": {"name": "status is 200", "path": "::status is 200", "passes": 38, "fails": 2}},
		"groups": {"login": {"name": "login", "path": "::login", "groups": {}, "checks": {"has token": {"name": "has token", "path": "::login::has token", "passes": 40, "fails": 0}}}}}}`
	rep := string(K6JUnit("load.js", []byte(legacy), 99, "log"))
	for _, want := range []string{`tests="5"`, `failures="2"`, `name="http_req_duration p(95)&lt;500"`, `crossed p(95)&lt;500`, `name="check status is 200"`, `2 of 40 failed`, `name="check login › has token"`} {
		if !strings.Contains(rep, want) {
			t.Errorf("legacy: no %s in\n%s", want, rep)
		}
	}
	// Exit 0 is the judge: nothing crossed, whatever the booleans say.
	if rep := string(K6JUnit("load.js", []byte(legacy), 0, "")); !strings.Contains(rep, `failures="1"`) {
		t.Errorf("exit 0 should fail only the check:\n%s", rep)
	}
	// Newer shape: {ok:false} and a list of checks.
	newer := `{"metrics": {"http_req_duration": {"thresholds": {"p(95)<500": {"ok": false}}}}, "root_group": {"checks": [{"name": "ok", "passes": 3, "fails": 0}], "groups": []}}`
	if rep := string(K6JUnit("load.js", []byte(newer), 99, "")); !strings.Contains(rep, `failures="1"`) || !strings.Contains(rep, `tests="2"`) {
		t.Errorf("newer:\n%s", rep)
	}
	// Exit 99 with nothing marked: still a failure.
	if rep := string(K6JUnit("load.js", []byte(`{"metrics": {}}`), 99, "")); !strings.Contains(rep, `k6 reported thresholds crossed`) {
		t.Errorf("unmarked 99:\n%s", rep)
	}
	if rep := string(K6JUnit("load.js", nil, 107, "SyntaxError")); !strings.Contains(rep, `errors="1"`) || !strings.Contains(rep, "SyntaxError") {
		t.Errorf("no summary:\n%s", rep)
	}
}

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		var j Job
		_ = json.NewDecoder(r.Body).Decode(&j)
		if j.Kind == "busy" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(Result{ExitCode: 1, Log: "ran " + string(j.Archive), Files: map[string][]byte{"junit.xml": []byte("<x/>")}})
	}))
	defer srv.Close()
	c, _ := NewClient(srv.URL, "tok", 60)
	r, err := c.Run(context.Background(), Job{Kind: KindK6, Archive: []byte("zz"), TimeoutSeconds: 5})
	if err != nil || r.ExitCode != 1 || r.Log != "ran zz" || string(r.Files["junit.xml"]) != "<x/>" {
		t.Fatalf("run: %+v %v", r, err)
	}
	if _, err := c.Run(context.Background(), Job{Kind: "busy"}); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	bad, _ := NewClient(srv.URL, "bad", 60)
	if _, err := bad.Run(context.Background(), Job{}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("bad token: %v", err)
	}
	if got := Tail(strings.Repeat("a\n", 100), 11); !strings.HasPrefix(got, "…\n") || len(got) > 15 {
		t.Errorf("tail: %q", got)
	}
}

func deadline() time.Time { return time.Now().Add(2 * time.Second) }
