// Package soak is gate T5 (§14.1): the real binaries under steady, mixed
// traffic for hours, sampled every few seconds, with the leak and bloat
// checks asserted at the end — memory, goroutines, connections, queue
// backlog, jobs table, latency drift. A 10-minute run proves the harness and
// catches fast leaks; the gate itself is SOAK_DURATION=24h..72h on a host
// that is not also the laptop.
//
//	SOAK_TEST=1 TEST_DATABASE_URL=… go test -count=1 -v -timeout 80h ./test/soak/
//
//	SOAK_DURATION      how long to drive traffic (default 10m)
//	SOAK_SAMPLE        sampling interval (default 30s)
//	SOAK_REPORTERS     concurrent CI pipelines, 2 per project (default 6)
//	SOAK_SUITE         tests per run (default 200)
//	SOAK_RUN_INTERVAL  one run per reporter per interval (default 8s; §7.4 allows 60 req/min/token)
//	SOAK_JOB_RETENTION JOB_RETENTION handed to the worker (default 2m, so the prune loop is observed)
//	SOAK_OBJSTORE      s3 to upload to a real bucket (OBJSTORE_* from the environment); default memory
//	SOAK_OUT           CSV of every sample (default: next to the test binary's temp dir, path logged)
//
// Traffic per run: create → 2 batches of events (every 25th test fails,
// every 40th is flaky: fails then passes on retry) → 2 artifacts through the
// presigned PUT, one in ten completed with a wrong checksum on purpose
// (must be a 422, not an error) → every 7th run replays its first batch
// (must dedupe) → finish. One signed-in browser walks the dashboard every
// 2 s and files a triage event every 30 s.
package soak

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
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

