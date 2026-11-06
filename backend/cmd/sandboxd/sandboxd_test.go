package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/sandbox"
)

func targz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeDocker is the Engine API as sandboxd uses it. A started container
// "runs" in Go: it reads the archive from stdin, asks the forwarder for the
// site (as a script would, by the environment's name), and prints what the
// real script prints — base64 of a .tar.gz with exit, log.txt, junit.xml.
type fakeDocker struct {
	t       *testing.T
	fwdIP   string
	mu      sync.Mutex
	created map[string]any
	stdin   []byte
	stdinOK chan struct{}
	stdout  []byte
	stderr  []byte
	code    int
	hang    bool // never finishes until killed
	killed  chan struct{}
	removed []string
	nets    map[string]bool
	calls   []string
}

func (f *fakeDocker) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, apiVersion)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+path)
		f.mu.Unlock()
		switch {
		case r.Method == "POST" && path == "/networks/create":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["Internal"] != true {
				f.t.Errorf("job network not internal: %v", in)
			}
			f.mu.Lock()
			f.nets["net1"] = true
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{"Id":"net1"}`)
		case r.Method == "GET" && path == "/networks/net1":
			_, _ = io.WriteString(w, `{"Id":"net1","IPAM":{"Config":[{"Subnet":"10.99.0.0/24"}]}}`)
		case r.Method == "POST" && (path == "/networks/net1/connect" || path == "/networks/net1/disconnect"):
		case r.Method == "DELETE" && path == "/networks/net1":
			f.mu.Lock()
			delete(f.nets, "net1")
			f.mu.Unlock()
		case r.Method == "GET" && path == "/networks", r.Method == "GET" && path == "/containers/json":
			_, _ = io.WriteString(w, `[]`)
		case r.Method == "GET" && strings.HasPrefix(path, "/containers/sandboxd"):
			fmt.Fprintf(w, `{"Id":"self","NetworkSettings":{"Networks":{"atp-sbx-%s":{"IPAddress":%q}}}}`, jobID, f.fwdIP)
		case r.Method == "POST" && path == "/containers/create":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.mu.Lock()
			f.created = in
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{"Id":"c1"}`)
		case r.Method == "POST" && path == "/containers/c1/attach":
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				f.t.Error(err)
				return
			}
			_, _ = io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			go func() {
				b, _ := io.ReadAll(buf)
				f.mu.Lock()
				f.stdin = b
				f.mu.Unlock()
				close(f.stdinOK)
				conn.Close()
			}()
		case r.Method == "POST" && path == "/containers/c1/start":
			go f.runContainer()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "POST" && path == "/containers/c1/wait":
			if f.hang {
				select {
				case <-f.killed:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, `{"StatusCode":137}`)
				return
			}
			<-f.stdinOK
			time.Sleep(50 * time.Millisecond) // the container prints, then stops
			f.mu.Lock()
			code := f.code
			f.mu.Unlock()
			fmt.Fprintf(w, `{"StatusCode":%d}`, code)
		case r.Method == "POST" && path == "/containers/c1/kill":
			select {
			case <-f.killed:
			default:
				close(f.killed)
			}
		case r.Method == "GET" && path == "/containers/c1/logs":
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, chunk := range []struct {
				stream byte
				b      []byte
			}{{1, f.stdout}, {2, f.stderr}} {
				if len(chunk.b) == 0 {
					continue
				}
				var hdr [8]byte
				hdr[0] = chunk.stream
				binary.BigEndian.PutUint32(hdr[4:], uint32(len(chunk.b)))
				_, _ = w.Write(hdr[:])
				_, _ = w.Write(chunk.b)
			}
		case r.Method == "DELETE" && path == "/containers/c1":
			f.mu.Lock()
			f.removed = append(f.removed, "c1")
			f.mu.Unlock()
		default:
			f.t.Errorf("unexpected docker call %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotImplemented)
		}
	})
}

const jobID = "0b6f5c1e-1111-4222-8333-944455556666"

