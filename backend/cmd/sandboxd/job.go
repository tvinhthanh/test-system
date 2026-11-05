package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/sandbox"
)

// settings are sandboxd's own (env, main.go).
type settings struct {
	Self           string // this container's id or name: joins each job's network
	PlaywrightImg  string
	K6Img          string
	Runtime        string // "" = runc; runsc (gVisor) when the host has it
	MemoryMB       int
	CPUs           float64
	Pids           int
	AllowPrivate   bool
	AllowAddresses []netip.Addr // tests only: exact addresses the guard lets through
	MaxSeconds     int
}

// runner runs jobs, one container and one network each.
type runner struct {
	d      *docker
	s      settings
	log    *slog.Logger
	lookup sandbox.Resolver

	mu      sync.Mutex
	subnets map[string]netip.Prefix // job networks now: no api call from them
}

const label = "atp.sandbox"

var idRe = regexp.MustCompile(`^[0-9a-f-]{8,64}$`)

// fromJob says whether addr is inside a job's network: the api answers
// nobody there, even with the token.
func (r *runner) fromJob(addr netip.Addr) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.subnets {
		if p.Contains(addr.Unmap()) {
			return true
		}
	}
	return false
}

// sweep removes what an earlier sandboxd left (a crash mid-run).
func (r *runner) sweep(ctx context.Context) {
	ids, err := r.d.containers(ctx, label+"=1")
	if err != nil {
		r.log.Warn("sweep containers", "err", err)
	}
	for _, id := range ids {
		_ = r.d.remove(ctx, id)
	}
	nets, err := r.d.networks(ctx, label+"=1")
	if err != nil {
		r.log.Warn("sweep networks", "err", err)
	}
	for _, n := range nets {
		_ = r.d.disconnect(ctx, n.ID, r.s.Self)
		_ = r.d.removeNetwork(ctx, n.ID)
	}
}

// scripts run as `sh -c`. Every value from the job reaches them as an
// environment variable, never inside this text. The archive comes in on
// stdin; what comes out (exit code, the log's tail, the report) leaves on
// stdout as base64 of a .tar.gz — the container has nowhere else to put it.
// The log is also streamed to stderr, so a run that is killed still has one.
const prelude = `set -u
finish() {
  echo "$1" > /work/out/exit
  [ -n "${TAILPID:-}" ] && kill "$TAILPID" 2>/dev/null
  tail -c 65536 /work/out/run.log > /work/out/log.txt 2>/dev/null
  cd /work/out && tar -czf - exit log.txt $(ls junit.xml summary.json 2>/dev/null) | base64
  exit 0
}
mkdir -p /work/src /work/out && touch /work/out/run.log
tail -n +1 -f /work/out/run.log >&2 & TAILPID=$!
cd /work/src && tar -xzf - >>/work/out/run.log 2>&1 || { echo "the archive did not unpack" >>/work/out/run.log; finish 70; }
`

// Playwright: the image's @playwright/test (matching its browsers) replaces
// any the archive brings; the JUnit reporter is forced on.
const playwrightScript = prelude + `for p in @playwright/test playwright playwright-core; do
  rm -rf "node_modules/$p"; mkdir -p "node_modules/$(dirname "$p")"; ln -s "/opt/atp/node_modules/$p" "node_modules/$p"
done
set -- test --config="$ATP_ENTRY" --reporter=junit,line
[ -n "$ATP_PROJECT" ] && set -- "$@" --project="$ATP_PROJECT"
[ -n "$ATP_GREP" ] && set -- "$@" --grep="$ATP_GREP"
PLAYWRIGHT_JUNIT_OUTPUT_NAME=/work/out/junit.xml node /opt/atp/node_modules/@playwright/test/cli.js "$@" >>/work/out/run.log 2>&1
finish $?
`

// k6: the run's vus and duration replace the script's scenarios (k6's CLI
// shortcuts win), so the caps hold whatever the script says.
const k6Script = prelude + `k6 run --no-usage-report --summary-export=/work/out/summary.json --vus "$ATP_VUS" --duration "${ATP_SECONDS}s" \
  -e BASE_URL="$ATP_BASE_URL" "$ATP_ENTRY" >>/work/out/run.log 2>&1
finish $?
`

// target is the host:port a job may reach, from the environment's address.
func target(base string) (host string, port uint16, err error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", 0, errors.New("the environment has no http(s) address")
	}
	host = strings.ToLower(u.Hostname())
	if _, err := netip.ParseAddr(host); err == nil {
		return "", 0, errors.New("scripts run against an environment addressed by name, not by IP")
	}
	p := u.Port()
	if p == "" {
		p = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	n, err := strconv.ParseUint(p, 10, 16)
	if err != nil || n == 0 {
		return "", 0, errors.New("the environment's port is not a port")
	}
	return host, uint16(n), nil
}