func TestMain(m *testing.M) {
	if os.Getenv("SOAK_TEST") != "1" {
		fmt.Println("test/soak: set SOAK_TEST=1 (and SOAK_DURATION) to run the T5 soak")
		os.Exit(0)
	}
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

func envDur(name string, def time.Duration) time.Duration {
	if v := os.Getenv(name); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// ---- traffic bookkeeping ----

// counters is what the generators report: totals for the whole run and a
// latency window the sampler drains every interval.
type counters struct {
	mu       sync.Mutex
	requests map[string]int
	errors   map[string]int
	unexpect []string // first few unexpected answers, for the log
	window   []time.Duration
	runs     int
	mismatch int // intentional wrong checksums, must all be 422
	replays  int
	triage   int
}

func newCounters() *counters { return &counters{requests: map[string]int{}, errors: map[string]int{}} }

// note records r under label; ok codes are the accepted answers. Events
// latencies go to the window so the sampler can report p95 per interval.
func (c *counters) note(label string, r harness.Resp, okCodes ...int) bool {
	ok := r.Err == nil
	if ok {
		ok = false
		for _, code := range okCodes {
			if r.Code == code {
				ok = true
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests[label]++
	if !ok {
		c.errors[label]++
		if len(c.unexpect) < 20 {
			c.unexpect = append(c.unexpect, fmt.Sprintf("%s: err=%v code=%d body=%v", label, r.Err, r.Code, r.Body))
		}
	}
	if label == "events" {
		c.window = append(c.window, r.Elapsed)
	}
	return ok
}

func (c *counters) drainWindow() (n int, p50, p95 time.Duration) {
	c.mu.Lock()
	w := c.window
	c.window = nil
	c.mu.Unlock()
	if len(w) == 0 {
		return 0, 0, 0
	}
	sort.Slice(w, func(i, j int) bool { return w[i] < w[j] })
	pct := func(p float64) time.Duration { return w[min(len(w)-1, int(float64(len(w))*p))] }
	return len(w), pct(0.5), pct(0.95)
}

func (c *counters) totals() (requests, errors int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.requests {
		requests += n
	}
	for _, n := range c.errors {
		errors += n
	}
	return
}

// ---- samples ----

// columns is the CSV header and the order every sample is written in.
var columns = []string{
	"t", "elapsed_s",
	"api_rss_mb", "api_heap_live_mb", "api_heap_live_adj_mb", "store_bytes_mb", "api_heap_mb", "api_goroutines", "api_pool_acquired", "api_pool_idle", "api_5xx_total",
	"worker_rss_mb", "worker_heap_live_mb", "worker_heap_mb", "worker_goroutines", "worker_pool_acquired", "worker_jobs_done", "worker_jobs_failed", "worker_heartbeat_age_s",
	"db_connections", "backlog", "backlog_oldest_s", "running_jobs", "dead_jobs", "jobs_rows", "jobs_done_rows", "jobs_dead_tuples", "jobs_table_mb",
	"runs", "runs_running", "runs_stuck", "results", "results_unresolved_late", "results_table_mb", "unlinked_artifacts", "unlinked_overdue", "review_pending", "runs_open_over_limit",
	"win_events_n", "win_events_p50_ms", "win_events_p95_ms", "requests_total", "errors_total",
}

type sample map[string]float64

func (s sample) row(at time.Time) []string {
	out := make([]string, len(columns))
	out[0] = at.UTC().Format(time.RFC3339)
	for i, c := range columns[1:] {
		out[i+1] = strconv.FormatFloat(s[c], 'f', -1, 64)
	}
	return out
}

// sumMetric adds every series of name whose label text satisfies pred.
func sumMetric(text, name string, pred func(labels string) bool) float64 {
	var total float64
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, name) {
			continue
		}
		rest := line[len(name):]
		if rest == "" || (rest[0] != '{' && rest[0] != ' ') {
			continue
		}
		labels := ""
		if rest[0] == '{' {
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				continue
			}
			labels = rest[1:end]
			rest = rest[end+1:]
		}
		if pred != nil && !pred(labels) {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
			total += v
		}
	}
	return total
}

func metricOr(text, name string, def float64, labels ...string) float64 {
	if v, ok := harness.Metric(text, name, labels...); ok {
		return v
	}
	return def
}

const mb = 1 << 20

func (s *soak) collect(started time.Time, c *counters) sample {
	h := s.h
	api, wrk := h.Metrics(), h.WorkerMetrics()
	out := sample{"elapsed_s": time.Since(started).Seconds()}

	out["api_rss_mb"] = metricOr(api, "process_resident_memory_bytes", 0) / mb
	out["api_heap_mb"] = metricOr(api, "go_memstats_heap_inuse_bytes", 0) / mb
	out["api_heap_live_mb"] = metricOr(api, "go_memstats_heap_live_bytes", 0) / mb
	out["api_goroutines"] = metricOr(api, "go_goroutines", 0)
	out["api_pool_acquired"] = metricOr(api, "db_pool_conns_acquired", 0)
	out["api_pool_idle"] = metricOr(api, "db_pool_conns_idle", 0)
	out["api_5xx_total"] = sumMetric(api, "http_requests_total", func(l string) bool { return strings.Contains(l, `status="5`) })
	out["backlog"] = metricOr(api, "queue_backlog_jobs", -1)
	out["backlog_oldest_s"] = metricOr(api, "queue_backlog_oldest_seconds", -1)
	out["running_jobs"] = metricOr(api, "queue_running_jobs", -1)
	out["dead_jobs"] = metricOr(api, "queue_dead_jobs", -1)
	out["review_pending"] = metricOr(api, "review_queue_pending", -1)
	out["runs_open_over_limit"] = metricOr(api, "runs_open_over_limit", -1)

	out["worker_rss_mb"] = metricOr(wrk, "process_resident_memory_bytes", 0) / mb
	out["worker_heap_mb"] = metricOr(wrk, "go_memstats_heap_inuse_bytes", 0) / mb
	out["worker_heap_live_mb"] = metricOr(wrk, "go_memstats_heap_live_bytes", 0) / mb
	out["worker_goroutines"] = metricOr(wrk, "go_goroutines", 0)
	out["worker_pool_acquired"] = metricOr(wrk, "db_pool_conns_acquired", 0)
	out["worker_jobs_done"] = sumMetric(wrk, "queue_jobs_total", func(l string) bool { return strings.Contains(l, `result="done"`) })
	out["worker_jobs_failed"] = sumMetric(wrk, "queue_jobs_total", func(l string) bool { return !strings.Contains(l, `result="done"`) })
	out["unlinked_overdue"] = metricOr(wrk, "artifacts_unlinked_overdue", -1)
	if ts, ok := harness.Metric(wrk, "queue_worker_last_poll_timestamp_seconds"); ok && ts > 0 {
		out["worker_heartbeat_age_s"] = math.Max(0, float64(time.Now().Unix())-ts)
	} else {
		out["worker_heartbeat_age_s"] = -1
	}

	out["db_connections"] = float64(h.Connections())
	out["jobs_rows"] = float64(h.Count(`SELECT count(*) FROM jobs`))
	out["jobs_done_rows"] = float64(h.Count(`SELECT count(*) FROM jobs WHERE status = 'done'`))
	out["jobs_dead_tuples"] = h.Float(`SELECT n_dead_tup::float8 FROM pg_stat_user_tables WHERE schemaname = current_schema() AND relname = 'jobs'`)
	out["jobs_table_mb"] = h.Float(`SELECT pg_total_relation_size('jobs')::float8`) / mb
	out["runs"] = float64(h.Count(`SELECT count(*) FROM runs`))
	out["runs_running"] = float64(h.Count(`SELECT count(*) FROM runs WHERE status = 'running'`))
	out["runs_stuck"] = float64(h.Count(`SELECT count(*) FROM runs WHERE status = 'running' AND created_at < now() - interval '5 minutes'`))
	out["results"] = float64(h.Count(`SELECT count(*) FROM test_results`))
	out["results_unresolved_late"] = float64(h.Count(`
		SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE t.stable_test_id IS NULL AND r.status <> 'running' AND r.finished_at < now() - interval '2 minutes'`))
	out["results_table_mb"] = h.Float(`SELECT pg_total_relation_size('test_results')::float8`) / mb
	out["unlinked_artifacts"] = float64(h.Count(`SELECT count(*) FROM artifacts WHERE test_result_attempt_id IS NULL AND status IN ('pending', 'uploaded', 'verified')`))
	// OBJSTORE=memory keeps every verified artifact in the api's heap by
	// design (dev store). The leak judgment is the live heap at the last GC
	// net of that payload: RSS and heap_inuse sawtooth with GC pacing
	// (target = 2× live) and grow with the store twice over, which is not a
	// leak. With OBJSTORE=s3 the adjustment is the same number minus zero.
	out["store_bytes_mb"] = h.Float(`SELECT coalesce(sum(actual_bytes), 0)::float8 FROM artifacts WHERE status = 'verified'`) / mb
	out["api_heap_live_adj_mb"] = out["api_heap_live_mb"] - out["store_bytes_mb"]

	n, p50, p95 := c.drainWindow()
	out["win_events_n"] = float64(n)
	out["win_events_p50_ms"] = float64(p50) / float64(time.Millisecond)
	out["win_events_p95_ms"] = float64(p95) / float64(time.Millisecond)
	req, errs := c.totals()
	out["requests_total"], out["errors_total"] = float64(req), float64(errs)
	return out
}

// ---- the test ----

type soak struct {
	h *harness.Harness
	t *testing.T
}

func TestSoakSteadyTraffic(t *testing.T) {
	duration := envDur("SOAK_DURATION", 10*time.Minute)
	every := envDur("SOAK_SAMPLE", 30*time.Second)
	reporters := envInt("SOAK_REPORTERS", 6)
	suite := envInt("SOAK_SUITE", 200)
	interval := envDur("SOAK_RUN_INTERVAL", 8*time.Second)
	retention := envDur("SOAK_JOB_RETENTION", 2*time.Minute)
	outPath := os.Getenv("SOAK_OUT")
	if outPath == "" {
		outPath = filepath.Join(os.TempDir(), fmt.Sprintf("atp-soak-%s.csv", time.Now().Format("20060102-150405")))
	}

	h := harness.New(t)
	h.ExtraEnv = []string{"JOB_RETENTION=" + retention.String()}
	// SOAK_OBJSTORE=s3 soaks against a real bucket (the OBJSTORE_* variables
	// pass through from this process's environment); default is the harness's
	// in-process memory store, whose payload the RSS trend subtracts.
	if store := os.Getenv("SOAK_OBJSTORE"); store != "" {
		h.ExtraEnv = append(h.ExtraEnv, "OBJSTORE="+store)
	}
	s := &soak{h: h, t: t}
	api := h.StartAPI()
	worker := h.StartWorker()
	t.Logf("soak: %s, sample every %s, %d reporters × %d tests every %s, JOB_RETENTION=%s, csv %s; api http://%s/ (dashboard login watcher@gate.example, password: soak passphrase long enough), api metrics+pprof http://%s/, worker http://%s/",
		duration, every, reporters, suite, interval, retention, outPath, h.APIAddr, h.MetricsAddr(), h.WorkerMetricsAddr())

	// Half as many projects as reporters: two pipelines share a suite, the
	// realistic shape, and tier-1 resolves inline once the suite is known.
	type pipeline struct {
		project string
		token   string
	}
	var pipes []pipeline
	for i := 0; i < (reporters+1)/2; i++ {
		p, tok := h.NewProject(fmt.Sprintf("soak%02d", i))
		pipes = append(pipes, pipeline{string(p), tok})
		if len(pipes) < reporters {
			pipes = append(pipes, pipeline{string(p), h.IssueToken(p, "second-pipeline")})
		}
	}
	pipes = pipes[:reporters]
	// The browser watches the first soak project, not the harness default.
	first := pipes[0].project
	h.NewUser("watcher@gate.example", "soak passphrase long enough", tenancy.ProjectID(first), authz.RoleMember)

	c := newCounters()
	ctx, stop := context.WithTimeout(context.Background(), duration)
	defer stop()
	var wg sync.WaitGroup

	// Reporters.
	for i, p := range pipes {
		wg.Add(1)
		go func(i int, p pipeline) {
			defer wg.Done()
			s.reporter(ctx, c, i, p.token, suite, interval)
		}(i, p)
	}
	// Browser.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.browser(ctx, c, first)
	}()

	// Sampler: rows every interval until the traffic stops, then one more
	// after the drain.
	var rows []sample
	var times []time.Time
	started := time.Now()
	log := func(x sample) {
		t.Logf("%5.0fs api rss %4.0f live %4.0f (net %4.0f) MB/%3.0f gor  wrk rss %3.0f live %3.0f MB/%3.0f gor  conns %2.0f  backlog %3.0f (oldest %3.0fs)  jobs %5.0f rows (%4.0f done, %3.1f MB)  runs %5.0f results %7.0f  events p50 %4.0f p95 %4.0f ms (n=%3.0f)  errors %.0f/%.0f  5xx %.0f",
			x["elapsed_s"], x["api_rss_mb"], x["api_heap_live_mb"], x["api_heap_live_adj_mb"], x["api_goroutines"], x["worker_rss_mb"], x["worker_heap_live_mb"], x["worker_goroutines"], x["db_connections"],
			x["backlog"], x["backlog_oldest_s"], x["jobs_rows"], x["jobs_done_rows"], x["jobs_table_mb"], x["runs"], x["results"],
			x["win_events_p50_ms"], x["win_events_p95_ms"], x["win_events_n"], x["errors_total"], x["requests_total"], x["api_5xx_total"])
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-tick.C:
			if !api.Alive() || !worker.Alive() {
				t.Fatalf("a process died during the soak: api alive=%v worker alive=%v\napi:\n%s\nworker:\n%s", api.Alive(), worker.Alive(), api.Tail(), worker.Tail())
			}
			x := s.collect(started, c)
			rows, times = append(rows, x), append(times, time.Now())
			log(x)
		}
	}
	wg.Wait()
	t.Logf("traffic stopped after %s: %d runs, %d intentional checksum mismatches, %d replays, %d triage events", time.Since(started).Round(time.Second), c.runs, c.mismatch, c.replays, c.triage)

	// Drain: everything the reporters left behind must finish on its own.
	h.WaitFor("queue to drain after traffic stopped", 3*time.Minute, func() bool {
		st := h.JobsByStatus()
		return st["pending"] == 0 && st["running"] == 0
	})
	final := s.collect(started, c)
	rows, times = append(rows, final), append(times, time.Now())
	log(final)
	writeCSV(t, outPath, rows, times)

	// ---- invariants at the end ----
	req, errs := c.totals()
	if rate := float64(errs) / math.Max(1, float64(req)); rate >= 0.01 {
		t.Errorf("error rate %.3f%% (%d/%d), target < 1%%; first unexpected answers:\n  %s", rate*100, errs, req, strings.Join(c.unexpect, "\n  "))
	} else if errs > 0 {
		t.Logf("note: %d/%d unexpected answers (< 1%%):\n  %s", errs, req, strings.Join(c.unexpect, "\n  "))
	}
	if final["api_5xx_total"] != 0 {
		t.Errorf("%.0f 5xx answers under healthy conditions — see api log:\n%s", final["api_5xx_total"], api.Tail())
	}
	if final["dead_jobs"] != 0 || final["worker_jobs_failed"] != 0 {
		t.Errorf("dead jobs %.0f, failed attempts %.0f: %v", final["dead_jobs"], final["worker_jobs_failed"], h.Count(`SELECT count(*) FROM jobs WHERE last_error IS NOT NULL`))
	}
	if n := h.Count(`SELECT count(*) FROM jobs WHERE last_error IS NOT NULL`); n != 0 {
		t.Errorf("%d jobs needed a retry (last_error set)", n)
	}
	if final["runs_running"] != 0 || final["runs_stuck"] != 0 {
		t.Errorf("runs still running after every reporter finished: %.0f (stuck > 5 min: %.0f)", final["runs_running"], final["runs_stuck"])
	}
	if final["results_unresolved_late"] != 0 {
		t.Errorf("%.0f results of finished runs never got a stable id", final["results_unresolved_late"])
	}
	if final["unlinked_overdue"] != 0 {
		t.Errorf("artifacts unlinked for over an hour: %.0f", final["unlinked_overdue"])
	}
	if final["worker_heartbeat_age_s"] < 0 || final["worker_heartbeat_age_s"] > 10 {
		t.Errorf("worker heartbeat age %.0fs", final["worker_heartbeat_age_s"])
	}
	if c.mismatch == 0 {
		t.Errorf("no intentional checksum mismatch was sent — the generator is broken")
	}
	if want := c.runs; int(final["runs"]) != want {
		t.Errorf("runs in the database %.0f, reporters created %d", final["runs"], want)
	}

	// ---- trends: first third vs last third of the samples after warm-up ----
	// Warm-up is the first 20 % or the first two minutes, whichever is longer;
	// with fewer than 6 samples left the trends are reported, not judged.
	warm := len(rows) / 5
	for warm < len(rows) && rows[warm]["elapsed_s"] < 120 {
		warm++
	}
	post := rows[warm:]
	if len(post) < 6 {
		t.Logf("only %d samples after warm-up — trend checks skipped; run longer (SOAK_DURATION) or sample more often (SOAK_SAMPLE)", len(post))
		return
	}
	third := len(post) / 3
	head, tail := post[:third], post[len(post)-third:]
	med := func(rs []sample, key string) float64 {
		vs := make([]float64, len(rs))
		for i, r := range rs {
			vs[i] = r[key]
		}
		sort.Float64s(vs)
		return vs[len(vs)/2]
	}
	maxOf := func(rs []sample, key string) float64 {
		m := math.Inf(-1)
		for _, r := range rs {
			m = math.Max(m, r[key])
		}
		return m
	}
	trend := func(key string, allowRatio, allowAbs float64, unit string) {
		a, b := med(head, key), med(tail, key)
		limit := a*allowRatio + allowAbs
		verdict := "ok"
		if b > limit {
			verdict = "GROWING"
			t.Errorf("%s: median first third %.1f → last third %.1f %s (limit %.1f = ×%.2f + %.0f)", key, a, b, unit, limit, allowRatio, allowAbs)
		}
		t.Logf("trend %-22s %8.1f → %8.1f %s  %s", key, a, b, unit, verdict)
	}
	// Leak detectors: live heap after GC, net of the dev store's payload.
	trend("api_heap_live_adj_mb", 1.30, 8, "MB")
	trend("worker_heap_live_mb", 1.30, 8, "MB")
	// RSS is reported, and bounded by GC pacing rather than trended: with
	// GOGC=100 a healthy process sits under ~2× live plus runtime overhead.
	for _, p := range []struct{ rss, live string }{{"api_rss_mb", "api_heap_live_mb"}, {"worker_rss_mb", "worker_heap_live_mb"}} {
		rss, live := med(tail, p.rss), med(tail, p.live)
		if limit := 3*live + 48; rss > limit {
			t.Errorf("%s %.0f MB against live heap %.0f MB (limit %.0f): memory outside the Go heap is growing", p.rss, rss, live, limit)
		} else {
			t.Logf("pacing %-22s %8.0f MB rss vs %6.0f MB live  ok (limit %.0f)", p.rss, rss, live, limit)
		}
	}
	trend("api_goroutines", 1.0, 20, "goroutines")
	trend("worker_goroutines", 1.0, 20, "goroutines")
	trend("db_connections", 1.0, 4, "connections")
	trend("api_pool_acquired", 1.0, 2, "conns")
	trend("worker_pool_acquired", 1.0, 2, "conns")
	// The backlog must not creep: the last third's median stays within the
	// first third's worst sample plus a little.
	if a, b := maxOf(head, "backlog"), med(tail, "backlog"); b > a+5 {
		t.Errorf("queue backlog creeping: first-third max %.0f, last-third median %.0f", a, b)
	} else {
		t.Logf("trend %-22s %8.0f → %8.0f jobs  ok (first-third max vs last-third median)", "backlog", a, b)
	}
	// Prune: with JOB_RETENTION short, the jobs table must plateau — its
	// row count in the last third stays below runs created in the retention
	// window plus the sweep's slack, instead of tracking total runs.
	perRun := 1.0 // one run.finished job per run
	plateau := perRun*float64(reporters)*((retention+time.Minute).Seconds()/interval.Seconds()) + 50
	if b := med(tail, "jobs_rows"); b > plateau {
		t.Errorf("jobs table not pruned: %.0f rows in the last third, expected ≤ %.0f with JOB_RETENTION=%s (runs so far %.0f)", b, plateau, retention, final["runs"])
	} else {
		t.Logf("trend %-22s %8.0f → %8.0f rows  ok (plateau ≤ %.0f, %.0f runs total)", "jobs_rows", med(head, "jobs_rows"), b, plateau, final["runs"])
	}
	// Latency drift: events p95 in the last third ≤ 2× the first third + 100 ms.
	if a, b := med(head, "win_events_p95_ms"), med(tail, "win_events_p95_ms"); a > 0 && b > 2*a+100 {
		t.Errorf("events p95 drifting: %.0f ms → %.0f ms", a, b)
	} else {
		t.Logf("trend %-22s %8.0f → %8.0f ms  ok", "win_events_p95_ms", a, b)
	}
}