// runContainer plays the script: it reaches the site through the
// forwarder, by the environment's name, and reports in JUnit.
func (f *fakeDocker) runContainer() {
	if f.hang {
		f.mu.Lock()
		f.stderr = []byte("Running 3 tests using 1 worker\n  ✓ login (1.2s)\n")
		f.mu.Unlock()
		return
	}
	<-f.stdinOK
	f.mu.Lock()
	cfg := f.created
	f.mu.Unlock()
	hosts := cfg["HostConfig"].(map[string]any)["ExtraHosts"].([]any)
	name, ip, _ := strings.Cut(hosts[0].(string), ":")
	port := ""
	for _, e := range cfg["Env"].([]any) {
		if v, ok := strings.CutPrefix(e.(string), "ATP_BASE_URL="); ok {
			_, port, _ = net.SplitHostPort(strings.TrimPrefix(v, "http://"))
		}
	}
	page := "unreachable"
	c := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip, port))
	}}, Timeout: 5 * time.Second}
	if resp, err := c.Get("http://" + name + ":" + port + "/"); err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		page = string(b)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, body := range map[string]string{
		"./exit": "1\n", "./log.txt": "1 failed\n  login › shows inbox\n",
		"./junit.xml": `<testsuites><testsuite name="login.spec.ts"><testcase name="shows inbox" classname="login.spec.ts"><failure message="` + page + `"/></testcase></testsuite></testsuites>`,
		"./other.txt": "ignored",
	} {
		_ = tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = io.WriteString(tw, body)
	}
	tw.Close()
	gz.Close()
	enc := base64.StdEncoding.EncodeToString(buf.Bytes())
	var wrapped strings.Builder
	for i := 0; i < len(enc); i += 76 {
		wrapped.WriteString(enc[i:min(i+76, len(enc))] + "\n")
	}
	f.mu.Lock()
	f.stdout = []byte(wrapped.String())
	f.stderr = []byte("streamed log\n")
	f.code = 0 // the script's finish exits 0; the code is in the tar
	f.mu.Unlock()
}

func setupRunner(t *testing.T, hang bool) (*runner, *fakeDocker, string) {
	t.Helper()
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.Host)
	}))
	t.Cleanup(site.Close)
	siteAddr := netip.MustParseAddrPort(site.Listener.Addr().String())
	// The forwarder listens on the job network's address; here another
	// loopback address with the site's port.
	fd := &fakeDocker{t: t, fwdIP: "127.0.0.2", stdinOK: make(chan struct{}), killed: make(chan struct{}), nets: map[string]bool{}, hang: hang}
	dsrv := httptest.NewServer(fd.handler())
	t.Cleanup(dsrv.Close)
	d, err := newDocker("tcp://" + dsrv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := &runner{d: d, log: slog.New(slog.NewTextHandler(io.Discard, nil)), subnets: map[string]netip.Prefix{},
		s: settings{Self: "sandboxd", PlaywrightImg: "atp/sandbox-playwright:test", K6Img: "grafana/k6:test", MemoryMB: 1024, CPUs: 1, Pids: 256,
			MaxSeconds: 900, AllowAddresses: []netip.Addr{siteAddr.Addr()}},
		lookup: func(_ context.Context, _, host string) ([]netip.Addr, error) {
			if host == "staging.example.test" {
				return []netip.Addr{siteAddr.Addr()}, nil
			}
			return nil, fmt.Errorf("nxdomain %s", host)
		}}
	return r, fd, fmt.Sprintf("http://staging.example.test:%d", siteAddr.Port())
}

func TestRunPlaywright(t *testing.T) {
	r, fd, base := setupRunner(t, false)
	archive := targz(t, map[string]string{"playwright.config.ts": "export default {}", "tests/login.spec.ts": "test()"})
	res, err := r.run(context.Background(), sandbox.Job{ID: jobID, Kind: sandbox.KindPlaywright, Base: base, Archive: archive,
		Grep: "inbox", TimeoutSeconds: 60})
	if err != nil || res.Error != "" {
		t.Fatalf("run: %+v %v", res, err)
	}
	if res.ExitCode != 1 || !strings.Contains(res.Log, "1 failed") || res.TimedOut {
		t.Errorf("result: %+v", res)
	}
	// The script reached the site by its name, through the forwarder only.
	if j := string(res.Files["junit.xml"]); !strings.Contains(j, `message="hello from staging.example.test:`) {
		t.Errorf("junit: %s", j)
	}
	if _, ok := res.Files["other.txt"]; ok {
		t.Error("a file sandboxd does not read came back")
	}
	s := fd.snap()
	if !bytes.Equal(s.stdin, archive) {
		t.Error("the container did not get the archive on stdin")
	}
	cfg := s.created
	hc := cfg["HostConfig"].(map[string]any)
	for k, want := range map[string]any{"ReadonlyRootfs": true, "Privileged": false, "NetworkMode": "atp-sbx-" + jobID, "Memory": float64(1 << 30),
		"MemorySwap": float64(1 << 30), "PidsLimit": float64(256), "NanoCpus": float64(1e9), "IpcMode": "private"} {
		if hc[k] != want {
			t.Errorf("HostConfig.%s = %v, want %v", k, hc[k], want)
		}
	}
	if fmt.Sprint(hc["CapDrop"]) != "[ALL]" || fmt.Sprint(hc["SecurityOpt"]) != "[no-new-privileges:true]" || fmt.Sprint(hc["Dns"]) != "[127.0.0.2]" {
		t.Errorf("caps/dns: %v %v %v", hc["CapDrop"], hc["SecurityOpt"], hc["Dns"])
	}
	for _, k := range []string{"Binds", "Mounts", "Devices", "CapAdd", "PortBindings"} {
		if _, ok := hc[k]; ok {
			t.Errorf("HostConfig.%s set", k)
		}
	}
	if cfg["User"] != "10001:10001" || cfg["Image"] != "atp/sandbox-playwright:test" {
		t.Errorf("user/image: %v %v", cfg["User"], cfg["Image"])
	}
	// The filter travels as a variable, never inside the shell text.
	if s := fmt.Sprint(cfg["Cmd"]); strings.Contains(s, "inbox") {
		t.Error("a job value inside the script")
	}
	if !strings.Contains(fmt.Sprint(cfg["Env"]), "ATP_GREP=inbox") {
		t.Errorf("env: %v", cfg["Env"])
	}
	r.mu.Lock()
	subnets := len(r.subnets)
	r.mu.Unlock()
	if s.removed != 1 || s.nets != 0 || subnets != 0 {
		t.Errorf("left behind: removed %d, nets %d, subnets %d", s.removed, s.nets, subnets)
	}
}