// check refuses a job before anything is created.
func (r *runner) check(j *sandbox.Job) error {
	if !idRe.MatchString(j.ID) {
		return errors.New("id")
	}
	s, err := sandbox.CheckArchive(j.Archive, j.Kind, j.Entry)
	if err != nil {
		return err
	}
	j.Entry = s.Entry
	if j.Project, err = sandbox.CleanFilter("project", j.Project); err != nil {
		return err
	}
	if j.Grep, err = sandbox.CleanFilter("grep", j.Grep); err != nil {
		return err
	}
	if j.Kind == sandbox.KindK6 && (j.VUs < 1 || j.VUs > 200 || j.Seconds < 5 || j.Seconds > 600) {
		return errors.New("k6: vus 1 to 200, seconds 5 to 600")
	}
	if j.TimeoutSeconds < 30 || j.TimeoutSeconds > r.s.MaxSeconds {
		return fmt.Errorf("timeoutSeconds: 30 to %d", r.s.MaxSeconds)
	}
	_, _, err = target(j.Base)
	return err
}

// run does one job end to end; an error is sandboxd's own trouble (Docker
// down), a Result.Error is the job's (said to the tester).
func (r *runner) run(ctx context.Context, j sandbox.Job) (sandbox.Result, error) {
	if err := r.check(&j); err != nil {
		return sandbox.Result{Error: err.Error()}, nil
	}
	host, port, _ := target(j.Base)
	targets, err := sandbox.Targets(ctx, r.lookup, host, port, r.s.AllowPrivate, r.s.AllowAddresses...)
	if err != nil {
		return sandbox.Result{Error: err.Error()}, nil
	}
	name := "atp-sbx-" + j.ID
	labels := map[string]string{label: "1", label + ".job": j.ID}
	// Cleanup runs on a context of its own: the caller's may be gone.
	cleanup := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), time.Minute)
	}

	netID, err := r.d.createNetwork(ctx, name, labels)
	if err != nil {
		return sandbox.Result{}, fmt.Errorf("network: %w", err)
	}
	defer func() {
		c, cancel := cleanup()
		defer cancel()
		_ = r.d.disconnect(c, netID, r.s.Self)
		if err := r.d.removeNetwork(c, netID); err != nil {
			r.log.Warn("network left behind", "job", j.ID, "err", err)
		}
		r.mu.Lock()
		delete(r.subnets, netID)
		r.mu.Unlock()
	}()
	if err := r.d.connect(ctx, netID, r.s.Self); err != nil {
		return sandbox.Result{}, fmt.Errorf("join the job's network: %w", err)
	}
	info, err := r.d.inspectNetwork(ctx, netID)
	if err != nil {
		return sandbox.Result{}, err
	}
	for _, c := range info.IPAM.Config {
		if p, err := netip.ParsePrefix(c.Subnet); err == nil {
			r.mu.Lock()
			r.subnets[netID] = p
			r.mu.Unlock()
		}
	}
	self, err := r.d.inspect(ctx, r.s.Self)
	if err != nil {
		return sandbox.Result{}, err
	}
	myIP := self.NetworkSettings.Networks[name].IPAddress
	if _, err := netip.ParseAddr(myIP); err != nil {
		return sandbox.Result{}, fmt.Errorf("no address on the job's network")
	}
	fwd, err := sandbox.Forward(net.JoinHostPort(myIP, strconv.Itoa(int(port))), targets)
	if err != nil {
		return sandbox.Result{}, fmt.Errorf("forwarder: %w", err)
	}
	defer fwd.Close()

	image, script := r.s.PlaywrightImg, playwrightScript
	if j.Kind == sandbox.KindK6 {
		image, script = r.s.K6Img, k6Script
	}
	env := []string{"HOME=/tmp", "CI=1", "ATP_BASE_URL=" + j.Base, "BASE_URL=" + j.Base, "ATP_ENTRY=" + j.Entry,
		"ATP_PROJECT=" + j.Project, "ATP_GREP=" + j.Grep, "ATP_VUS=" + strconv.Itoa(j.VUs), "ATP_SECONDS=" + strconv.Itoa(j.Seconds)}
	mem := int64(r.s.MemoryMB) << 20
	hostCfg := map[string]any{
		"NetworkMode":    name,
		"ReadonlyRootfs": true,
		"CapDrop":        []string{"ALL"},
		"SecurityOpt":    []string{"no-new-privileges:true"},
		"Privileged":     false,
		"Memory":         mem,
		"MemorySwap":     mem, // no swap on top
		"NanoCpus":       int64(r.s.CPUs * 1e9),
		"PidsLimit":      r.s.Pids,
		"Ulimits":        []map[string]any{{"Name": "nofile", "Soft": 4096, "Hard": 4096}},
		"Tmpfs":          map[string]string{"/work": "rw,nosuid,nodev,size=1g,mode=1777", "/tmp": "rw,nosuid,nodev,size=512m,mode=1777"},
		"ShmSize":        int64(512 << 20),
		"IpcMode":        "private",
		"Init":           true,
		// The environment's name points at the forwarder; nothing else
		// resolves (the network's DNS forwards to an address with no server).
		"ExtraHosts": []string{host + ":" + myIP},
		"Dns":        []string{myIP},
		"DnsSearch":  []string{},
		"LogConfig":  map[string]any{"Type": "json-file", "Config": map[string]string{"max-size": "50m", "max-file": "1"}},
	}
	if r.s.Runtime != "" {
		hostCfg["Runtime"] = r.s.Runtime
	}
	id, err := r.d.create(ctx, name, map[string]any{
		"Image": image, "Entrypoint": []string{"sh", "-c"}, "Cmd": []string{script}, "Env": env,
		"User": "10001:10001", "WorkingDir": "/tmp", "Labels": labels,
		"AttachStdin": true, "OpenStdin": true, "StdinOnce": true, "AttachStdout": true, "AttachStderr": true, "Tty": false,
		"HostConfig": hostCfg,
	})
	if errors.Is(err, errNoSuch) {
		return sandbox.Result{Error: "the sandbox image " + image + " is not on the server (the operator pulls or builds it)"}, nil
	}
	if err != nil {
		return sandbox.Result{}, fmt.Errorf("create: %w", err)
	}
	defer func() {
		c, cancel := cleanup()
		defer cancel()
		if err := r.d.remove(c, id); err != nil {
			r.log.Warn("container left behind", "job", j.ID, "err", err)
		}
	}()
	stdin, err := r.d.attachStdin(ctx, id)
	if err != nil {
		return sandbox.Result{}, fmt.Errorf("attach: %w", err)
	}
	if err := r.d.start(ctx, id); err != nil {
		stdin.Close()
		return sandbox.Result{}, fmt.Errorf("start: %w", err)
	}
	go func() {
		_, _ = io.Copy(stdin, bytes.NewReader(j.Archive))
		stdin.Close()
	}()

	wctx, cancel := context.WithTimeout(ctx, time.Duration(j.TimeoutSeconds)*time.Second)
	code, werr := r.d.wait(wctx, id)
	cancel()
	timedOut := false
	if werr != nil {
		if ctx.Err() != nil && !errors.Is(wctx.Err(), context.DeadlineExceeded) {
			return sandbox.Result{}, ctx.Err()
		}
		timedOut = true
		c, cancel := cleanup()
		_ = r.d.kill(c, id)
		code, _ = r.d.wait(c, id)
		cancel()
	}
	c, cancelLogs := cleanup()
	stdout, stderr, err := r.d.logs(c, id, 48<<20, sandbox.MaxLogBytes)
	cancelLogs()
	if err != nil {
		return sandbox.Result{}, fmt.Errorf("logs: %w", err)
	}
	return collect(stdout, stderr, code, timedOut), nil
}