// ---- generators ----

// reporter is one CI pipeline: a run every interval until ctx ends.
func (s *soak) reporter(ctx context.Context, c *counters, idx int, token string, suite int, interval time.Duration) {
	h := s.h
	half := suite / 2
	batch := func(lo, hi int) map[string]any {
		var events []any
		seq := 0
		for k := lo; k <= hi; k++ {
			seq++
			status, outcome := "passed", "expected"
			switch {
			case k%40 == 0:
				// Flaky: attempt 0 fails, attempt 1 passes.
				e := harness.EventWith(seq, fmt.Sprintf("tests/area%02d.spec.ts", k%10), fmt.Sprintf("case %04d", k), "failed", "unexpected")
				events = append(events, e)
				seq++
				r := harness.EventWith(seq, fmt.Sprintf("tests/area%02d.spec.ts", k%10), fmt.Sprintf("case %04d", k), "passed", "flaky")
				r["attempt"] = 1
				events = append(events, r)
				continue
			case k%25 == 0:
				status, outcome = "failed", "unexpected"
			}
			events = append(events, harness.EventWith(seq, fmt.Sprintf("tests/area%02d.spec.ts", k%10), fmt.Sprintf("case %04d", k), status, outcome))
		}
		return map[string]any{"events": events}
	}
	first, second := batch(1, half), batch(half+1, suite)

	for iter := 0; ctx.Err() == nil; iter++ {
		start := time.Now()
		local := harness.LocalRun(idx*1_000_000 + iter)
		if !c.note("create", h.CreateRunAs(token, local), 201) {
			s.pause(ctx, start, interval)
			continue
		}
		c.note("events", h.PostAs(token, "/v1/runs/"+local+"/events", first), 200)
		c.note("events", h.PostAs(token, "/v1/runs/"+local+"/events", second), 200)
		if iter%7 == 0 {
			// The reporter "lost the ack" and sends the batch again (§7.1).
			r := h.PostAs(token, "/v1/runs/"+local+"/events", first)
			if c.note("replay", r, 200) {
				if d, _ := r.Body["duplicates"].(float64); int(d) != len(first["events"].([]any)) {
					c.note("replay-dedupe", harness.Resp{Code: 599, Body: r.Body}, 200)
				}
			}
			c.mu.Lock()
			c.replays++
			c.mu.Unlock()
		}
		s.artifact(c, token, local, fmt.Sprintf("trace-%d.txt", iter), false)
		s.artifact(c, token, local, fmt.Sprintf("log-%d.txt", iter), iter%10 == 0)
		c.note("finish", h.PostAs(token, "/v1/runs/"+local+"/finish", map[string]any{}), 200)
		c.mu.Lock()
		c.runs++
		c.mu.Unlock()
		s.pause(ctx, start, interval)
	}
}

