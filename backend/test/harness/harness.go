// Package harness runs the real binaries — cmd/api and cmd/worker built
// from this tree — against a real Postgres schema, for the gate suites in
// test/failure (§14.3) and test/load (§14.2). The unit tests in internal/
// prove the mechanisms; these suites prove the deployed processes use them
// when they die at a bad moment or when a hundred reporters arrive at once.
//
// Needs TEST_DATABASE_URL (skipped otherwise, like every Postgres test) and
// a Go toolchain on PATH to build the binaries once per test package.
package harness

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
	"strconv"
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

// Pepper is the TOKEN_PEPPER the binaries and the seeding share.
const Pepper = "gate-harness-pepper-gate-harness-pepper!"

var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

// Binaries builds cmd/api and cmd/worker once per process.
func Binaries(t *testing.T) (api, worker string) {
	t.Helper()
	buildOnce.Do(func() {
		buildDir, buildErr = os.MkdirTemp("", "atp-gate-")
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

// Cleanup removes the built binaries; call it from TestMain after m.Run.
func Cleanup() {
	if buildDir != "" {
		os.RemoveAll(buildDir)
	}
}

func exe() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// Harness is one test's world: a schema of its own (pgtest), the binaries
// pointed at that schema through DATABASE_URL's search_path runtime
// parameter, and a seeded organization with one project and ingest token.
type Harness struct {
	T       *testing.T
	Ctx     context.Context
	DB      *pg.DB
	DBURL   string
	APIAddr string
	Org     tenancy.OrganizationID
	WS      tenancy.WorkspaceID
	Project tenancy.ProjectID
	Token   string
	HTTP    *http.Client

	tenancy   tenancy.Service
	authz     authz.Service
	appName   string
	apiBin    string
	workerBin string
	apiMetric string
	wrkMetric string
	logDir    string
}

// New builds the world. Every process it starts is killed in t.Cleanup.
func New(t *testing.T) *Harness {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	apiBin, workerBin := Binaries(t)
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
	h := &Harness{
		T: t, Ctx: ctx, DB: db,
		// application_name tags the binaries' connections so a scenario can
		// terminate exactly them — other packages share this database.
		DBURL:     base + sep + "search_path=" + schema + "&application_name=atp-" + schema,
		appName:   "atp-" + schema,
		APIAddr:   freeAddr(t),
		HTTP:      &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 0}},
		tenancy:   tenancy.New(db),
		authz:     authz.New(db, []byte(Pepper)),
		apiBin:    apiBin,
		workerBin: workerBin,
		apiMetric: freeAddr(t),
		wrkMetric: freeAddr(t),
		logDir:    t.TempDir(),
	}
	org, err := h.tenancy.CreateOrganization(ctx, "Gate Co", "gate")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := h.tenancy.CreateWorkspace(ctx, org.ID, "main")
	if err != nil {
		t.Fatal(err)
	}
	h.Org, h.WS = org.ID, ws.ID
	h.Project, h.Token = h.NewProject("app")
	return h
}

// NewProject adds a project to the harness workspace with one ingest token.
func (h *Harness) NewProject(name string) (tenancy.ProjectID, string) {
	h.T.Helper()
	p, err := h.tenancy.CreateProject(h.Ctx, h.WS, name, "")
	if err != nil {
		h.T.Fatal(err)
	}
	return p.ID, h.IssueToken(p.ID, "ci")
}

// IssueToken mints another project:ingest token — the per-token rate limit
// (§7.4) is per CI pipeline, so a load test with several pipelines on one
// project holds several tokens, as real customers would.
func (h *Harness) IssueToken(project tenancy.ProjectID, name string) string {
	h.T.Helper()
	tok, err := h.authz.IssueToken(h.Ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(project)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: name})
	if err != nil {
		h.T.Fatal(err)
	}
	return tok.Plaintext
}

