package ingest_test

import (
	"context"
	"testing"
)

// A Surefire report with a flaky pass (a failed run, then a pass), a
// failure rerun once, and a plain pass.
const junitReport = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="com.acme.pay.CardTest" timestamp="2026-03-02T09:15:00">
  <testcase name="declinesExpired" classname="com.acme.pay.CardTest" time="0.25"/>
  <testcase name="chargesVisa" classname="com.acme.pay.CardTest" time="1.5">
    <flakyFailure message="expected 200 but was 502" type="java.lang.AssertionError" time="1.2"/>
  </testcase>
  <testcase name="refunds" classname="com.acme.pay.CardTest" time="0.9">
    <failure message="refund id missing" type="AssertionError">at CardTest.java:77</failure>
    <rerunFailure message="refund id missing" type="AssertionError" time="0.8"/>
  </testcase>
</testsuite>`

const xmlType = "application/xml"

// §6.8.5 / QĐ-13: one report is one historic run through the reporter's own
// pipeline — dated from the report, resolved by the worker, flaky folded —
// and importing it again adds nothing.
func TestJUnitImportThroughTheIngestPipeline(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	path := "/v1/imports/junit?environment=nightly&branch=refs/heads/main&commit=abc123"

	r := e.post(t, e.tokenA, path, junitReport, "Content-Type", xmlType)
	if r.code != 201 || r.body["created"] != true || r.body["tests"] != float64(3) || r.body["attempts"] != float64(5) {
		t.Fatalf("import: %d %s", r.code, r.raw)
	}
	if r.body["status"] != "failed" || r.body["flakyCount"] != float64(1) || r.body["startedAt"] != "2026-03-02T09:15:00Z" {
		t.Errorf("run summary: %s", r.raw)
	}
	runID := r.body["runId"].(string)
	if n := e.drain(t); n != 1 {
		t.Fatalf("jobs drained: %d, want the one run.finished", n)
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := e.db.Q(ctx).QueryRow(ctx, q, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	// Dated from the report, on the branch given, in the environment given.
	if n := count(`SELECT count(*) FROM runs r JOIN environments v ON v.id = r.environment_id
		WHERE r.id = $1 AND r.branch = 'main' AND r.commit_sha = 'abc123' AND v.key = 'nightly'
		  AND r.started_at = '2026-03-02T09:15:00Z' AND r.finished_at IS NOT NULL`, runID); n != 1 {
		t.Error("run row: date, branch, commit or environment wrong")
	}
	if n := count(`SELECT count(*) FROM test_results WHERE run_id = $1 AND stable_test_id IS NOT NULL`, runID); n != 3 {
		t.Errorf("resolved results: %d, want 3 (tier 2 ran)", n)
	}
	if n := count(`SELECT count(*) FROM test_results WHERE run_id = $1 AND final_status = 'flaky_pass' AND attempt_count = 2`, runID); n != 1 {
		t.Error("the Surefire flaky pass is not a flaky_pass with two attempts")
	}
	if n := count(`SELECT count(*) FROM test_results WHERE run_id = $1 AND final_status = 'failed' AND attempt_count = 2`, runID); n != 1 {
		t.Error("the rerun failure is not a failed result with two attempts")
	}
	if n := count(`SELECT count(*) FROM test_identities i JOIN test_results t ON t.stable_test_id = i.id WHERE t.run_id = $1 AND i.framework = 'junit'`, runID); n != 3 {
		t.Errorf("identities in the junit namespace: %d", n)
	}

	// The same file again: the same run, nothing added, nothing enqueued.
	again := e.post(t, e.tokenA, path, junitReport, "Content-Type", xmlType)
	if again.code != 200 || again.body["created"] != false || again.body["runId"] != runID || again.body["attempts"] != float64(0) || again.body["duplicates"] != float64(5) {
		t.Fatalf("re-import: %d %s", again.code, again.raw)
	}
	if n := e.drain(t); n != 0 {
		t.Errorf("re-import enqueued %d jobs", n)
	}
	// A later report of the same suite lands on the same identities (tier 1).
	later := `<testsuite name="com.acme.pay.CardTest" timestamp="2026-03-03T09:15:00"><testcase name="declinesExpired" classname="com.acme.pay.CardTest" time="0.2"/></testsuite>`
	if r := e.post(t, e.tokenA, "/v1/imports/junit?environment=nightly", later, "Content-Type", xmlType); r.code != 201 {
		t.Fatalf("second report: %d %s", r.code, r.raw)
	}
	e.drain(t)
	if n := count(`SELECT count(DISTINCT stable_test_id) FROM test_results WHERE display_name = 'declinesExpired'`); n != 1 {
		t.Errorf("declinesExpired split into %d identities across two imports", n)
	}
}

func TestJUnitImportRefusals(t *testing.T) {
	e := setup(t)
	for name, tc := range map[string]struct {
		token, path, body, ctype string
		code                     int
		errCode                  string
	}{
		"no token":            {"", "/v1/imports/junit", junitReport, xmlType, 401, "UNAUTHENTICATED"},
		"read-only token":     {e.tokenRO, "/v1/imports/junit", junitReport, xmlType, 403, "FORBIDDEN"},
		"org-scoped token":    {e.tokenOrg, "/v1/imports/junit", junitReport, xmlType, 403, "TOKEN_SCOPE"},
		"json body":           {e.tokenA, "/v1/imports/junit", `{"a":1}`, "application/json", 415, "UNSUPPORTED_MEDIA_TYPE"},
		"not a report":        {e.tokenA, "/v1/imports/junit", `<html/>`, xmlType, 400, "INVALID_JUNIT"},
		"no date":             {e.tokenA, "/v1/imports/junit", `<testsuite name="x"><testcase name="a" classname="A"/></testsuite>`, xmlType, 400, "INVALID_JUNIT"},
		"bad environment":     {e.tokenA, "/v1/imports/junit?environment=Prod!", junitReport, xmlType, 400, "INVALID_PAYLOAD"},
		"bad startedAt":       {e.tokenA, "/v1/imports/junit?startedAt=yesterday", junitReport, xmlType, 400, "INVALID_PAYLOAD"},
		"localRunId not uuid": {e.tokenA, "/v1/imports/junit?localRunId=run-1", junitReport, xmlType, 400, "INVALID_PAYLOAD"},
	} {
		t.Run(name, func(t *testing.T) {
			r := e.post(t, tc.token, tc.path, tc.body, "Content-Type", tc.ctype)
			if r.code != tc.code || r.errCode() != tc.errCode {
				t.Fatalf("%d %s, want %d %s", r.code, r.raw, tc.code, tc.errCode)
			}
		})
	}
	// Nothing of the refused imports was stored.
	var n int
	if err := e.db.Q(context.Background()).QueryRow(context.Background(), `SELECT count(*) FROM runs`).Scan(&n); err != nil || n != 0 {
		t.Errorf("runs after refusals: %d (%v)", n, err)
	}
}

// A localRunId a reporter already holds is never extended by an import.
func TestJUnitImportNeverAddsToAReporterRun(t *testing.T) {
	e := setup(t)
	if r := e.post(t, e.tokenA, "/v1/runs", runBody(localRun), "X-Protocol-Version", "1.1"); r.code != 201 {
		t.Fatalf("reporter run: %d %s", r.code, r.raw)
	}
	r := e.post(t, e.tokenA, "/v1/imports/junit?localRunId="+localRun, junitReport, "Content-Type", xmlType)
	if r.code != 409 || r.errCode() != "RUN_CONFLICT" {
		t.Fatalf("import into a reporter run: %d %s", r.code, r.raw)
	}
	var n int
	if err := e.db.Q(context.Background()).QueryRow(context.Background(), `SELECT count(*) FROM test_results`).Scan(&n); err != nil || n != 0 {
		t.Errorf("results after the refusal: %d (%v)", n, err)
	}
}
