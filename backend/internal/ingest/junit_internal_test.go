package ingest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// Maven Surefire with rerunFailingTestsCount: a flaky pass, a failure that
// never passed, a timeout, a skip, one plain pass.
const surefire = `<?xml version="1.0" encoding="UTF-8"?>
<testsuite name="com.acme.pay.CardTest" timestamp="2026-03-02T09:15:00" tests="5">
  <testcase name="declinesExpired" classname="com.acme.pay.CardTest" time="0.250"/>
  <testcase name="chargesVisa" classname="com.acme.pay.CardTest" time="1.5">
    <flakyFailure message="expected 200 but was 502" type="java.lang.AssertionError" time="1.2">
      <stackTrace>java.lang.AssertionError: expected 200 but was 502
	at com.acme.pay.CardTest.chargesVisa(CardTest.java:41)</stackTrace>
    </flakyFailure>
  </testcase>
  <testcase name="refunds" classname="com.acme.pay.CardTest" time="0.9">
    <failure message="refund id missing" type="org.opentest4j.AssertionFailedError">at CardTest.java:77</failure>
    <rerunFailure message="refund id missing" type="org.opentest4j.AssertionFailedError" time="0.8"/>
  </testcase>
  <testcase name="settles" classname="com.acme.pay.CardTest" time="30">
    <error message="test timed out after 30 seconds" type="org.junit.runners.model.TestTimedOutException"/>
  </testcase>
  <testcase name="threeDS" classname="com.acme.pay.CardTest" time="0">
    <skipped message="3DS sandbox down"/>
  </testcase>
</testsuite>`