func (s *soak) pause(ctx context.Context, start time.Time, interval time.Duration) {
	rest := interval - time.Since(start)
	if rest <= 0 {
		return
	}
	select {
	case <-ctx.Done():
	case <-time.After(rest):
	}
}

// artifact runs the §7.7 three-step upload on the run's first test. corrupt
// flips one bit of the uploaded bytes: the server hashes what landed,
// answers 422 CHECKSUM_MISMATCH and keeps the run valid (§14.2 artifact
// row). Claiming a different hash at complete would be a 400 by contract
// and would not exercise the verification.
func (s *soak) artifact(c *counters, token, local, name string, corrupt bool) {
	data := make([]byte, 4096+len(name)*7)
	_, _ = rand.Read(data)
	declare, put, complete := s.h.Artifact(token, local, name, data, corrupt)
	if !c.note("artifact-declare", declare, 201) {
		return
	}
	if !c.note("artifact-put", put, 200) {
		return
	}
	if corrupt {
		c.mu.Lock()
		c.mismatch++
		c.mu.Unlock()
		if c.note("artifact-complete-mismatch", complete, 422) {
			if e, _ := complete.Body["error"].(map[string]any); e["code"] != "CHECKSUM_MISMATCH" {
				c.note("artifact-mismatch-code", harness.Resp{Code: 599, Body: complete.Body}, 200)
			}
		}
		return
	}
	c.note("artifact-complete", complete, 200)
}