// fakeState is the fake's state, copied under its lock: its handlers run
// on the server's goroutines, so tests read it only through snap.
type fakeState struct {
	stdin                []byte
	created              map[string]any
	removed, nets, calls int
}

func (f *fakeDocker) snap() fakeState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeState{stdin: f.stdin, created: f.created, removed: len(f.removed), nets: len(f.nets), calls: len(f.calls)}
}

func TestRunTimeoutAndRefusals(t *testing.T) {
	r, fd, base := setupRunner(t, true)
	archive := targz(t, map[string]string{"load.js": "export default function(){}"})
	// A short timeout for the test: below check()'s floor, so call past it.
	j := sandbox.Job{ID: jobID, Kind: sandbox.KindK6, Base: base, Archive: archive, Entry: "load.js", VUs: 5, Seconds: 10, TimeoutSeconds: 30}
	ctx := context.Background()
	done := make(chan sandbox.Result, 1)
	go func() {
		res, err := r.run(ctx, j)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	// Halfway the job's network is known, and the api refuses it.
	deadline := time.Now().Add(5 * time.Second)
	for !r.fromJob(netip.MustParseAddr("10.99.0.7")) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !r.fromJob(netip.MustParseAddr("10.99.0.7")) {
		t.Fatal("the job network was not tracked")
	}
	srv := httptest.NewServer((&server{r: r, token: strings.Repeat("t", 32), slots: make(chan struct{}, 1)}).routes())
	defer srv.Close()
	req := httptest.NewRequest("POST", "/run", nil)
	req.RemoteAddr = "10.99.0.7:5555"
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	rec := httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a call from a job network: %d", rec.Code)
	}
	req = httptest.NewRequest("POST", "/run", nil)
	rec = httptest.NewRecorder()
	srv.Config.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rec.Code)
	}
	// The container never ends: the test's deadline is the job's.
	select {
	case <-done:
		t.Fatal("finished before its time limit")
	case <-time.After(300 * time.Millisecond):
	}
	close(fd.killed) // as if the limit came; wait answers 137 to the runner
	var res sandbox.Result
	select {
	case res = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no answer after the kill")
	}
	if res.ExitCode != 137 || !strings.Contains(res.Log, "✓ login") {
		t.Errorf("killed run: %+v", res)
	}
	// The container is checked after the run, not when the subnet shows
	// up: the subnet is tracked before the container is created, and
	// reading it then raced the fake's handler (go test -race in CI).
	s := fd.snap()
	if cmd := fmt.Sprint(s.created["Cmd"]); !strings.Contains(cmd, `--vus "$ATP_VUS"`) || s.created["Image"] != "grafana/k6:test" {
		t.Errorf("k6 command: %s", cmd)
	}
	if s.removed != 1 || s.nets != 0 {
		t.Errorf("left behind: %d %d", s.removed, s.nets)
	}

	// Refused before anything is created.
	for name, bad := range map[string]sandbox.Job{
		"ip address":    {ID: jobID, Kind: sandbox.KindK6, Base: "http://93.184.215.14", Archive: archive, Entry: "load.js", VUs: 1, Seconds: 5, TimeoutSeconds: 60},
		"too many vus":  {ID: jobID, Kind: sandbox.KindK6, Base: base, Archive: archive, Entry: "load.js", VUs: 500, Seconds: 5, TimeoutSeconds: 60},
		"long timeout":  {ID: jobID, Kind: sandbox.KindK6, Base: base, Archive: archive, Entry: "load.js", VUs: 1, Seconds: 5, TimeoutSeconds: 99999},
		"bad id":        {ID: "../../x", Kind: sandbox.KindK6, Base: base, Archive: archive, Entry: "load.js", VUs: 1, Seconds: 5, TimeoutSeconds: 60},
		"unknown host":  {ID: jobID, Kind: sandbox.KindK6, Base: "https://nowhere.example.test", Archive: archive, Entry: "load.js", VUs: 1, Seconds: 5, TimeoutSeconds: 60},
		"shell in grep": {ID: jobID, Kind: sandbox.KindPlaywright, Base: base, Archive: targz(t, map[string]string{"playwright.config.js": ""}), Grep: "$(id)", TimeoutSeconds: 60},
	} {
		before := fd.snap().calls
		res, err := r.run(ctx, bad)
		if err != nil || res.Error == "" {
			t.Errorf("%s: %+v %v", name, res, err)
		}
		if fd.snap().calls != before {
			t.Errorf("%s: docker was called", name)
		}
	}
}