// collect reads what the script printed: a base64 .tar.gz of exit, log.txt
// and the report. A run that died before it printed (killed, out of
// memory) has its streamed log instead.
func collect(stdout, stderr []byte, code int, timedOut bool) sandbox.Result {
	res := sandbox.Result{ExitCode: code, TimedOut: timedOut, Files: map[string][]byte{}}
	files, ok := unpackOut(stdout)
	if !ok || timedOut {
		res.Log = sandbox.Tail(string(stderr), sandbox.MaxLogBytes)
		if timedOut {
			res.Log += "\n[ATP] the run reached its time limit and was stopped"
		} else if code == 137 {
			res.Log += "\n[ATP] the container was killed (out of memory?)"
		}
		return res
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(files["exit"]))); err == nil {
		res.ExitCode = n
	}
	res.Log = sandbox.Tail(string(files["log.txt"]), sandbox.MaxLogBytes)
	for _, name := range []string{"junit.xml", "summary.json"} {
		if b, ok := files[name]; ok {
			res.Files[name] = b
		}
	}
	return res
}

func unpackOut(stdout []byte) (map[string][]byte, bool) {
	clean := bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, stdout)
	raw, err := base64.StdEncoding.DecodeString(string(clean))
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		name := strings.TrimPrefix(h.Name, "./")
		switch name {
		case "exit", "log.txt", "junit.xml", "summary.json":
		default:
			continue
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, sandbox.MaxReportBytes+1))
		if err != nil || len(b) > sandbox.MaxReportBytes {
			continue
		}
		out[name] = b
	}
	_, ok := out["exit"]
	return out, ok
}
