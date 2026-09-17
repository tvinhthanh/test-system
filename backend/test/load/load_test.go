// Package load measures the §14.2 targets on the real binaries: ingestion
// under 100 concurrent reporters, resolver latency, and the dashboard's
// common queries with 100k results in one project. Numbers are logged in
// full and the targets are asserted, so a regression fails the gate rather
// than quietly moving a number in a document.
//
// LOAD_SCALE=10 turns the dashboard dataset into 1M results (the top of the
// §14.2 range) for a manual run; the default 1 keeps `go test` at a minute.
package load

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/test/harness"
)

// The suite runs only with LOAD_TEST=1 (and TEST_DATABASE_URL): it takes
// minutes and its numbers only mean something on a quiet machine, so the
// ordinary `go test ./...` is not the place for it.
func TestMain(m *testing.M) {
	if os.Getenv("LOAD_TEST") != "1" {
		fmt.Println("test/load: set LOAD_TEST=1 to run the §14.2 measurements")
		os.Exit(0)
	}
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

// ---- latency bookkeeping ----

type sample struct {
	label string
	d     time.Duration
	ok    bool
}

type stats struct {
	mu      sync.Mutex
	samples map[string][]sample
}

func newStats() *stats { return &stats{samples: map[string][]sample{}} }

func (s *stats) add(label string, r harness.Resp, okCodes ...int) {
	ok := r.Err == nil
	if ok {
		ok = false
		for _, c := range okCodes {
			if r.Code == c {
				ok = true
			}
		}
	}
	s.mu.Lock()
	s.samples[label] = append(s.samples[label], sample{label, r.Elapsed, ok})
	s.mu.Unlock()
}

type summary struct {
	n, errors          int
	p50, p95, p99, max time.Duration
}

func (s *stats) summarize(label string) summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.samples[label]
	out := summary{n: len(list)}
	if len(list) == 0 {
		return out
	}
	ds := make([]time.Duration, 0, len(list))
	for _, x := range list {
		if !x.ok {
			out.errors++
		}
		ds = append(ds, x.d)
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	pct := func(p float64) time.Duration { return ds[min(len(ds)-1, int(float64(len(ds))*p))] }
	out.p50, out.p95, out.p99, out.max = pct(0.50), pct(0.95), pct(0.99), ds[len(ds)-1]
	return out
}

func (s *stats) labels() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for l := range s.samples {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func (s *stats) report(t *testing.T, title string) {
	t.Helper()
	t.Logf("%s", title)
	t.Logf("  %-42s %6s %6s %9s %9s %9s %9s", "request", "n", "errors", "p50", "p95", "p99", "max")
	for _, l := range s.labels() {
		x := s.summarize(l)
		t.Logf("  %-42s %6d %6d %9s %9s %9s %9s", l, x.n, x.errors, ms(x.p50), ms(x.p95), ms(x.p99), ms(x.max))
	}
}

func ms(d time.Duration) string { return fmt.Sprintf("%.0f ms", float64(d)/float64(time.Millisecond)) }

// ---- §14.2 row 1–2: ingestion under 100 concurrent reporters, resolver latency ----

func TestIngest100ConcurrentReporters(t *testing.T) {
	h := harness.New(t)
	h.StartAPI()
	worker := h.StartWorker()

	// 100 customers' pipelines, one project and token each: the realistic
	// mix, and the per-token rate limit (§7.4) is per pipeline anyway.
	const clients, suite = 100, 500
	type client struct {
		project tenancy.ProjectID
		token   string
	}
	cs := make([]client, clients)
	for i := range cs {
		p, tok := h.NewProject(fmt.Sprintf("p%03d", i))
		cs[i] = client{p, tok}
	}
	batch := func(size int) map[string]any {
		var events []any
		for k := 1; k <= size; k++ {
			events = append(events, harness.Event(k, "tests/suite.spec.ts", fmt.Sprintf("case %04d", k)))
		}
		return map[string]any{"events": events}
	}
	body := batch(suite)

	push := func(st *stats, c client, local string) {
		st.add("POST /v1/runs", h.CreateRunAs(c.token, local), 201)
		st.add("POST /v1/runs/{id}/events (500 events)", h.PostAs(c.token, "/v1/runs/"+local+"/events", body), 200)
		st.add("POST /v1/runs/{id}/finish", h.PostAs(c.token, "/v1/runs/"+local+"/finish", map[string]any{}), 200)
	}
	fanOut := func(st *stats, round int) time.Duration {
		var wg sync.WaitGroup
		start := time.Now()
		for i, c := range cs {
			wg.Add(1)
			go func(i int, c client) {
				defer wg.Done()
				push(st, c, harness.LocalRun(round*1000+i))
			}(i, c)
		}
		wg.Wait()
		return time.Since(start)
	}

	// Baseline first: one reporter alone, sequential — the isolated cost of a
	// 500-event batch on this machine, so the concurrent numbers below can
	// be read as queueing on top of it rather than as a mystery.
	solo := newStats()
	{
		p, tok := h.NewProject("solo")
		_ = p
		for i := 0; i < 10; i++ {
			local := harness.LocalRun(900000 + i)
			solo.add("POST /v1/runs", h.CreateRunAs(tok, local), 201)
			solo.add("POST /v1/runs/{id}/events (500 events)", h.PostAs(tok, "/v1/runs/"+local+"/events", body), 200)
			solo.add("POST /v1/runs/{id}/finish", h.PostAs(tok, "/v1/runs/"+local+"/finish", map[string]any{}), 200)
		}
		h.WaitFor("solo jobs to drain", time.Minute, func() bool {
			s := h.JobsByStatus()
			return s["pending"] == 0 && s["running"] == 0
		})
	}
	solo.report(t, "baseline: one reporter, sequential (first run creates the suite, the rest resolve inline)")

	// Warm-up: each project's first run creates its identities (tier 2,
	// unmeasured — a suite is only ever new once). Wait for the worker so
	// the measured round resolves inline.
	warm := newStats()
	took := fanOut(warm, 0)
	h.WaitFor("warm-up jobs to drain", 5*time.Minute, func() bool {
		s := h.JobsByStatus()
		return s["pending"] == 0 && s["running"] == 0
	})
	if n := h.Count(`SELECT count(*) FROM test_identities WHERE status = 'active'`); n != (clients+1)*suite {
		t.Fatalf("warm-up identities: %d, want %d", n, (clients+1)*suite)
	}
	warm.report(t, fmt.Sprintf("warm-up round (new suites, %d clients, wall %s)", clients, took.Round(time.Millisecond)))

	// Measured round: 100 reporters at once, 300 requests, 50k events.
	// LOAD_WORKER=0 measures ingest alone — what a deployment with the
	// worker on its own machine sees.
	if os.Getenv("LOAD_WORKER") == "0" {
		worker.Kill()
	}
	st := newStats()
	burstStart := time.Now().UTC()
	took = fanOut(st, 1)
	st.report(t, fmt.Sprintf("measured round (%d concurrent reporters, wall %s)", clients, took.Round(time.Millisecond)))

	total, errors := 0, 0
	for _, l := range st.labels() {
		x := st.summarize(l)
		total += x.n
		errors += x.errors
	}
	if rate := float64(errors) / float64(total); rate >= 0.01 {
		t.Errorf("error rate %.2f%% (%d/%d), target < 1%%", rate*100, errors, total)
	}
	if x := st.summarize("POST /v1/runs/{id}/events (500 events)"); x.p95 >= time.Second {
		t.Errorf("events p95 %s, target < 1 s (§14.2)", x.p95)
	}

	if !worker.Alive() {
		worker = h.StartWorker()
	}
	_ = worker

	// Resolver: clientTestId → stableTestId. Inline (tier 1) at ingest for
	// known suites, so the run is fully resolved at finish; the run.finished
	// job then confirms nothing is left. Its queue latency is the p50/p99.
	h.WaitFor("measured-round jobs to drain", 2*time.Minute, func() bool {
		s := h.JobsByStatus()
		return s["pending"] == 0 && s["running"] == 0
	})
	if n := h.Count(`SELECT count(*) FROM test_results WHERE stable_test_id IS NULL`); n != 0 {
		t.Errorf("%d results unresolved after the measured round", n)
	}
	if n := h.Count(`SELECT count(*) FROM jobs WHERE last_error IS NOT NULL OR status = 'dead'`); n != 0 {
		t.Errorf("%d jobs failed", n)
	}
	inline := h.Count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.created_at >= $1 AND t.stable_test_id IS NOT NULL`, burstStart)
	p50 := h.Float(`SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM finished_at - created_at)) FROM jobs WHERE created_at >= $1`, burstStart)
	p99 := h.Float(`SELECT percentile_cont(0.99) WITHIN GROUP (ORDER BY extract(epoch FROM finished_at - created_at)) FROM jobs WHERE created_at >= $1`, burstStart)
	t.Logf("resolver: %d/%d results resolved inline at ingest; run.finished job latency p50 %.3f s, p99 %.3f s", inline, clients*suite, p50, p99)
	if p50 >= 1 || p99 >= 5 {
		t.Errorf("resolver p50 %.3f s / p99 %.3f s, targets < 1 s / < 5 s (§14.2)", p50, p99)
	}
	if n := h.Count(`SELECT count(*) FROM test_identities WHERE status = 'active'`); n != (clients+1)*suite {
		t.Errorf("identities after the measured round: %d, want %d (a known suite must not create new ones)", n, (clients+1)*suite)
	}
}

// ---- §14.2 row 3: dashboard queries with 100k results in one project ----

func TestDashboardQueriesAt100kResults(t *testing.T) {
	h := harness.New(t)
	h.StartAPI()
	h.StartWorker()

	scale := 1
	if s, err := strconv.Atoi(os.Getenv("LOAD_SCALE")); err == nil && s > 0 {
		scale = s
	}
	// Pipelines scale with the dataset so each token stays under its §7.4
	// burst (120 requests): 100 runs × 4 requests over 5 tokens.
	const perRun = 1000
	runs, pipelines := 100*scale, 5*scale
	tokens := make([]string, pipelines)
	for i := range tokens {
		tokens[i] = h.IssueToken(h.Project, fmt.Sprintf("pipeline-%d", i))
	}

	// 100 runs × 1000 tests through the real ingest path, 5 pipelines in
	// parallel; every 10th case fails, so failures-first ordering, triage
	// and flaky signals have something to chew on.
	half := func(lo, hi int) map[string]any {
		var events []any
		for k := lo; k <= hi; k++ {
			status, outcome := "passed", "expected"
			if k%10 == 0 {
				status, outcome = "failed", "unexpected"
			}
			events = append(events, harness.EventWith(k, fmt.Sprintf("tests/area%02d.spec.ts", k%20), fmt.Sprintf("case %04d", k), status, outcome))
		}
		return map[string]any{"events": events}
	}
	first, second := half(1, perRun/2), half(perRun/2+1, perRun)
	ingest := newStats()
	work := make(chan int, runs)
	for n := 1; n <= runs; n++ {
		work <- n
	}
	close(work)
	var wg sync.WaitGroup
	start := time.Now()
	now := start
	for _, tok := range tokens {
		wg.Add(1)
		go func(tok string) {
			defer wg.Done()
			for n := range work {
				local := harness.LocalRun(n)
				// Six months of history, oldest first: the 30-day window the
				// metrics use holds about a sixth of it, as in a real project.
				started := now.Add(-time.Duration(runs-n) * (180 * 24 * time.Hour) / time.Duration(runs))
				ingest.add("create", h.CreateRunAt(tok, local, started), 201)
				ingest.add("events ×500", h.PostAs(tok, "/v1/runs/"+local+"/events", first), 200)
				ingest.add("events ×500", h.PostAs(tok, "/v1/runs/"+local+"/events", second), 200)
				ingest.add("finish", h.PostAs(tok, "/v1/runs/"+local+"/finish", map[string]any{}), 200)
			}
		}(tok)
	}
	wg.Wait()
	ingest.report(t, fmt.Sprintf("dataset ingest (%d runs × %d, %d pipelines, wall %s)", runs, perRun, pipelines, time.Since(start).Round(time.Millisecond)))
	for _, l := range ingest.labels() {
		if x := ingest.summarize(l); x.errors != 0 {
			t.Fatalf("dataset ingest %s: %d errors", l, x.errors)
		}
	}
	h.WaitFor("dataset jobs to drain", 10*time.Minute, func() bool {
		s := h.JobsByStatus()
		return s["pending"] == 0 && s["running"] == 0
	})
	// The planner needs statistics for a table that grew from 0 to 10^5–10^6
	// rows a minute ago; in production autovacuum would have run by now.
	h.Exec(`ANALYZE`)
	results := h.Count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.project_id = $1`, string(h.Project))
	if results != runs*perRun {
		t.Fatalf("dataset: %d results, want %d", results, runs*perRun)
	}
	t.Logf("dataset: %d results, %d identities, %d flaky signals", results,
		h.Count(`SELECT count(*) FROM test_identities WHERE project_id = $1`, string(h.Project)),
		h.Count(`SELECT count(*) FROM flaky_signals WHERE project_id = $1`, string(h.Project)))

	// A member logs in through the contract and times every read route.
	h.NewUser("ana@gate.example", "gate passphrase long enough", h.Project, authz.RoleMember)
	jar, _ := cookiejar.New(nil)
	web := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	const origin = "http://localhost:5173"
	get := func(path string) harness.Resp {
		req, _ := http.NewRequest("GET", "http://"+h.APIAddr+path, nil)
		req.Header.Set("Accept", "application/json")
		start := time.Now()
		res, err := web.Do(req)
		if err != nil {
			return harness.Resp{Err: err, Elapsed: time.Since(start)}
		}
		defer res.Body.Close()
		out := harness.Resp{Code: res.StatusCode, Elapsed: time.Since(start)}
		var body map[string]any
		if err := decode(res, &body); err == nil {
			out.Body = body
		}
		return out
	}
	boot := get("/v1/session")
	csrf, _ := boot.Body["csrfToken"].(string)
	{
		req, _ := http.NewRequest("POST", "http://"+h.APIAddr+"/v1/session", strings.NewReader(`{"email":"ana@gate.example","password":"gate passphrase long enough"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set(authz.CSRFHeader, csrf)
		res, err := web.Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("login: %v %v", err, res)
		}
		res.Body.Close()
	}
	base := "/v1/projects/" + string(h.Project)
	// Ids for the detail routes come from the list routes, as the UI does.
	runsPage := get(base + "/runs?limit=50")
	if runsPage.Code != 200 || len(items(runsPage)) == 0 {
		t.Fatalf("runs page: %d %v", runsPage.Code, runsPage.Body)
	}
	runID := items(runsPage)[0]["id"].(string)
	cursor, _ := runsPage.Body["nextCursor"].(string)
	resultsPage := get(base + "/runs/" + runID + "/results?limit=50")
	resultID := items(resultsPage)[0]["id"].(string)
	resultsCursor, _ := resultsPage.Body["nextCursor"].(string)
	idPage := get(base + "/identities?limit=50")
	identityID := items(idPage)[0]["id"].(string)
	idCursor, _ := idPage.Body["nextCursor"].(string)

	routes := []struct{ label, path string }{
		{"GET /v1/projects", "/v1/projects"},
		{"GET …/runs (page 1)", base + "/runs?limit=50"},
		{"GET …/runs (page 2, cursor)", base + "/runs?limit=50&cursor=" + cursor},
		{"GET …/runs?status=failed", base + "/runs?limit=50&status=failed"},
		{"GET …/runs?q=deadbeef", base + "/runs?limit=50&q=deadbeef"},
		{"GET …/runs/{id}", base + "/runs/" + runID},
		{"GET …/runs/{id}/results (page 1)", base + "/runs/" + runID + "/results?limit=50"},
		{"GET …/runs/{id}/results (page 2)", base + "/runs/" + runID + "/results?limit=50&cursor=" + resultsCursor},
		{"GET …/results/{id}", base + "/results/" + resultID},
		{"GET …/identities (page 1)", base + "/identities?limit=50"},
		{"GET …/identities (page 2)", base + "/identities?limit=50&cursor=" + idCursor},
		{"GET …/identities?view=flaky", base + "/identities?limit=50&view=flaky"},
		{"GET …/identities/{id}", base + "/identities/" + identityID},
		{"GET …/identities/{id}/runs", base + "/identities/" + identityID + "/runs?limit=50"},
		{"GET …/metrics/triage", base + "/metrics/triage"},
		{"GET …/metrics/trend", base + "/metrics/trend"},
	}
	const reps = 30
	dash := newStats()
	for rep := 0; rep < reps; rep++ {
		for _, r := range routes {
			dash.add(r.label, get(r.path), 200)
		}
	}
	dash.report(t, fmt.Sprintf("dashboard reads with %d results in the project (%d reps each, sequential)", results, reps))
	for _, r := range routes {
		x := dash.summarize(r.label)
		if x.errors != 0 {
			t.Errorf("%s: %d errors", r.label, x.errors)
		}
		if x.p95 >= 500*time.Millisecond {
			t.Errorf("%s: p95 %s, target < 500 ms (§14.2)", r.label, x.p95)
		}
	}
}

func items(r harness.Resp) []map[string]any {
	raw, _ := r.Body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func decode(res *http.Response, v any) error { return json.NewDecoder(res.Body).Decode(v) }
