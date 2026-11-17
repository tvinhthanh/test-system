package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

var pepper = []byte("test-pepper-test-pepper-test-pepper!")

type env struct {
	srv      *httptest.Server
	reg      *metric.Registry
	tokenA   string // project A ingest
	tokenAID string // its id, the metric label
	tokenB   string // project B ingest
	tokenOrg string // org-scoped ingest
	tokenRO  string // project A read-only
	results  results.Service
	projectA string
	q        *queue.Queue
	jobs     *ingest.Jobs
	worker   *queue.Worker
	flaky    flaky.Service
	db       *pg.DB
}

// drain runs every queued job — what cmd/worker does continuously.
func (e *env) drain(t *testing.T) int {
	t.Helper()
	n, err := e.worker.Drain(context.Background())
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	return n
}

func setup(t *testing.T) *env {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	ten := tenancy.New(db)
	az := authz.New(db, pepper)
	reg := metric.New()

	org, err := ten.CreateOrganization(ctx, "Acme", "acme")
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	pa, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	pb, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	var lastID string
	issue := func(level authz.Level, id string, perm authz.Permission) string {
		is, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: level, ID: id}, Permissions: []authz.Permission{perm}, Name: "t"})
		if err != nil {
			t.Fatal(err)
		}
		lastID = is.Token.ID
		return is.Plaintext
	}
	e := &env{reg: reg, results: results.New(db), projectA: string(pa.ID), q: queue.New(db), db: db}
	e.tokenA = issue(authz.LevelProject, string(pa.ID), authz.PermProjectIngest)
	e.tokenAID = lastID
	e.tokenB = issue(authz.LevelProject, string(pb.ID), authz.PermProjectIngest)
	e.tokenOrg = issue(authz.LevelOrganization, string(org.ID), authz.PermProjectIngest)
	e.tokenRO = issue(authz.LevelProject, string(pa.ID), authz.PermProjectRead)

	deps := ingest.Deps{
		DB: db, Queue: e.q, Tenancy: ten, Authz: az, Results: e.results, Identity: identity.New(db), Metrics: reg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	ingest.Routes(mux, deps)
	e.jobs = ingest.NewJobs(deps)
	e.flaky = flaky.New(db, e.results)
	e.worker = e.jobs.Worker(1, func(ctx context.Context, projectID string, runID results.RunID) error {
		_, err := e.flaky.RecomputeRun(ctx, projectID, string(runID))
		return err
	})
	e.srv = httptest.NewServer(httpx.RequestID(mux))
	t.Cleanup(e.srv.Close)
	return e
}

type resp struct {
	code int
	body map[string]any
	raw  string
}

func (e *env) post(t *testing.T, token, path string, body any, headers ...string) resp {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		_ = json.NewEncoder(&buf).Encode(b)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (r resp) errCode() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	return ""
}

const localRun = "0198c3e1-7b1a-7c3d-9e4f-0123456789ab"

// Keep repeated payloads identical and finished evidence inside the 30-day window.
var fixtureStartedAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

func runBody(local string) map[string]any {
	return map[string]any{
		"localRunId": local, "environmentKey": "staging",
		"trigger":   map[string]any{"type": "ci", "provider": "github-actions", "ref": "refs/heads/main", "commitSha": "ab12", "ciRunUrl": "https://ci.example/1"},
		"framework": map[string]any{"name": "playwright", "version": "1.46.0"},
		"reporter":  map[string]any{"name": "@platform/playwright-reporter", "version": "0.3.1", "protocolVersion": "1.1"},
		"startedAt": fixtureStartedAt.Format(time.RFC3339Nano),
	}
}

