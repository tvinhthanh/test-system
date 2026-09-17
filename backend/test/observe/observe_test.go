// Package observe is T3 (§14.1): when something is broken, the numbers on
// /metrics — the ones deploy/prometheus/alerts.yml watches — must say so,
// and the logs must never say a secret. Real binaries, real Postgres, the
// same failures as test/failure, but this time the assertion is on what an
// operator would see.
package observe

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/test/harness"
)

func TestMain(m *testing.M) {
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

func metric(t *testing.T, text, name string, labels ...string) float64 {
	t.Helper()
	v, ok := harness.Metric(text, name, labels...)
	if !ok {
		t.Fatalf("metric %s%v not exposed", name, labels)
	}
	return v
}

// The worker dying, the queue backing up, the worker coming back and the
// backlog draining — each state readable from the api's scrape, which is
// the one still answering when the worker is gone.
func TestBacklogIsVisibleFromTheAPIWhenTheWorkerIsDead(t *testing.T) {
	h := harness.New(t)
	h.StartAPI()
	w := h.StartWorker()

	if v := metric(t, h.Metrics(), "queue_backlog_jobs"); v != 0 {
		t.Fatalf("backlog before any work: %v", v)
	}
	w.Kill()
	const runs = 5
	for n := 1; n <= runs; n++ {
		local := harness.LocalRun(n)
		if r := h.CreateRun(local); r.Code != 201 {
			t.Fatalf("create: %d %v", r.Code, r.Body)
		}
		if r := h.Post("/v1/runs/"+local+"/events", map[string]any{"events": []any{harness.Event(1, "tests/o.spec.ts", "one")}}); r.Code != 200 {
			t.Fatalf("events: %d %v", r.Code, r.Body)
		}
		if r := h.Post("/v1/runs/"+local+"/finish", map[string]any{}); r.Code != 200 {
			t.Fatalf("finish: %d %v", r.Code, r.Body)
		}
	}
	time.Sleep(1500 * time.Millisecond)
	text := h.Metrics()
	if v := metric(t, text, "queue_backlog_jobs"); v != runs {
		t.Errorf("queue_backlog_jobs = %v with the worker dead, want %d", v, runs)
	}
	if v := metric(t, text, "queue_backlog_oldest_seconds"); v < 1 {
		t.Errorf("queue_backlog_oldest_seconds = %v, want the age of the first finish (≥ 1)", v)
	}
	if v := metric(t, text, "queue_dead_jobs"); v != 0 {
		t.Errorf("queue_dead_jobs = %v", v)
	}

	// Worker back: backlog drains, and the worker's own numbers appear —
	// enqueue-to-done latency (the resolver latency alert reads it) and a
	// fresh heartbeat.
	w = h.StartWorker()
	h.WaitFor("backlog to drain", 30*time.Second, func() bool {
		v, ok := harness.Metric(h.Metrics(), "queue_backlog_jobs")
		return ok && v == 0
	})
	wt := h.WorkerMetrics()
	if v := metric(t, wt, "queue_jobs_total", `kind="run.finished"`, `result="done"`); v != runs {
		t.Errorf("worker queue_jobs_total done = %v, want %d", v, runs)
	}
	if v := metric(t, wt, "queue_job_latency_seconds_count", `kind="run.finished"`); v != runs {
		t.Errorf("queue_job_latency_seconds_count = %v, want %d", v, runs)
	}
	if v := metric(t, wt, "queue_job_latency_seconds_sum", `kind="run.finished"`); v < 1 {
		t.Errorf("queue_job_latency_seconds_sum = %v: jobs waited ≥ 1.5 s for a worker, the histogram should show it", v)
	}
	if v := metric(t, wt, "queue_worker_last_poll_timestamp_seconds", `kind="run.finished"`); time.Since(time.Unix(int64(v), 0)) > 10*time.Second {
		t.Errorf("worker heartbeat is %v old", time.Since(time.Unix(int64(v), 0)))
	}
	if !w.Alive() {
		t.Error("worker died")
	}
}

// The api's own failures show as 5xx on both the route counter (infra
// alert) and the per-project ingest counter (§6.4 ingest error rate); a
// client's mistakes show as 4xx and never feed the 5xx rate.
func TestErrorsAreCountedWhereTheAlertsLook(t *testing.T) {
	h := harness.New(t)
	api := h.StartAPI()

	local := harness.LocalRun(1)
	if r := h.CreateRun(local); r.Code != 201 {
		t.Fatalf("create: %d %v", r.Code, r.Body)
	}
	// Client errors: a bad token, an oversized batch.
	if r := h.PostAs("atp_ing_not_a_real_token", "/v1/runs", map[string]any{}); r.Code != 401 {
		t.Fatalf("bad token: %d %v", r.Code, r.Body)
	}
	var big []any
	for i := 1; i <= 501; i++ {
		big = append(big, harness.Event(i, "tests/o.spec.ts", fmt.Sprintf("t%d", i)))
	}
	if r := h.Post("/v1/runs/"+local+"/events", map[string]any{"events": big}); r.Code != 413 {
		t.Fatalf("oversized batch: %d %v", r.Code, r.Body)
	}
	// Our error: every database connection torn down under the api; the
	// next request answers 503 (the reporter retries).
	h.DropConnections()
	var got int
	for i := 0; i < 10 && got == 0; i++ {
		if r := h.Post("/v1/runs/"+local+"/events", map[string]any{"events": []any{harness.Event(1, "tests/o.spec.ts", "one")}}); r.Code >= 500 {
			got = r.Code
		}
	}
	if got == 0 {
		t.Skip("no request hit a dead connection this time — the pool healed before the first use; nothing to observe")
	}

	text := h.Metrics()
	if v := metric(t, text, "ingest_requests_total", `endpoint="events"`, `status="5xx"`); v < 1 {
		t.Errorf("ingest_requests_total{events,5xx} = %v after a %d", v, got)
	}
	if v := metric(t, text, "http_requests_total", `route="POST /v1/runs/{localRunId}/events"`, fmt.Sprintf(`status="%d"`, got)); v < 1 {
		t.Errorf("http_requests_total for the %d not counted on the route", got)
	}
	if v := metric(t, text, "http_requests_total", `route="POST /v1/runs"`, `status="401"`); v != 1 {
		t.Errorf("the bad token should be one 401 on POST /v1/runs, got %v", v)
	}
	if v := metric(t, text, "ingest_requests_total", `endpoint="events"`, `status="4xx"`); v != 1 {
		t.Errorf("the oversized batch should be one 4xx on events, got %v", v)
	}
	if _, ok := harness.Metric(text, "ingest_requests_total", `endpoint="runs"`, `status="5xx"`); ok {
		t.Error("a bad token must not count as a 5xx on runs")
	}
	if !api.Alive() {
		t.Error("api died")
	}
}

// A run nobody finishes and an artifact nobody claims: both invisible to a
// reporter, both must be visible to the operator — before the sweep as a
// gauge the api computes, after the sweep as the worker's counters.
func TestStuckRunsAndOrphanArtifactsSurface(t *testing.T) {
	h := harness.New(t)
	h.StartAPI()

	// A run that started seven hours ago and never finished.
	stuck := harness.LocalRun(1)
	if r := h.CreateRunAt(h.Token, stuck, time.Now().Add(-7*time.Hour)); r.Code != 201 {
		t.Fatalf("create: %d %v", r.Code, r.Body)
	}
	// An artifact declared, uploaded and verified for a test whose event
	// never arrives (the reporter crashed between upload and batch).
	orphan := harness.LocalRun(2)
	if r := h.CreateRun(orphan); r.Code != 201 {
		t.Fatalf("create: %d %v", r.Code, r.Body)
	}
	body := []byte("PNG not really")
	sum := sha256.Sum256(body)
	decl := h.Post("/v1/artifacts", map[string]any{
		"localRunId": orphan, "clientTestId": "ct_never", "occurrenceIndex": 0, "attempt": 0,
		"name": "shot.png", "kind": "screenshot", "contentType": "image/png", "sizeBytes": len(body), "sha256": hex.EncodeToString(sum[:]),
	})
	if decl.Code != 201 {
		t.Fatalf("declare: %d %v", decl.Code, decl.Body)
	}
	upload, _ := decl.Body["upload"].(map[string]any)
	req, _ := http.NewRequest("PUT", upload["url"].(string), bytes.NewReader(body))
	for k, v := range upload["headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode/100 != 2 {
		t.Fatalf("presigned PUT: %v %v", err, res)
	}
	if r := h.Post("/v1/artifacts/"+decl.Body["artifactId"].(string)+"/complete", map[string]any{"sha256": hex.EncodeToString(sum[:])}); r.Code != 200 {
		t.Fatalf("complete: %d %v", r.Code, r.Body)
	}
	if r := h.Post("/v1/runs/"+orphan+"/finish", map[string]any{}); r.Code != 200 {
		t.Fatalf("finish: %d %v", r.Code, r.Body)
	}
	// Two hours pass for the artifact.
	h.Exec(`UPDATE artifacts SET created_at = created_at - interval '2 hours'`)

	// Before any sweep: the api sees the stuck run.
	if v := metric(t, h.Metrics(), "runs_open_over_limit"); v != 1 {
		t.Errorf("runs_open_over_limit = %v, want 1", v)
	}
	// The worker sweeps on boot: the run is closed and counted, the
	// artifact is counted as unlinked past the hour.
	h.StartWorker()
	h.WaitFor("the sweep to close the run", 20*time.Second, func() bool {
		v, ok := harness.Metric(h.Metrics(), "runs_open_over_limit")
		return ok && v == 0
	})
	wt := h.WorkerMetrics()
	if v := metric(t, wt, "runs_autoclosed_total", `project="`+string(h.Project)+`"`); v != 1 {
		t.Errorf("runs_autoclosed_total = %v, want 1", v)
	}
	// The artifact sweep runs after the run sweep in the same boot pass.
	h.WaitFor("the artifact sweep to count the orphan", 20*time.Second, func() bool {
		v, ok := harness.Metric(h.WorkerMetrics(), "artifacts_unlinked_overdue")
		return ok && v == 1
	})
	if n := h.Count(`SELECT count(*) FROM runs WHERE status = 'error' AND error_reason = 'RUN_TIMED_OUT'`); n != 1 {
		t.Errorf("auto-closed runs in the database: %d", n)
	}
	if v := metric(t, h.Metrics(), "review_queue_pending"); v != 0 {
		t.Errorf("review_queue_pending = %v", v)
	}
}

// §11.5: no token, no password, no session secret in any log line — and
// every error line the api writes carries the request id (§6.4).
func TestLogsCarryCorrelationIDsAndNoSecrets(t *testing.T) {
	h := harness.New(t)
	api := h.StartAPI()
	w := h.StartWorker()

	const password = "correct horse battery staple"
	h.NewUser("ops@gate.example", password, h.Project, authz.RoleMember)
	// Traffic of every kind: a good ingest, a bad token, a login (good and
	// bad), a 5xx from a torn-down database, a job in the worker.
	local := harness.LocalRun(1)
	h.CreateRun(local)
	h.Post("/v1/runs/"+local+"/events", map[string]any{"events": []any{harness.Event(1, "tests/o.spec.ts", "one")}})
	h.PostAs("atp_ing_"+strings.Repeat("f", 40), "/v1/runs", map[string]any{})
	jar := &cookieClient{h: h}
	jar.get("/v1/session")
	jar.login("ops@gate.example", "wrong password entirely")
	jar.login("ops@gate.example", password)
	h.DropConnections()
	for i := 0; i < 5; i++ {
		h.Post("/v1/runs/"+local+"/finish", map[string]any{})
	}
	h.WaitFor("the job to run", 20*time.Second, func() bool { return h.JobsByStatus()["done"] >= 1 })
	time.Sleep(300 * time.Millisecond)

	for _, p := range []*harness.Proc{api, w} {
		log := p.Log()
		for _, secret := range []string{h.Token, password, "atp_sess_"} {
			if strings.Contains(log, secret) {
				t.Errorf("%s log contains a secret (%s…)", p.Name, secret[:8])
			}
		}
		if strings.Contains(log, strings.Repeat("f", 40)) {
			t.Errorf("%s log contains the rejected bearer token", p.Name)
		}
	}
	apiLog := api.Log()
	errorLines, withID := 0, 0
	for _, line := range strings.Split(apiLog, "\n") {
		if !strings.Contains(line, "level=ERROR") && !strings.Contains(line, "level=WARN") {
			continue
		}
		if strings.Contains(line, "boot") || strings.Contains(line, "health gauge") {
			continue // process-level, no request
		}
		errorLines++
		if strings.Contains(line, "request_id=") {
			withID++
		}
	}
	if errorLines == 0 {
		t.Error("expected at least one request-level ERROR/WARN line from the torn-down database")
	}
	if withID != errorLines {
		t.Errorf("%d of %d request-level error lines carry request_id", withID, errorLines)
	}
	t.Logf("api log: %d request-level error/warn lines, all with request_id: %v", errorLines, withID == errorLines)
}

// cookieClient is the minimum of a browser: a jar, the bootstrap token.
type cookieClient struct {
	h    *harness.Harness
	http http.Client
	csrf string
}

func (c *cookieClient) get(path string) {
	if c.http.Jar == nil {
		c.http.Jar, _ = newJar()
	}
	res, err := c.http.Get("http://" + c.h.APIAddr + path)
	if err != nil {
		return
	}
	defer res.Body.Close()
	for _, ck := range res.Cookies() {
		if ck.Name == authz.CSRFCookie {
			c.csrf = ck.Value
		}
	}
}

func (c *cookieClient) login(email, password string) {
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req, _ := http.NewRequest("POST", "http://"+c.h.APIAddr+"/v1/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set(authz.CSRFHeader, c.csrf)
	if res, err := c.http.Do(req); err == nil {
		res.Body.Close()
	}
}

func newJar() (http.CookieJar, error) { return cookiejar.New(nil) }