func TestCollect(t *testing.T) {
	res := collect([]byte("not base64!"), []byte("partial log"), 137, false)
	if res.ExitCode != 137 || !strings.Contains(res.Log, "out of memory") || !strings.Contains(res.Log, "partial log") {
		t.Errorf("crash: %+v", res)
	}
	var stream bytes.Buffer
	for _, c := range []struct {
		s byte
		b string
	}{{1, "abc"}, {2, "err1"}, {1, "defgh"}, {2, "err2"}} {
		var hdr [8]byte
		hdr[0] = c.s
		binary.BigEndian.PutUint32(hdr[4:], uint32(len(c.b)))
		stream.Write(hdr[:])
		stream.WriteString(c.b)
	}
	out, errb, err := demux(&stream, 5, 100)
	if err != nil || string(out) != "abcde" || string(errb) != "err1err2" {
		t.Errorf("demux: %q %q %v", out, errb, err)
	}
}

// The scripts parse (sh -n); they run only inside the images.
func TestScriptsParse(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh here")
	}
	for name, s := range map[string]string{"playwright": playwrightScript, "k6": k6Script} {
		if out, err := exec.Command(sh, "-n", "-c", s).CombinedOutput(); err != nil {
			t.Errorf("%s: %v %s", name, err, out)
		}
	}
}

// The real script, run by sh with a folder for /work and no k6 on the
// path: the archive unpacks from stdin, the command fails, and what comes
// out on stdout is what collect reads.
func TestScriptRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh here")
	}
	work := filepath.ToSlash(t.TempDir())
	cmd := exec.Command(sh, "-c", strings.ReplaceAll(k6Script, "/work", work))
	cmd.Env = append(os.Environ(), "ATP_ENTRY=load.js", "ATP_VUS=2", "ATP_SECONDS=5", "ATP_BASE_URL=https://x.example", "PATH=/usr/bin:/bin")
	cmd.Stdin = bytes.NewReader(targz(t, map[string]string{"load.js": "export default function(){}"}))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("script: %v %s", err, stderr.String())
	}
	res := collect(stdout.Bytes(), stderr.Bytes(), 0, false)
	if res.ExitCode != 127 || !strings.Contains(res.Log, "k6") {
		t.Errorf("result: %+v (stderr %q)", res, stderr.String())
	}
	if b, err := os.ReadFile(filepath.Join(work, "src", "load.js")); err != nil || !strings.Contains(string(b), "export default") {
		t.Errorf("the archive did not unpack: %v", err)
	}
}

// The worker gives up mid-run (shutdown, its own timeout): the request's
// context ends, and the container and the job's network still go — the
// cleanup runs on a context of its own.
func TestRunCancelCleansUp(t *testing.T) {
	r, fd, base := setupRunner(t, true)
	archive := targz(t, map[string]string{"load.js": "export default function(){}"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type answer struct {
		res sandbox.Result
		err error
	}
	done := make(chan answer, 1)
	go func() {
		res, err := r.run(ctx, sandbox.Job{ID: jobID, Kind: sandbox.KindK6, Base: base, Archive: archive, Entry: "load.js", VUs: 1, Seconds: 5, TimeoutSeconds: 60})
		done <- answer{res, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for fd.snap().created == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if fd.snap().created == nil {
		t.Fatal("the container was not created")
	}
	time.Sleep(100 * time.Millisecond) // started and waiting
	cancel()
	var a answer
	select {
	case a = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after the cancel")
	}
	if !errors.Is(a.err, context.Canceled) {
		t.Errorf("cancelled run: %+v %v", a.res, a.err)
	}
	s := fd.snap()
	if s.removed != 1 || s.nets != 0 || r.fromJob(netip.MustParseAddr("10.99.0.7")) {
		t.Errorf("left behind after a cancel: removed %d, nets %d, subnet tracked %v", s.removed, s.nets, r.fromJob(netip.MustParseAddr("10.99.0.7")))
	}
}