// NewUser creates a verified user with the password and a role on project.
func (h *Harness) NewUser(email, password string, project tenancy.ProjectID, role authz.Role) string {
	h.T.Helper()
	u, err := h.authz.CreateUser(h.Ctx, email)
	if err != nil {
		h.T.Fatal(err)
	}
	if err := h.authz.SetPassword(h.Ctx, u.ID, password, ""); err != nil {
		h.T.Fatal(err)
	}
	if err := h.authz.VerifyEmail(h.Ctx, u.ID); err != nil {
		h.T.Fatal(err)
	}
	if _, err := h.authz.GrantRole(h.Ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project)}, role); err != nil {
		h.T.Fatal(err)
	}
	return u.ID
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// Proc is a running binary. Kill is the crash: TerminateProcess / SIGKILL,
// no shutdown hook, no chance to finish the transaction.
type Proc struct {
	Name string
	cmd  *exec.Cmd
	log  *os.File
	path string
	done chan struct{}
}

func (h *Harness) env() []string {
	return append(os.Environ(),
		"APP_ENV=dev",
		"DATABASE_URL="+h.DBURL,
		"TOKEN_PEPPER="+Pepper,
		"OBJSTORE=memory",
		"COOKIE_SECURE=false",
		"HTTP_ADDR="+h.APIAddr,
	)
}

func (h *Harness) start(name, bin, metricsAddr string) *Proc {
	h.T.Helper()
	logPath := filepath.Join(h.logDir, fmt.Sprintf("%s-%d.log", name, time.Now().UnixNano()))
	f, err := os.Create(logPath)
	if err != nil {
		h.T.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(h.env(), "METRICS_ADDR="+metricsAddr)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		h.T.Fatalf("start %s: %v", name, err)
	}
	p := &Proc{Name: name, cmd: cmd, log: f, path: logPath, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(p.done) }()
	h.T.Cleanup(func() { p.Kill() })
	// Ready when the metrics listener answers — both binaries bring it up
	// last, after the DB ping.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			h.T.Fatalf("%s exited during boot:\n%s", name, p.Tail())
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
	h.T.Fatalf("%s not ready after 20 s:\n%s", name, p.Tail())
	return nil
}

// StartAPI starts cmd/api on h.APIAddr.
func (h *Harness) StartAPI() *Proc { return h.start("api", h.apiBin, h.apiMetric) }

// StartWorker starts cmd/worker.
func (h *Harness) StartWorker() *Proc { return h.start("worker", h.workerBin, h.wrkMetric) }

// Metrics fetches the api's /metrics text; WorkerMetrics the worker's.
func (h *Harness) Metrics() string       { return scrape(h.apiMetric) }
func (h *Harness) WorkerMetrics() string { return scrape(h.wrkMetric) }

func scrape(addr string) string {
	res, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

// Metric reads one sample from an exposition text: the first line whose
// name and label set contain every given label pair, in any order. Returns
// ok=false when no such series exists. Histograms expose _count/_sum/_bucket.
func Metric(text, name string, labels ...string) (float64, bool) {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, name) {
			continue
		}
		rest := line[len(name):]
		if rest == "" || (rest[0] != '{' && rest[0] != ' ') {
			continue
		}
		match := true
		for _, l := range labels {
			if !strings.Contains(rest, l) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		fields := strings.Fields(rest)
		v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			continue
		}
		return v, true
	}
	return 0, false
}

// Log returns everything the process wrote so far.
func (p *Proc) Log() string {
	b, _ := os.ReadFile(p.path)
	return string(b)
}

// Kill terminates the process and waits for it to be gone.
func (p *Proc) Kill() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = p.cmd.Process.Kill()
	<-p.done
	p.log.Close()
}