func ev(seq int, client string, attempt int, status, outcome string) map[string]any {
	return map[string]any{
		"sequence": seq, "type": "test.finished", "clientTestId": client, "occurrenceIndex": 0, "attempt": attempt,
		"identity":   map[string]any{"framework": "playwright", "filePath": "tests/checkout/payment.spec.ts", "titlePath": []string{"Checkout", "Payment", client}, "paramSignature": "browser=firefox"},
		"status":     status,
		"outcome":    outcome,
		"durationMs": 8412,
		"startedAt":  fixtureStartedAt.Add(time.Second).Format(time.RFC3339Nano), "finishedAt": fixtureStartedAt.Add(10 * time.Second).Format(time.RFC3339Nano),
	}
}

func TestIngestEndToEnd(t *testing.T) {
	e := setup(t)

	// Auth boundary.
	if r := e.post(t, "", "/v1/runs", runBody(localRun)); r.code != 401 || r.errCode() != "UNAUTHENTICATED" {
		t.Errorf("no token: %d %s", r.code, r.raw)
	}
	if r := e.post(t, "atp_ing_"+strings.Repeat("0", 64), "/v1/runs", runBody(localRun)); r.code != 401 {
		t.Errorf("unknown token: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenOrg, "/v1/runs", runBody(localRun)); r.code != 403 || r.errCode() != "TOKEN_SCOPE" {
		t.Errorf("org-scoped token: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenRO, "/v1/runs", runBody(localRun)); r.code != 403 || r.errCode() != "FORBIDDEN" {
		t.Errorf("read-only token: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenA, "/v1/runs", runBody(localRun), "X-Protocol-Version", "2.0"); r.code != 426 {
		t.Errorf("future protocol: %d %s", r.code, r.raw)
	}

	// Create run: 201, then 200 on resend with the same id (§7.2 #1).
	r := e.post(t, e.tokenA, "/v1/runs", runBody(localRun), "X-Protocol-Version", "1.1")
	if r.code != 201 {
		t.Fatalf("create: %d %s", r.code, r.raw)
	}
	runID, _ := r.body["runId"].(string)
	lim, _ := r.body["limits"].(map[string]any)
	if runID == "" || lim["maxEventsPerBatch"] != float64(500) || lim["artifactBytes"].(map[string]any)["video"] != float64(20<<20) {
		t.Errorf("create body: %s", r.raw)
	}
	if r := e.post(t, e.tokenA, "/v1/runs", runBody(localRun)); r.code != 200 || r.body["runId"] != runID {
		t.Errorf("resend: %d %s", r.code, r.raw)
	}
	for field, body := range map[string]map[string]any{
		"localRunId":     {"localRunId": "not-a-uuid", "environmentKey": "staging", "startedAt": "2026-09-14T02:11:04Z"},
		"environmentKey": {"localRunId": localRun, "environmentKey": "Prod Smoke", "startedAt": "2026-09-14T02:11:04Z"},
		"startedAt":      {"localRunId": localRun, "environmentKey": "staging"},
		"trigger":        {"localRunId": localRun, "environmentKey": "staging", "startedAt": "2026-09-14T02:11:04Z", "trigger": []int{1}},
	} {
		r := e.post(t, e.tokenA, "/v1/runs", body)
		det, _ := r.body["error"].(map[string]any)["details"].(map[string]any)
		if r.code != 400 || r.errCode() != "INVALID_PAYLOAD" || det["field"] != field {
			t.Errorf("bad %s: %d %s", field, r.code, r.raw)
		}
	}
	if r := e.post(t, e.tokenA, "/v1/runs", `{"localRunId": `); r.code != 400 {
		t.Errorf("malformed json: %d %s", r.code, r.raw)
	}

	// Events.
	events := []map[string]any{
		ev(1, "ct_pass", 0, "passed", "expected"),
		ev(2, "ct_known", 0, "failed", "unexpected"),
		ev(3, "ct_known", 1, "passed", "flaky"),
	}
	events[1]["error"] = map[string]any{"message": "expected 200, got 500", "stack": strings.Repeat("x", 70<<10)}
	r = e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": events})
	if r.code != 200 || r.body["accepted"] != float64(3) || r.body["duplicates"] != float64(0) {
		t.Fatalf("events: %d %s", r.code, r.raw)
	}
	if im, _ := r.body["identityMap"].(map[string]any); len(im) != 0 {
		t.Errorf("first sight of these tests: tier 1 must know nothing, got %v", im)
	}
	// Retried batch: all duplicates, still 200 (§7.2 #2).
	r = e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": events})
	if r.code != 200 || r.body["accepted"] != float64(0) || r.body["duplicates"] != float64(3) {
		t.Errorf("retry: %d %s", r.code, r.raw)
	}
	// Upper-case localRunId in the path still finds the run.
	r = e.post(t, e.tokenA, "/v1/runs/"+strings.ToUpper(localRun)+"/events", map[string]any{"events": events[:1]})
	if r.code != 200 {
		t.Errorf("upper-case path: %d %s", r.code, r.raw)
	}

	// Validation at the boundary, with the offending index and field.
	badEv := ev(9, "ct_bad", 0, "exploded", "expected")
	r = e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": []any{ev(8, "ct_ok", 0, "passed", "expected"), badEv}})
	det, _ := r.body["error"].(map[string]any)["details"].(map[string]any)
	if r.code != 400 || det["index"] != float64(1) || det["field"] != "events[1].status" {
		t.Errorf("bad event: %d %s", r.code, r.raw)
	}
	noAttempt := ev(9, "ct_bad", 0, "passed", "expected")
	delete(noAttempt, "attempt")
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": []any{noAttempt}}); r.code != 400 || !strings.Contains(r.raw, "attempt") {
		t.Errorf("missing attempt: %d %s", r.code, r.raw)
	}
	other := ev(9, "ct_bad", 0, "passed", "expected")
	other["type"] = "test.started"
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": []any{other}}); r.code != 400 || r.errCode() != "UNSUPPORTED_EVENT_TYPE" {
		t.Errorf("unsupported type: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": []any{}}); r.code != 400 {
		t.Errorf("empty batch: %d %s", r.code, r.raw)
	}

	// Caps (§7.4): 501 events, and a body over 5 MB.
	many := make([]any, 501)
	for i := range many {
		many[i] = ev(100+i, fmt.Sprintf("ct_%d", i), 0, "passed", "expected")
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": many}); r.code != 413 || r.errCode() != "BATCH_TOO_LARGE" {
		t.Errorf("501 events: %d %s", r.code, r.raw)
	}
	huge := ev(700, "ct_huge", 0, "passed", "expected")
	huge["identity"].(map[string]any)["filePath"] = "x"
	huge["padding"] = strings.Repeat("p", 6<<20)
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": []any{huge}}); r.code != 413 {
		t.Errorf("6 MB body: %d %s", r.code, r.raw[:min(len(r.raw), 200)])
	}

	// Cross-tenant (§11.3.3 release gate): B cannot see A's run, at all.
	for _, path := range []string{"/v1/runs/" + localRun + "/events", "/v1/runs/" + localRun + "/finish"} {
		if r := e.post(t, e.tokenB, path, map[string]any{"events": events}); r.code != 404 || r.errCode() != "RUN_NOT_FOUND" {
			t.Errorf("tenant B on A's run %s: %d %s", path, r.code, r.raw)
		}
	}
	// B creating a run with the same localRunId gets its own run.
	if r := e.post(t, e.tokenB, "/v1/runs", runBody(localRun)); r.code != 201 || r.body["runId"] == runID {
		t.Errorf("B create: %d %s", r.code, r.raw)
	}

	// Finish: failed? No — ct_known ended flaky, ct_pass passed → passed, flakyCount 1.
	r = e.post(t, e.tokenA, "/v1/runs/"+localRun+"/finish", map[string]any{"finishedAt": fixtureStartedAt.Add(10 * time.Minute).Format(time.RFC3339Nano)})
	if r.code != 200 || r.body["status"] != "passed" || r.body["flakyCount"] != float64(1) || r.body["runId"] != runID {
		t.Fatalf("finish: %d %s", r.code, r.raw)
	}
	// Finish enqueued exactly one tier-2 job for the run (§6.9.6 outbox);
	// the worker runs it. A second finish must not enqueue another.
	if pending, _, _ := e.q.Depth(context.Background(), ingest.JobRunFinished); pending != 1 {
		t.Errorf("jobs after finish = %d, want 1", pending)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/finish", nil); r.code != 200 || r.body["status"] != "passed" {
		t.Errorf("finish twice, empty body: %d %s", r.code, r.raw)
	}
	if n := e.drain(t); n != 1 {
		t.Errorf("worker ran %d jobs after two finishes, want 1", n)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+localRun+"/events", map[string]any{"events": []any{ev(50, "ct_late", 0, "passed", "expected")}}); r.code != 409 || r.errCode() != "RUN_FINISHED" {
		t.Errorf("events after finish: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/00000000-0000-4000-8000-000000000000/finish", nil); r.code != 404 {
		t.Errorf("finish unknown: %d %s", r.code, r.raw)
	}

	// Metrics (§6.4): per-project request counters by status class exist.
	m := e.reg.Expose()
	for _, want := range []string{
		`ingest_requests_total{endpoint="runs",project="`, `status="2xx"} `,
		`ingest_events_total{project="`, `result="duplicate"} `,
		// §11.4 volume alert needs the accepted count per token id.
		`ingest_events_by_token_total{project="` + e.projectA + `",token="` + e.tokenAID + `"} `,
		`ingest_request_seconds_count{endpoint="events"}`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
	// Tier 2 ran at finish: every result of the run now has a stable id.
	list, err := e.results.ListResults(context.Background(), results.RunID(runID))
	if err != nil || len(list) == 0 {
		t.Fatalf("list results: %v", err)
	}
	stable := map[string]string{}
	for _, tr := range list {
		if tr.StableTestID == "" {
			t.Errorf("%s unresolved after finish", tr.ClientTestID)
		}
		stable[tr.ClientTestID] = tr.StableTestID
	}
	if !strings.Contains(m, `identity_resolve_total{`) {
		t.Error("no identity_resolve_total metric")
	}

	// Run 2, same tests: tier 1 knows them now and the ids are stable.
	local2 := "0198c3e1-7b1a-7c3d-9e4f-0123456789ac"
	if r := e.post(t, e.tokenA, "/v1/runs", runBody(local2)); r.code != 201 {
		t.Fatalf("run 2: %d %s", r.code, r.raw)
	}
	r = e.post(t, e.tokenA, "/v1/runs/"+local2+"/events", map[string]any{"events": events})
	im, _ := r.body["identityMap"].(map[string]any)
	if r.code != 200 || im["ct_pass"] != stable["ct_pass"] || im["ct_known"] != stable["ct_known"] {
		t.Errorf("run 2 identityMap = %v, want %v", im, stable)
	}

	// Run 3: ct_pass was renamed, ct_known still runs. Tier 2 at finish must
	// auto-match the rename (§9.3) and keep ct_known's id.
	local3 := "0198c3e1-7b1a-7c3d-9e4f-0123456789ad"
	renamed := ev(1, "ct_pass_v2", 0, "passed", "expected")
	renamed["identity"].(map[string]any)["titlePath"] = []string{"Checkout", "Payment", "ct_pass renamed"}
	_ = e.post(t, e.tokenA, "/v1/runs", runBody(local3))
	if r := e.post(t, e.tokenA, "/v1/runs/"+local3+"/events", map[string]any{"events": []any{renamed, ev(2, "ct_known", 0, "passed", "expected")}}); r.code != 200 {
		t.Fatalf("run 3 events: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+local3+"/finish", nil); r.code != 200 {
		t.Fatalf("run 3 finish: %d %s", r.code, r.raw)
	}
	e.drain(t)
	run3, _ := e.results.GetRun(context.Background(), e.projectA, local3)
	list3, _ := e.results.ListResults(context.Background(), run3.ID)
	for _, tr := range list3 {
		switch tr.ClientTestID {
		case "ct_pass_v2":
			if tr.StableTestID != stable["ct_pass"] {
				t.Errorf("renamed test got %q, want ct_pass's identity %q (auto-match)", tr.StableTestID, stable["ct_pass"])
			}
		case "ct_known":
			if tr.StableTestID != stable["ct_known"] {
				t.Errorf("ct_known drifted to %q", tr.StableTestID)
			}
		}
	}

	// Flaky (§8.6) ran after resolve: ct_known was flaky_pass in run 1 and
	// passed in run 3; run 2 was never finished and so is not evidence →
	// two runs, one flaky pass, no flips.
	sig, err := e.flaky.Get(context.Background(), stable["ct_known"], run3.EnvironmentID)
	if err != nil || sig.TotalRuns != 2 || sig.FlakyPassRuns != 1 || sig.FlipCount != 0 || !sig.HasScore {
		t.Errorf("flaky signal for ct_known: %+v %v", sig, err)
	}
}

func TestRateLimitPerToken(t *testing.T) {
	e := setup(t)
	codes := map[int]int{}
	for i := 0; i < ingest.RateBurst+5; i++ {
		r := e.post(t, e.tokenA, "/v1/runs/00000000-0000-4000-8000-000000000000/finish", nil)
		codes[r.code]++
	}
	// The bucket refills at one token per second while the loop runs, so
	// under a slow CI a few extra requests get through: assert the shape,
	// not the exact split.
	if codes[429] < 1 || codes[404] > ingest.RateBurst+10 {
		t.Errorf("codes = %v; want roughly the burst then 429s", codes)
	}
	// Another token is unaffected.
	if r := e.post(t, e.tokenB, "/v1/runs/00000000-0000-4000-8000-000000000000/finish", nil); r.code != 404 {
		t.Errorf("other token limited: %d", r.code)
	}
}

// TestSweepAutoClosesStaleRuns is §7.4 / §14.3 "no run stuck in running":
// a run open longer than 6 h is closed as error RUN_TIMED_OUT, its tier-2
// job is queued, and a run that is still fresh is left alone.
func TestSweepAutoClosesStaleRuns(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	stale, fresh := "0198c3e1-7b1a-7c3d-9e4f-00000000aaaa", "0198c3e1-7b1a-7c3d-9e4f-00000000bbbb"
	oldBody := runBody(stale)
	oldBody["startedAt"] = time.Now().Add(-7 * time.Hour).UTC().Format(time.RFC3339)
	if r := e.post(t, e.tokenA, "/v1/runs", oldBody); r.code != 201 {
		t.Fatalf("stale run: %d %s", r.code, r.raw)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+stale+"/events", map[string]any{"events": []any{ev(1, "ct_stale", 0, "passed", "expected")}}); r.code != 200 {
		t.Fatalf("stale events: %d %s", r.code, r.raw)
	}
	freshBody := runBody(fresh)
	freshBody["startedAt"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if r := e.post(t, e.tokenA, "/v1/runs", freshBody); r.code != 201 {
		t.Fatalf("fresh run: %d %s", r.code, r.raw)
	}

	n, err := e.jobs.Sweep(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sweep closed %d, err %v; want 1", n, err)
	}
	if n, _ := e.jobs.Sweep(ctx); n != 0 {
		t.Errorf("second sweep closed %d more", n)
	}
	run, _ := e.results.GetRun(ctx, e.projectA, stale)
	if run.Status != results.RunError || run.ErrorReason != "RUN_TIMED_OUT" || run.FinishedAt.IsZero() {
		t.Errorf("stale run after sweep: %+v", run)
	}
	if r := e.post(t, e.tokenA, "/v1/runs/"+stale+"/events", map[string]any{"events": []any{ev(2, "ct_late", 0, "passed", "expected")}}); r.code != 409 {
		t.Errorf("events into an auto-closed run: %d", r.code)
	}
	if run, _ := e.results.GetRun(ctx, e.projectA, fresh); run.Status != results.RunRunning {
		t.Errorf("fresh run touched: %+v", run)
	}
	// The closed run still gets identities.
	if n := e.drain(t); n != 1 {
		t.Errorf("drained %d jobs, want 1", n)
	}
	list, _ := e.results.ListResults(ctx, run.ID)
	if len(list) != 1 || list[0].StableTestID == "" {
		t.Errorf("auto-closed run left unresolved: %+v", list)
	}
	if !strings.Contains(e.reg.Expose(), `runs_autoclosed_total{project="`+e.projectA+`"} 1`) {
		t.Error("runs_autoclosed_total not counted")
	}
}

// TestEventSequenceBoundary pins the public contract of Event.sequence
// (deploy/openapi.yaml: integer, minimum 1): the request boundary owns
// "malformed", the unique index owns "duplicate". Gaps are fine — the
// counter is the reporter's, not a database sequence.
func TestEventSequenceBoundary(t *testing.T) {
	e := setup(t)
	local := "0198c3e1-7b1a-7c3d-9e4f-00000000beef"
	if r := e.post(t, e.tokenA, "/v1/runs", runBody(local)); r.code != 201 {
		t.Fatalf("create run: %d %s", r.code, r.raw)
	}
	detailField := func(r resp) string {
		err, _ := r.body["error"].(map[string]any)
		details, _ := err["details"].(map[string]any)
		f, _ := details["field"].(string)
		return f
	}
	send := func(events ...map[string]any) resp {
		return e.post(t, e.tokenA, "/v1/runs/"+local+"/events", map[string]any{"events": events})
	}
	withSeq := func(seq any) map[string]any {
		ev := ev(1, "ct_seq", 0, "passed", "expected")
		if seq == nil {
			delete(ev, "sequence")
		} else {
			ev["sequence"] = seq
		}
		return ev
	}

	for _, tc := range []struct {
		name string
		seq  any
	}{{"missing", nil}, {"negative", -1}, {"zero", 0}} {
		r := send(withSeq(tc.seq))
		if r.code != 400 || r.errCode() != "INVALID_PAYLOAD" || detailField(r) != "events[0].sequence" {
			t.Errorf("sequence %s: %d %s %q, want 400 INVALID_PAYLOAD on events[0].sequence", tc.name, r.code, r.errCode(), detailField(r))
		}
	}
	// 1 is the first valid value; 42 is a valid gap.
	if r := send(withSeq(1), func() map[string]any { x := withSeq(42); x["clientTestId"] = "ct_gap"; return x }()); r.code != 200 || r.body["accepted"] != float64(2) || r.body["duplicates"] != float64(0) {
		t.Fatalf("sequence 1 and 42: %d %s", r.code, r.raw)
	}
	// The same positive value again is a duplicate — counted, not stored.
	if r := send(withSeq(1)); r.code != 200 || r.body["accepted"] != float64(0) || r.body["duplicates"] != float64(1) {
		t.Errorf("repeated sequence 1: %d %s, want accepted 0 duplicates 1", r.code, r.raw)
	}
	var stored int
	if err := e.db.Q(context.Background()).QueryRow(context.Background(), `SELECT count(*) FROM run_events e JOIN runs r ON r.id = e.run_id WHERE r.local_run_id = $1`, local).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != 2 {
		t.Errorf("%d events stored, want 2 (sequence 1 once, 42 once)", stored)
	}
}
