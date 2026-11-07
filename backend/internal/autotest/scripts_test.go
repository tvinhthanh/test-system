package autotest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

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

// fakeSandbox is sandboxd: it answers each job with the next result.
type fakeSandbox struct {
	mu      sync.Mutex
	jobs    []sandbox.Job
	results []sandbox.Result
	errs    []error
}

func (f *fakeSandbox) Run(_ context.Context, j sandbox.Job) (sandbox.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs = append(f.jobs, j)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return sandbox.Result{}, err
		}
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r, nil
}

const pwJUnit = `<testsuites><testsuite name="inbox.spec.ts">
<testcase name="shows inbox" classname="inbox.spec.ts"/>
<testcase name="sends a message" classname="inbox.spec.ts"><failure message="expected 1 message"/></testcase>
</testsuite></testsuites>`

// Script runs (QĐ-19 tier 4): the archive is checked before anything is
// queued, k6 needs the proof, one at a time per organization, the report
// goes to the recorder and the archive is dropped when the run ends.
func TestScriptRuns(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	box := &fakeSandbox{}
	f.svc.Sandbox = box
	f.svc.ScriptCaps = ScriptCaps{MaxSeconds: 600}
	f.svc.LoadCaps = LoadCaps{MaxVUs: 10, MaxSeconds: 60}
	pw := targz(t, map[string]string{"playwright.config.ts": "export default {}", "tests/inbox.spec.ts": "test()"})
	k6 := targz(t, map[string]string{"load.js": "export default function () {}"})
	run := func(in NewScriptRun) (ScriptRun, error) {
		in.ProjectID, in.OrgID, in.EnvironmentID, in.Actor = f.project, f.org, f.env, "tia"
		return f.svc.CreateScriptRun(ctx, in)
	}

	for name, bad := range map[string]NewScriptRun{
		"not an archive":     {Kind: "playwright", Archive: []byte("nope")},
		"no config":          {Kind: "playwright", Archive: k6},
		"unknown kind":       {Kind: "cypress", Archive: pw},
		"shell in grep":      {Kind: "playwright", Archive: pw, Grep: "`id`"},
		"vus on playwright":  {Kind: "playwright", Archive: pw, VUs: 3},
		"timeout over cap":   {Kind: "playwright", Archive: pw, TimeoutSeconds: 601},
		"k6 without entry":   {Kind: "k6", Archive: k6, VUs: 2, Seconds: 10},
		"k6 vus over cap":    {Kind: "k6", Archive: k6, Entry: "load.js", VUs: 11, Seconds: 10},
		"k6 grep":            {Kind: "k6", Archive: k6, Entry: "load.js", VUs: 1, Seconds: 10, Grep: "x"},
		"k6 timeout too low": {Kind: "k6", Archive: k6, Entry: "load.js", VUs: 1, Seconds: 60, TimeoutSeconds: 60},
	} {
		if _, err := run(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := run(NewScriptRun{Kind: "k6", Archive: k6, Entry: "load.js", VUs: 2, Seconds: 10}); !errors.Is(err, ErrNotProved) {
		t.Fatalf("k6 before a proof: %v", err)
	}

	// Playwright: queued with the config it found, one at a time.
	r, err := run(NewScriptRun{Kind: "playwright", Archive: pw, Grep: "inbox"})
	if err != nil || r.Status != RunQueued || r.Entry != "playwright.config.ts" || r.TimeoutSeconds != 600 || r.ArchiveBytes != len(pw) {
		t.Fatalf("create: %+v %v", r, err)
	}
	if _, err := run(NewScriptRun{Kind: "playwright", Archive: pw}); !errors.Is(err, ErrBusy) {
		t.Errorf("a second script at once: %v", err)
	}
	// sandboxd busy: back in the queue, nothing recorded.
	box.errs = []error{sandbox.ErrBusy}
	if _, err := f.svc.ScriptWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if again, _ := f.svc.GetScriptRun(ctx, f.project, r.ID); again.Status != RunQueued {
		t.Fatalf("after busy: %+v", again)
	}
	box.results = []sandbox.Result{{ExitCode: 1, Log: "1 failed", Files: map[string][]byte{"junit.xml": []byte(pwJUnit)}}}
	if _, err := f.svc.DB.Q(ctx).Exec(ctx, `UPDATE jobs SET run_at = now() - interval '1 hour' WHERE kind = $1`, JobScript); err != nil {
		t.Fatal(err)
	}
	if n, err := f.svc.ScriptWorker(1).Drain(ctx); err != nil || n != 1 {
		t.Fatalf("drain: %d %v", n, err)
	}
	r, _ = f.svc.GetScriptRun(ctx, f.project, r.ID)
	if r.Status != RunDone || r.Verdict != "failed" || r.Tests != 2 || r.Failures != 1 || r.ExitCode == nil || *r.ExitCode != 1 ||
		r.TestRunID != "f0000000-0000-4000-8000-000000000002" || r.Log != "1 failed" {
		t.Fatalf("done: %+v", r)
	}
	if j := box.jobs[len(box.jobs)-1]; j.ID != r.ID || j.Kind != "playwright" || j.Base != "https://chat.example.com" || j.Grep != "inbox" || !bytes.Equal(j.Archive, pw) {
		t.Errorf("sandboxd was asked: %+v", j)
	}
	if rep := f.recorder.reports[0]; rep.Framework != "playwright" || rep.LocalRunID != r.ID || string(rep.JUnit) != pwJUnit {
		t.Errorf("recorded: %+v", rep)
	}

	// A transient results-plane failure resumes report import from the durable
	// checkpoint. It must not run the team's script twice or discard its result.
	r, err = run(NewScriptRun{Kind: "playwright", Archive: pw})
	if err != nil {
		t.Fatal(err)
	}
	passing := `<testsuites><testsuite name="inbox.spec.ts">
<testcase name="shows inbox" classname="inbox.spec.ts"/><testcase name="sends a message" classname="inbox.spec.ts"/>
</testsuite></testsuites>`
	box.results = []sandbox.Result{{ExitCode: 0, Log: "2 passed", Files: map[string][]byte{"junit.xml": []byte(passing)}}}
	f.recorder.errors = []error{errors.New("results temporarily unavailable")}
	beforeJobs := len(box.jobs)
	if _, err := f.svc.ScriptWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	checkpoint, _ := f.svc.GetScriptRun(ctx, f.project, r.ID)
	if checkpoint.Status != RunRunning || checkpoint.Verdict != "passed" || checkpoint.Tests != 2 || checkpoint.Failures != 0 {
		t.Fatalf("checkpoint: %+v", checkpoint)
	}
	if _, err := f.svc.DB.Q(ctx).Exec(ctx, `UPDATE jobs SET run_at = now() - interval '1 hour' WHERE kind = $1`, JobScript); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ScriptWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	r, _ = f.svc.GetScriptRun(ctx, f.project, r.ID)
	if r.Status != RunDone || r.Verdict != "passed" || r.TestRunID == "" || len(box.jobs) != beforeJobs+1 {
		t.Fatalf("resumed report without rerunning sandbox: %+v jobs=%d before=%d", r, len(box.jobs), beforeJobs)
	}
	// Both tries sent the same report under the run's id (the import is idempotent on it).
	if n := len(f.recorder.reports); n < 2 || f.recorder.reports[n-2].LocalRunID != r.ID || f.recorder.reports[n-1].LocalRunID != r.ID ||
		!bytes.Equal(f.recorder.reports[n-2].JUnit, f.recorder.reports[n-1].JUnit) {
		t.Errorf("the retry sent another report: %d reports", n)
	}
	var left int
	if err := f.svc.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM script_runs WHERE archive IS NOT NULL`).Scan(&left); err != nil || left != 0 {
		t.Errorf("an archive outlived its run: %d %v", left, err)
	}

	// k6, once the host is proved: the run's vus and seconds go to sandboxd,
	// the summary becomes JUnit, and k6's exit code is the verdict's judge.
	loader := &fakeLoader{}
	f.svc.Loader = loader
	o, _ := f.svc.Ownership(ctx, f.project, f.org, f.env, "tia")
	loader.file = o.FileText()
	if _, err := f.svc.CheckOwnership(ctx, f.project, f.org, f.env, "file", "tia"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.OwnershipWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	r, err = run(NewScriptRun{Kind: "k6", Archive: k6, Entry: "./load.js", VUs: 4, Seconds: 20, TimeoutSeconds: 120})
	if err != nil || r.Entry != "load.js" || r.VUs != 4 {
		t.Fatalf("k6 create: %+v %v", r, err)
	}
	box.results = []sandbox.Result{{ExitCode: 99, Files: map[string][]byte{"summary.json": []byte(
		`{"metrics": {"http_req_duration": {"p(95)": 900, "thresholds": {"p(95)<500": true}}}, "root_group": {"checks": {}}}`)}}}
	if _, err := f.svc.ScriptWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	r, _ = f.svc.GetScriptRun(ctx, f.project, r.ID)
	if r.Status != RunDone || r.Verdict != "failed" || r.Tests != 1 || r.Failures != 1 {
		t.Fatalf("k6 done: %+v", r)
	}
	if j := box.jobs[len(box.jobs)-1]; j.VUs != 4 || j.Seconds != 20 || j.Entry != "load.js" || j.TimeoutSeconds != 120 {
		t.Errorf("k6 job: %+v", j)
	}
	if rep := f.recorder.reports[len(f.recorder.reports)-1]; rep.Framework != "k6" || !strings.Contains(string(rep.JUnit), "crossed p(95)&lt;500") {
		t.Errorf("k6 report: %s", rep.JUnit)
	}

	// The proof is checked again before the load: gone, nothing runs.
	loader.file = "gone"
	r, err = run(NewScriptRun{Kind: "k6", Archive: k6, Entry: "load.js", VUs: 1, Seconds: 10})
	if err != nil {
		t.Fatal(err)
	}
	asked := len(box.jobs)
	if _, err := f.svc.ScriptWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	r, _ = f.svc.GetScriptRun(ctx, f.project, r.ID)
	if r.Status != RunError || !strings.Contains(r.Error, "no longer holds") || len(box.jobs) != asked {
		t.Fatalf("a proof that went: %+v, %d jobs", r, len(box.jobs))
	}

	// A sandbox refusal or no sandboxd at all is said as is.
	r, _ = run(NewScriptRun{Kind: "playwright", Archive: pw})
	box.results = []sandbox.Result{{Error: "the sandbox image atp/sandbox-playwright:local is not on the server"}}
	_, _ = f.svc.ScriptWorker(1).Drain(ctx)
	if r, _ = f.svc.GetScriptRun(ctx, f.project, r.ID); r.Status != RunError || !strings.Contains(r.Error, "not on the server") {
		t.Errorf("refused: %+v", r)
	}
	f.svc.Sandbox = nil
	r, _ = run(NewScriptRun{Kind: "playwright", Archive: pw})
	_, _ = f.svc.ScriptWorker(1).Drain(ctx)
	if r, _ = f.svc.GetScriptRun(ctx, f.project, r.ID); r.Status != RunError || !strings.Contains(r.Error, "SANDBOX=on") {
		t.Errorf("no sandboxd: %+v", r)
	}
	if runs, err := f.svc.ScriptRuns(ctx, f.project, 10); err != nil || len(runs) != 6 {
		t.Errorf("list: %d %v", len(runs), err)
	}
	if err := f.svc.PurgeProject(ctx, f.project); err != nil {
		t.Fatal(err)
	}
	if runs, _ := f.svc.ScriptRuns(ctx, f.project, 10); len(runs) != 0 {
		t.Errorf("purge left %d script runs", len(runs))
	}
}

func TestCountJUnit(t *testing.T) {
	if tests, failures := countJUnit([]byte(pwJUnit)); tests != 2 || failures != 1 {
		t.Errorf("%d %d", tests, failures)
	}
	if tests, failures := countJUnit([]byte(`<testsuite><testcase name="a"><failure/><error/></testcase></testsuite>`)); tests != 1 || failures != 1 {
		t.Errorf("both in one case: %d %d", tests, failures)
	}
}