// browser is one signed-in user refreshing the dashboard every 2 s and
// triaging a failure every 30 s.
func (s *soak) browser(ctx context.Context, c *counters, project string) {
	h := s.h
	b := h.Login("watcher@gate.example", "soak passphrase long enough")
	base := "/v1/projects/" + project
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for iter := 0; ; iter++ {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		c.note("dash", b.Get("/v1/projects"), 200)
		runs := b.Get(base + "/runs?limit=20")
		if !c.note("dash", runs, 200) || len(runs.Items()) == 0 {
			continue
		}
		runID, _ := runs.Items()[0]["id"].(string)
		c.note("dash", b.Get(base+"/runs/"+runID), 200)
		results := b.Get(base + "/runs/" + runID + "/results?limit=50")
		c.note("dash", results, 200)
		if items := results.Items(); len(items) > 0 {
			resultID, _ := items[0]["id"].(string)
			c.note("dash", b.Get(base+"/results/"+resultID), 200)
			if iter%15 == 0 {
				c.note("triage", b.Post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}}), 201, 200)
				c.mu.Lock()
				c.triage++
				c.mu.Unlock()
			}
		}
		c.note("dash", b.Get(base+"/identities?limit=20"), 200)
		c.note("dash", b.Get(base+"/identities?limit=20&view=flaky"), 200)
		c.note("dash", b.Get(base+"/metrics/triage"), 200)
		c.note("dash", b.Get(base+"/metrics/trend"), 200)
	}
}

func writeCSV(t *testing.T, path string, rows []sample, times []time.Time) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Logf("csv: %v", err)
		return
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(columns)
	for i, r := range rows {
		_ = w.Write(r.row(times[i]))
	}
	w.Flush()
	t.Logf("samples written to %s", path)
}