func sig(t *testing.T, a results.AttemptEvent) identityRaw {
	t.Helper()
	var id identityRaw
	if err := json.Unmarshal(a.IdentityRaw, &id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSurefireMapping(t *testing.T) {
	rep, err := parseJUnit([]byte(surefire), "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tests != 5 || len(rep.Attempts) != 7 {
		t.Fatalf("tests %d attempts %d, want 5 and 7", rep.Tests, len(rep.Attempts))
	}
	type row struct {
		name    string
		attempt int
		status  results.AttemptStatus
		outcome results.Outcome
	}
	want := []row{
		{"declinesExpired", 0, results.AttemptPassed, results.OutcomeExpected},
		{"chargesVisa", 0, results.AttemptFailed, results.OutcomeFlaky},
		{"chargesVisa", 1, results.AttemptPassed, results.OutcomeFlaky},
		{"refunds", 0, results.AttemptFailed, results.OutcomeUnexpected},
		{"refunds", 1, results.AttemptFailed, results.OutcomeUnexpected},
		{"settles", 0, results.AttemptTimedOut, results.OutcomeUnexpected},
		{"threeDS", 0, results.AttemptSkipped, results.OutcomeSkipped},
	}
	for i, w := range want {
		a := rep.Attempts[i]
		if a.DisplayName != w.name || a.Attempt != w.attempt || a.Status != w.status || a.Outcome != w.outcome {
			t.Errorf("attempt %d = %s #%d %s/%s, want %+v", i, a.DisplayName, a.Attempt, a.Status, a.Outcome, w)
		}
		if a.Sequence != int64(i+1) {
			t.Errorf("attempt %d sequence %d, want %d (1-based, file order)", i, a.Sequence, i+1)
		}
	}
	// Java: the class is the file; the title is the method alone.
	id := sig(t, rep.Attempts[0])
	if id.Framework != "junit" || id.FilePath != "com/acme/pay/CardTest" || strings.Join(id.TitlePath, "|") != "declinesExpired" || id.ParamSignature != "" {
		t.Errorf("identity = %+v", id)
	}
	// Retries share the test's clientTestId; different tests do not.
	if rep.Attempts[1].ClientTestID != rep.Attempts[2].ClientTestID || rep.Attempts[0].ClientTestID == rep.Attempts[1].ClientTestID {
		t.Error("clientTestId: attempts of one test must share it, tests must not")
	}
	if !strings.HasPrefix(rep.Attempts[0].ClientTestID, "ct_") || len(rep.Attempts[0].ClientTestID) != 35 {
		t.Errorf("clientTestId %q, want ct_ + 32 hex (§9.2)", rep.Attempts[0].ClientTestID)
	}
	// Errors carry the type, the message and the stack.
	var e errorRaw
	_ = json.Unmarshal(rep.Attempts[1].Error, &e)
	if e.Message != "java.lang.AssertionError: expected 200 but was 502" || !strings.Contains(e.Stack, "CardTest.java:41") {
		t.Errorf("flaky failure error = %+v", e)
	}
	// Times: no zone means UTC; cases follow each other; a rerun before the
	// final attempt.
	start := time.Date(2026, 3, 2, 9, 15, 0, 0, time.UTC)
	if !rep.StartedAt.Equal(start) || !rep.Attempts[0].StartedAt.Equal(start) {
		t.Errorf("started %v / %v, want %v", rep.StartedAt, rep.Attempts[0].StartedAt, start)
	}
	if got := rep.Attempts[1].StartedAt; !got.Equal(start.Add(250 * time.Millisecond)) {
		t.Errorf("second case starts %v", got)
	}
	if !rep.Attempts[2].StartedAt.Equal(rep.Attempts[1].FinishedAt) || rep.Attempts[1].DurationMs != 1200 || rep.Attempts[2].DurationMs != 1500 {
		t.Errorf("flaky attempts timing: %+v / %+v", rep.Attempts[1], rep.Attempts[2])
	}
	if !rep.FinishedAt.Equal(rep.Attempts[len(rep.Attempts)-1].FinishedAt) {
		t.Error("FinishedAt is not the last attempt's end")
	}
	// Every event passes the same validation as a reporter's.
	for i, a := range rep.Attempts {
		if a.StartedAt.IsZero() || a.DisplayName == "" || len(a.IdentityRaw) == 0 {
			t.Errorf("attempt %d incomplete: %+v", i, a)
		}
	}
}

// pytest --junitxml: file attribute (with -o junit_family=xunit1), the
// class inside the module, parameters in brackets.
func TestPytestMapping(t *testing.T) {
	const doc = `<testsuites><testsuite name="pytest" timestamp="2026-03-02T10:00:00.123456+07:00">
	  <testcase classname="tests.test_pay.TestCard" name="test_charge[visa-3ds]" file="tests/test_pay.py" time="0.01"/>
	  <testcase classname="tests.test_pay.TestCard" name="test_charge[mastercard]" file="tests/test_pay.py" time="0.01"/>
	  <testcase classname="tests.test_pay" name="test_module_level" file="tests/test_pay.py" time="0.01"/>
	</testsuite></testsuites>`
	rep, err := parseJUnit([]byte(doc), "pytest", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := sig(t, rep.Attempts[0]), sig(t, rep.Attempts[1]), sig(t, rep.Attempts[2])
	if a.Framework != "pytest" || a.FilePath != "tests/test_pay.py" || strings.Join(a.TitlePath, "|") != "TestCard|test_charge" || a.ParamSignature != "param=visa-3ds" {
		t.Errorf("parametrized = %+v", a)
	}
	if b.ParamSignature != "param=mastercard" || rep.Attempts[0].ClientTestID == rep.Attempts[1].ClientTestID {
		t.Error("two parameter sets must be two tests")
	}
	if strings.Join(c.TitlePath, "|") != "test_module_level" {
		t.Errorf("module-level test title = %v", c.TitlePath)
	}
	if want := time.Date(2026, 3, 2, 3, 0, 0, 123456000, time.UTC); !rep.StartedAt.Equal(want) {
		t.Errorf("zoned timestamp read as %v, want %v", rep.StartedAt, want)
	}
}

// jest-junit: nested suites under <testsuites>, file on the suite, the
// classname repeating describe + title.
func TestJestMapping(t *testing.T) {
	const doc = `<testsuites name="jest tests"><testsuite name="Cart" timestamp="2026-03-02T11:00:00" file="src/cart.test.ts">
	  <testcase classname="Cart adds an item" name="Cart adds an item" time="0.004"/>
	  <testcase classname="Cart adds an item" name="Cart adds an item" time="0.004"/>
	</testsuite></testsuites>`
	rep, err := parseJUnit([]byte(doc), "jest", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	id := sig(t, rep.Attempts[0])
	if id.FilePath != "src/cart.test.ts" || strings.Join(id.TitlePath, "|") != "Cart adds an item" {
		t.Errorf("identity = %+v", id)
	}
	// Two tests with the same name in one file: same fingerprint, told apart
	// by occurrenceIndex (N03), never collapsed.
	if rep.Attempts[0].OccurrenceIndex != 0 || rep.Attempts[1].OccurrenceIndex != 1 {
		t.Errorf("occurrence indexes %d, %d", rep.Attempts[0].OccurrenceIndex, rep.Attempts[1].OccurrenceIndex)
	}
}

// Node's test runner (node --test --test-reporter=junit, Node 22), trimmed
// from a real run: a top-level test straight under <testsuites>, each
// describe() a <testsuite>, no file, classname "test" on every case, no
// timestamp. Before 01-11 the top-level test was dropped without a word, and
// a report of top-level tests only was refused.
const nodeReport = `<?xml version="1.0" encoding="utf-8"?>
<testsuites>
	<testcase name="totals an empty cart" time="0.000581" classname="test"/>
	<testsuite name="Cart" time="0.001861" disabled="0" errors="0" tests="3" failures="1" skipped="2" hostname="ci-runner">
		<testcase name="adds an item" time="0.000706" classname="test"/>
		<testsuite name="coupons" time="0.000686" disabled="0" errors="0" tests="1" failures="1" skipped="0" hostname="ci-runner">
			<testcase name="applies a coupon" time="0.000559" classname="test" failure="coupon not applied">
				<failure type="testCodeFailure" message="coupon not applied">
Error [ERR_TEST_FAILURE]: coupon not applied
  cause: AssertionError [ERR_ASSERTION]: coupon not applied
      at TestContext.&lt;anonymous> (file:///work/test/cart.test.mjs:7:41)
				</failure>
			</testcase>
		</testsuite>
		<testcase name="ships abroad" time="0.000075" classname="test">
			<skipped type="skipped" message="carrier sandbox down"/>
		</testcase>
	</testsuite>
	<testsuite name="Checkout" time="0.000207" disabled="0" errors="0" tests="1" failures="0" skipped="0" hostname="ci-runner">
		<testcase name="adds an item" time="0.000143" classname="test"/>
	</testsuite>
	<!-- tests 5 -->
	<!-- duration_ms 76.9534 -->
</testsuites>`

func TestNodeTestRunnerMapping(t *testing.T) {
	at := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	rep, err := parseJUnit([]byte(nodeReport), "node", at)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tests != 5 || len(rep.Attempts) != 5 {
		t.Fatalf("tests %d attempts %d, want 5 and 5 (the top-level test too)", rep.Tests, len(rep.Attempts))
	}
	byTitle := map[string]results.AttemptEvent{}
	for _, a := range rep.Attempts {
		id := sig(t, a)
		if id.Framework != "node" || id.FilePath != "test" {
			t.Errorf("%s: identity %+v", a.DisplayName, id)
		}
		byTitle[strings.Join(id.TitlePath, " › ")] = a
	}
	for title, want := range map[string]results.AttemptStatus{
		"totals an empty cart":              results.AttemptPassed,
		"Cart › adds an item":               results.AttemptPassed,
		"Cart › coupons › applies a coupon": results.AttemptFailed,
		"Cart › ships abroad":               results.AttemptSkipped,
		"Checkout › adds an item":           results.AttemptPassed,
	} {
		a, ok := byTitle[title]
		if !ok {
			t.Errorf("no test %q among %d", title, len(byTitle))
			continue
		}
		if a.Status != want {
			t.Errorf("%q is %s, want %s", title, a.Status, want)
		}
	}
	// The describes are what the UI shows as the suite.
	if a := byTitle["Cart › coupons › applies a coupon"]; a.SuiteName != "Cart › coupons" || a.DisplayName != "applies a coupon" {
		t.Errorf("suite %q display %q", a.SuiteName, a.DisplayName)
	}
	// One name in two describes is two tests, not one test seen twice.
	if byTitle["Cart › adds an item"].ClientTestID == byTitle["Checkout › adds an item"].ClientTestID {
		t.Error("Cart › adds an item and Checkout › adds an item share a clientTestId")
	}
	var e errorRaw
	_ = json.Unmarshal(byTitle["Cart › coupons › applies a coupon"].Error, &e)
	if e.Message != "testCodeFailure: coupon not applied" || !strings.Contains(e.Stack, "cart.test.mjs:7:41") {
		t.Errorf("failure error = %+v", e)
	}
	// Node writes no timestamp: the caller dates the run.
	if !rep.StartedAt.Equal(at) {
		t.Errorf("started %v, want %v", rep.StartedAt, at)
	}
}

func TestJUnitRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		doc  string
		want string
	}{
		"not xml":          {`{"tests": []}`, "not a JUnit report"},
		"other root":       {`<html><body/></html>`, "root element <html>"},
		"no cases":         {`<testsuites><testsuite name="x" timestamp="2026-01-01T00:00:00"/></testsuites>`, "no <testcase>"},
		"no date anywhere": {`<testsuite name="x"><testcase name="a" classname="A"/></testsuite>`, "pass startedAt"},
		"node, no date":    {`<testsuites><testcase name="a" classname="test"/></testsuites>`, "no timestamp in the report — pass startedAt"},
		"bad timestamp":    {`<testsuite name="x" timestamp="yesterday"><testcase name="a" classname="A"/></testsuite>`, "want ISO 8601"},
		"nameless case":    {`<testsuite name="x" timestamp="2026-01-01T00:00:00"><testcase classname="A"/></testsuite>`, "has no name"},
		"external entity":  {`<?xml version="1.0"?><!DOCTYPE t [<!ENTITY x SYSTEM "file:///etc/passwd">]><testsuite name="&x;" timestamp="2026-01-01T00:00:00"><testcase name="a" classname="A"/></testsuite>`, "junit"},
		"entity expansion": {`<?xml version="1.0"?><!DOCTYPE t [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;">]><testsuite name="&b;" timestamp="2026-01-01T00:00:00"><testcase name="a" classname="A"/></testsuite>`, "junit"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseJUnit([]byte(tc.doc), "", time.Time{})
			if err == nil || !errors.Is(err, ErrJUnit) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrJUnit mentioning %q", err, tc.want)
			}
		})
	}
}

// A file without a timestamp is dated by the caller.
func TestJUnitStartedAtFromCaller(t *testing.T) {
	at := time.Date(2025, 12, 1, 8, 0, 0, 0, time.UTC)
	rep, err := parseJUnit([]byte(`<testsuite name="x"><testcase name="a" classname="A" time="2"/></testsuite>`), "", at)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.StartedAt.Equal(at) || !rep.FinishedAt.Equal(at.Add(2*time.Second)) {
		t.Errorf("span %v – %v", rep.StartedAt, rep.FinishedAt)
	}
}

func TestReportRunIDIsStableAndScoped(t *testing.T) {
	a := reportRunID([]byte(surefire), "ci", "junit")
	if a != reportRunID([]byte(surefire), "ci", "junit") {
		t.Error("same report, different id")
	}
	if !isUUID(a) || a[14] != '8' {
		t.Errorf("%q is not a v8 UUID", a)
	}
	if a == reportRunID([]byte(surefire), "nightly", "junit") || a == reportRunID([]byte(surefire+" "), "ci", "junit") {
		t.Error("another environment or another file must be another run")
	}
}
