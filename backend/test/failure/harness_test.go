// Package failure runs the §14.3 failure-injection scenarios against the
// real binaries — cmd/api and cmd/worker built from this tree, a real
// Postgres, processes killed with os.Process.Kill — and checks the
// invariants with SQL, not with mocks. The unit tests in internal/ prove the
// mechanisms (lease expiry, ON CONFLICT, (run, sequence) keys); this proves
// the deployed processes actually use them when they die at a bad moment.
//
// Needs TEST_DATABASE_URL (skipped otherwise, like every Postgres test) and
// a Go toolchain on PATH to build the binaries once per package run.
package failure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

const pepper = "failure-harness-pepper-failure-harness-pepper"

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

// binaries builds cmd/api and cmd/worker once for the whole package.
func binaries(t *testing.T) (api, worker string) {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "atp-failure-")
		if buildErr != nil {
			return
		}
		root, _ := filepath.Abs(filepath.Join("..", ".."))
		for _, name := range []string{"api", "worker"} {
			cmd := exec.Command("go", "build", "-o", filepath.Join(buildDir, name+exe()), "./cmd/"+name)
			cmd.Dir = root
			if out, err := cmd.CombinedOutput(); err != nil {
				buildErr = fmt.Errorf("build %s: %v\n%s", name, err, out)
				return
			}
		}
	})
	if buildErr != nil {
		t.Fatal(buildErr)
	}
	return filepath.Join(buildDir, "api"+exe()), filepath.Join(buildDir, "worker"+exe())
}

func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// harness is one scenario's world: a schema of its own (pgtest), the
// binaries pointed at that schema through DATABASE_URL's search_path
// runtime parameter, and a seeded project with an ingest token.
type harness struct {
	t         *testing.T
	ctx       context.Context
	db        *pg.DB
	dbURL     string
	apiBin    string
	workerBin string
	apiAddr   string
	apiMetric string
	wrkMetric string
	logDir    string
	project   tenancy.ProjectID
	token     string
	http      *http.Client
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	apiBin, workerBin := binaries(t)
	ctx := context.Background()

	var schema string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	base := os.Getenv(pgtest.EnvURL)
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	h := &harness{
		t: t, ctx: ctx, db: db,
		dbURL:     base + sep + "search_path=" + schema,
		apiBin:    apiBin,
		workerBin: workerBin,
		apiAddr:   freeAddr(t),
		apiMetric: freeAddr(t),
		wrkMetric: freeAddr(t),
		logDir:    t.TempDir(),
		http:      &http.Client{Timeout: 10 * time.Second},
	}

	ten, az := tenancy.New(db), authz.New(db, []byte(pepper))
	org, err := ten.CreateOrganization(ctx, "Failure Co", "failure")
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "main")
	p, _ := ten.CreateProject(ctx, ws.ID, "app", "")
	tok, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(p.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	h.project, h.token = p.ID, tok.Plaintext
	return h
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// proc is a running binary. kill is the crash: TerminateProcess / SIGKILL,
// no shutdown hook, no chance to finish the transaction.
type proc struct {
	name string
	cmd  *exec.Cmd
	log  *os.File
	done chan struct{}
}

func (h *harness) env() []string {
	return append(os.Environ(),
		"APP_ENV=dev",
		"DATABASE_URL="+h.dbURL,
		"TOKEN_PEPPER="+pepper,
		"OBJSTORE=memory",
		"COOKIE_SECURE=false",
		"HTTP_ADDR="+h.apiAddr,
	)
}

func (h *harness) start(name, bin, metricsAddr string) *proc {
	h.t.Helper()
	logPath := filepath.Join(h.logDir, fmt.Sprintf("%s-%d.log", name, time.Now().UnixNano()))
	f, err := os.Create(logPath)
	if err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(h.env(), "METRICS_ADDR="+metricsAddr)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		h.t.Fatalf("start %s: %v", name, err)
	}
	p := &proc{name: name, cmd: cmd, log: f, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	h.t.Cleanup(func() { p.kill() })
	// Ready when the metrics listener answers — both binaries bring it up
	// last, after the DB ping.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			h.t.Fatalf("%s exited during boot:\n%s", name, tail(logPath))
		default:
		}
		if res, err := http.Get("http://" + metricsAddr + "/metrics"); err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return p
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatalf("%s not ready after 20 s:\n%s", name, tail(logPath))
	return nil
}

func (h *harness) startAPI() *proc    { return h.start("api", h.apiBin, h.apiMetric) }
func (h *harness) startWorker() *proc { return h.start("worker", h.workerBin, h.wrkMetric) }

func (p *proc) kill() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	<-p.done
	p.log.Close()
}

func (p *proc) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func tail(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	return strings.Join(lines, "\n")
}

// ---- reporter-side HTTP ----

type resp struct {
	code int
	body map[string]any
	err  error
}

func (h *harness) post(path string, body any) resp {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest("POST", "http://"+h.apiAddr+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.token)
	res, err := h.http.Do(req)
	if err != nil {
		return resp{err: err}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (h *harness) createRun(local string) resp {
	return h.post("/v1/runs", map[string]any{
		"localRunId": local, "environmentKey": "ci",
		"trigger":   map[string]any{"ref": "refs/heads/main", "commitSha": "deadbeef"},
		"startedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

// event is one finished test; sequence and the test's own id come from seq
// so a run of N events has N distinct tests unless title is shared.
func event(seq int, file, title string) map[string]any {
	return map[string]any{
		"sequence": seq, "type": "test.finished", "clientTestId": fmt.Sprintf("ct_%s_%s", strings.ReplaceAll(file, "/", "_"), title),
		"occurrenceIndex": 0, "attempt": 0,
		"identity": map[string]any{"framework": "playwright", "filePath": file, "titlePath": []string{"Suite", title}, "paramSignature": ""},
		"status":   "passed", "outcome": "expected", "durationMs": 5,
	}
}

func localRun(n int) string { return fmt.Sprintf("0198c3e1-7b1a-7c3d-9e4f-%012x", n) }

// ---- invariants by SQL ----

func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.db.Q(h.ctx).QueryRow(h.ctx, query, args...).Scan(&n); err != nil {
		h.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (h *harness) exec(query string, args ...any) {
	h.t.Helper()
	if _, err := h.db.Q(h.ctx).Exec(h.ctx, query, args...); err != nil {
		h.t.Fatalf("%s: %v", query, err)
	}
}

// waitFor polls cond until true or the deadline; on timeout it fails with why.
func (h *harness) waitFor(what string, d time.Duration, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	h.t.Fatalf("timed out after %s waiting for %s", d, what)
}

func (h *harness) jobsByStatus() map[string]int {
	rows, err := h.db.Q(h.ctx).Query(h.ctx, `SELECT status, count(*) FROM jobs GROUP BY status`)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		_ = rows.Scan(&s, &n)
		out[s] = n
	}
	return out
}