// Alive reports whether the process is still running.
func (p *Proc) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Tail returns the last 30 lines of the process log.
func (p *Proc) Tail() string {
	b, err := os.ReadFile(p.path)
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

// Resp is one HTTP answer; Err is a transport error (no answer at all).
type Resp struct {
	Code    int
	Body    map[string]any
	Err     error
	Elapsed time.Duration
}

// Post sends JSON with the harness token.
func (h *Harness) Post(path string, body any) Resp { return h.PostAs(h.Token, path, body) }

// PostAs sends JSON with the given bearer token.
func (h *Harness) PostAs(token, path string, body any) Resp {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest("POST", "http://"+h.APIAddr+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	return h.Do(req)
}

// Do sends a prepared request and times it.
func (h *Harness) Do(req *http.Request) Resp {
	start := time.Now()
	res, err := h.HTTP.Do(req)
	if err != nil {
		return Resp{Err: err, Elapsed: time.Since(start)}
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := Resp{Code: res.StatusCode, Elapsed: time.Since(start)}
	_ = json.Unmarshal(raw, &out.Body)
	return out
}

// CreateRun opens a run with the harness token.
func (h *Harness) CreateRun(local string) Resp { return h.CreateRunAs(h.Token, local) }

// CreateRunAs opens a run with the given token, started now.
func (h *Harness) CreateRunAs(token, local string) Resp {
	return h.CreateRunAt(token, local, time.Now())
}

// CreateRunAt opens a run with the given token and start time — a dataset
// spread over months, not a million results that all happened today.
func (h *Harness) CreateRunAt(token, local string, startedAt time.Time) Resp {
	return h.PostAs(token, "/v1/runs", map[string]any{
		"localRunId": local, "environmentKey": "ci",
		"trigger":   map[string]any{"ref": "refs/heads/main", "commitSha": "deadbeef"},
		"startedAt": startedAt.UTC().Format(time.RFC3339),
	})
}

// Event is one finished test; the test's own id comes from file and title
// so a run of N events has N distinct tests unless titles repeat.
func Event(seq int, file, title string) map[string]any {
	return EventWith(seq, file, title, "passed", "expected")
}

// EventWith is Event with an explicit status/outcome.
func EventWith(seq int, file, title, status, outcome string) map[string]any {
	e := map[string]any{
		"sequence": seq, "type": "test.finished", "clientTestId": fmt.Sprintf("ct_%s_%s", strings.ReplaceAll(file, "/", "_"), title),
		"occurrenceIndex": 0, "attempt": 0,
		"identity": map[string]any{"framework": "playwright", "filePath": file, "titlePath": []string{"Suite", title}, "paramSignature": ""},
		"status":   status, "outcome": outcome, "durationMs": 5,
	}
	if status != "passed" && status != "skipped" {
		e["error"] = map[string]any{"message": "expected 200, got 500", "stack": "at " + file + ":1"}
	}
	return e
}

// LocalRun returns the n-th deterministic client run id.
func LocalRun(n int) string { return fmt.Sprintf("0198c3e1-7b1a-7c3d-9e4f-%012x", n) }

// DropConnections terminates every database connection of this harness's
// api and worker — §14.3 "restart kết nối DB" — and returns how many. Other
// tests' connections on the same database are left alone.
func (h *Harness) DropConnections() int {
	h.T.Helper()
	return h.Count(`SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE application_name = $1 AND pid <> pg_backend_pid()`, h.appName)
}

// ---- invariants by SQL ----

// Count runs a single-integer query.
func (h *Harness) Count(query string, args ...any) int {
	h.T.Helper()
	var n int
	if err := h.DB.Q(h.Ctx).QueryRow(h.Ctx, query, args...).Scan(&n); err != nil {
		h.T.Fatalf("%s: %v", query, err)
	}
	return n
}

// Float runs a single-float query (NULL → 0).
func (h *Harness) Float(query string, args ...any) float64 {
	h.T.Helper()
	var f *float64
	if err := h.DB.Q(h.Ctx).QueryRow(h.Ctx, query, args...).Scan(&f); err != nil {
		h.T.Fatalf("%s: %v", query, err)
	}
	if f == nil {
		return 0
	}
	return *f
}

// Exec runs a statement and fails the test on error.
func (h *Harness) Exec(query string, args ...any) {
	h.T.Helper()
	if _, err := h.DB.Q(h.Ctx).Exec(h.Ctx, query, args...); err != nil {
		h.T.Fatalf("%s: %v", query, err)
	}
}

// WaitFor polls cond until true or the deadline; on timeout it fails with why.
func (h *Harness) WaitFor(what string, d time.Duration, cond func() bool) {
	h.T.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	h.T.Fatalf("timed out after %s waiting for %s", d, what)
}

// JobsByStatus counts the jobs table by status.
func (h *Harness) JobsByStatus() map[string]int {
	rows, err := h.DB.Q(h.Ctx).Query(h.Ctx, `SELECT status, count(*) FROM jobs GROUP BY status`)
	if err != nil {
		h.T.Fatal(err)
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
