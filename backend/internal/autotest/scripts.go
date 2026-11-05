package autotest

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/sandbox"
)

// Script runs (QĐ-19 tier 4): a team's own Playwright project or k6 script,
// uploaded as a .tar.gz, run by sandboxd in a container that reaches only
// the environment's host. The archive is checked on upload, kept only until
// the run ends, and its report goes through the JUnit import like any CI
// run. A k6 script sends load, so it needs the ownership proof of tier 3,
// checked again right before it runs, and the load caps.

const (
	// MaxActiveScripts per organization (queued or running); DailyScripts per project.
	MaxActiveScripts = 1
	DailyScripts     = 30
)

// ScriptCaps: the longest a script may run (SCRIPT_MAX_SECONDS).
type ScriptCaps struct {
	MaxSeconds int
}

func (c ScriptCaps) orDefault() ScriptCaps {
	if c.MaxSeconds <= 0 {
		c.MaxSeconds = 900
	}
	return c
}

type NewScriptRun struct {
	ProjectID      string
	OrgID          string
	EnvironmentID  string
	Kind           string // playwright | k6
	Archive        []byte // .tar.gz
	Entry          string // the k6 script; the Playwright config ("" = playwright.config.* at the top)
	Project        string // Playwright --project
	Grep           string // Playwright --grep
	VUs            int    // k6
	Seconds        int    // k6
	TimeoutSeconds int    // 0 = the server's cap
	Actor          string
}

type ScriptRun struct {
	ID             string
	ProjectID      string
	EnvironmentID  string
	Kind           string
	Base           string
	Entry          string
	Project        string
	Grep           string
	VUs            int
	Seconds        int
	TimeoutSeconds int
	ArchiveBytes   int
	Status         string // queued | running | done | error
	Verdict        string // "" | passed | failed
	ExitCode       *int
	TimedOut       bool
	Tests          int
	Failures       int
	Log            string
	Error          string
	TestRunID      string
	RequestedBy    string
	CreatedAt      time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
}

// Report is a run's JUnit report on its way to the results plane.
type Report struct {
	ProjectID     string
	EnvironmentID string
	Framework     string // playwright | k6
	LocalRunID    string
	StartedAt     time.Time
	Source        string
	JUnit         []byte
}

const scriptColumns = `id, project_id, environment_id, kind, base, entry, project, grep, coalesce(vus, 0), coalesce(seconds, 0), timeout_seconds,
	archive_bytes, status, verdict, exit_code, timed_out, tests, failures, log, error, coalesce(test_run_id::text, ''), requested_by,
	created_at, started_at, finished_at`

func scanScript(row interface{ Scan(dest ...any) error }) (ScriptRun, error) {
	var r ScriptRun
	var started, finished *time.Time
	err := row.Scan(&r.ID, &r.ProjectID, &r.EnvironmentID, &r.Kind, &r.Base, &r.Entry, &r.Project, &r.Grep, &r.VUs, &r.Seconds, &r.TimeoutSeconds,
		&r.ArchiveBytes, &r.Status, &r.Verdict, &r.ExitCode, &r.TimedOut, &r.Tests, &r.Failures, &r.Log, &r.Error, &r.TestRunID, &r.RequestedBy,
		&r.CreatedAt, &started, &finished)
	if errors.Is(err, pg.ErrNoRows) {
		return ScriptRun{}, ErrNotFound
	}
	if err != nil {
		return ScriptRun{}, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	if started != nil {
		r.StartedAt = started.UTC()
	}
	if finished != nil {
		r.FinishedAt = finished.UTC()
	}
	return r, nil
}

func (s *service) CreateScriptRun(ctx context.Context, in NewScriptRun) (ScriptRun, error) {
	if !uuidRe.MatchString(in.OrgID) {
		return ScriptRun{}, ErrNotFound
	}
	env, err := s.siteEnvironment(ctx, in.ProjectID, in.EnvironmentID)
	if err != nil {
		return ScriptRun{}, err
	}
	host, err := hostOf(env.BaseURL)
	if err != nil {
		return ScriptRun{}, invalid("%v", err)
	}
	if isIPHost(host) {
		return ScriptRun{}, invalid("scripts run against an environment addressed by name, not by IP")
	}
	sum, err := sandbox.CheckArchive(in.Archive, in.Kind, in.Entry)
	if err != nil {
		return ScriptRun{}, invalid("%s", strings.TrimPrefix(err.Error(), sandbox.ErrArchive.Error()+": "))
	}
	project, err := sandbox.CleanFilter("project", in.Project)
	if err == nil {
		in.Grep, err = sandbox.CleanFilter("grep", in.Grep)
	}
	if err != nil {
		return ScriptRun{}, invalid("%s", strings.TrimPrefix(err.Error(), sandbox.ErrArchive.Error()+": "))
	}
	caps := s.ScriptCaps.orDefault()
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = caps.MaxSeconds
	}
	if in.TimeoutSeconds < 30 || in.TimeoutSeconds > caps.MaxSeconds {
		return ScriptRun{}, invalid("timeoutSeconds: 30 to %d on this server (SCRIPT_MAX_SECONDS)", caps.MaxSeconds)
	}
	var vus, secs *int
	if in.Kind == sandbox.KindK6 {
		load := s.LoadCaps.orDefault()
		switch {
		case in.VUs < 1 || in.VUs > load.MaxVUs:
			return ScriptRun{}, invalid("vus: 1 to %d on this server (LOAD_MAX_VUS)", load.MaxVUs)
		case in.Seconds < 5 || in.Seconds > load.MaxSeconds:
			return ScriptRun{}, invalid("seconds: 5 to %d on this server (LOAD_MAX_SECONDS)", load.MaxSeconds)
		case in.Seconds+30 > in.TimeoutSeconds:
			return ScriptRun{}, invalid("timeoutSeconds must leave k6 30 s more than seconds")
		}
		vus, secs = &in.VUs, &in.Seconds
		if project != "" || in.Grep != "" {
			return ScriptRun{}, invalid("project and grep are Playwright's")
		}
	} else if in.VUs != 0 || in.Seconds != 0 {
		return ScriptRun{}, invalid("vus and seconds are k6's; a Playwright project sets its own workers")
	}
	actor, err := validActor(in.Actor)
	if err != nil {
		return ScriptRun{}, err
	}
	var out ScriptRun
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('autotest.script:' || $1, 0))`, in.OrgID); err != nil {
			return err
		}
		if in.Kind == sandbox.KindK6 {
			var status string
			err := q.QueryRow(ctx, `SELECT status FROM site_ownership WHERE org_id = $1 AND host = $2`, in.OrgID, host).Scan(&status)
			if errors.Is(err, pg.ErrNoRows) || (err == nil && status != OwnershipVerified) {
				return fmt.Errorf("%w: %s", ErrNotProved, host)
			}
			if err != nil {
				return err
			}
		}
		var active, today int
		if err := q.QueryRow(ctx, `SELECT (SELECT count(*) FROM script_runs WHERE org_id = $1 AND status IN ('queued', 'running')),
			(SELECT count(*) FROM script_runs WHERE project_id = $2 AND created_at > now() - interval '24 hours')`, in.OrgID, in.ProjectID).Scan(&active, &today); err != nil {
			return err
		}
		if active >= MaxActiveScripts || today >= DailyScripts {
			return ErrBusy
		}
		out, err = scanScript(q.QueryRow(ctx, `INSERT INTO script_runs (project_id, org_id, environment_id, kind, base, entry, project, grep, vus, seconds,
			timeout_seconds, archive, archive_bytes, requested_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING `+scriptColumns,
			in.ProjectID, in.OrgID, in.EnvironmentID, in.Kind, env.BaseURL, sum.Entry, project, in.Grep, vus, secs, in.TimeoutSeconds, in.Archive, len(in.Archive), actor))
		if err != nil {
			return err
		}
		// Busy sandboxd: tried again a few times, a minute or so apart.
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobScript, Key: "script/" + out.ID, MaxAttempts: 5,
			Payload: map[string]string{"scriptRunId": out.ID, "orgId": in.OrgID}})
		return err
	})
	return out, err
}

func isIPHost(h string) bool {
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	_, err := netip.ParseAddr(strings.Trim(h, "[]"))
	return err == nil
}

func (s *service) ScriptRuns(ctx context.Context, projectID string, limit int) ([]ScriptRun, error) {
	if !uuidRe.MatchString(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.DB.Q(ctx).Query(ctx, `SELECT `+scriptColumns+` FROM script_runs WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptRun{}
	for rows.Next() {
		r, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *service) GetScriptRun(ctx context.Context, projectID, id string) (ScriptRun, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return ScriptRun{}, ErrNotFound
	}
	return scanScript(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+scriptColumns+` FROM script_runs WHERE id = $1 AND project_id = $2`, id, projectID))
}

func (s *service) ScriptWorker(concurrency int) *queue.Worker {
	lease := time.Duration(s.ScriptCaps.orDefault().MaxSeconds)*time.Second + 10*time.Minute
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobScript, Handle: s.runScriptJob, Concurrency: concurrency, Lease: lease, Poll: 2 * time.Second}
}

// stillOwned checks an organization's proof for host again, right before
// traffic; failMsg is set when it no longer holds (the proof is marked
// failed).
func (s *service) stillOwned(ctx context.Context, orgID, host, base string) (failMsg string, err error) {
	q := s.DB.Q(ctx)
	o, err := scanOwnership(q.QueryRow(ctx, `SELECT `+ownershipColumns+` FROM site_ownership WHERE org_id = $1 AND host = $2`, orgID, host))
	if errors.Is(err, ErrNotFound) || (err == nil && o.Status != OwnershipVerified) {
		return "the organization has not proved it owns " + host, nil
	}
	if err != nil {
		return "", err
	}
	if err := s.prove(ctx, o, base); err != nil {
		msg, ok := told(err)
		if !ok {
			msg = "the ownership proof could not be checked: " + err.Error()
		}
		if _, uerr := q.Exec(ctx, `UPDATE site_ownership SET status = 'failed', error = $2, checked_at = clock_timestamp() WHERE id = $1`, o.ID, cut(msg, 1000)); uerr != nil {
			return "", uerr
		}
		return "the ownership proof no longer holds: " + msg, nil
	}
	return "", nil
}

func (s *service) runScriptJob(ctx context.Context, job queue.Job) error {
	var p struct {
		ScriptRunID string `json:"scriptRunId"`
		OrgID       string `json:"orgId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.ScriptRunID) || !uuidRe.MatchString(p.OrgID) {
		return nil
	}
	q := s.DB.Q(ctx)
	r, err := scanScript(q.QueryRow(ctx, `SELECT `+scriptColumns+` FROM script_runs WHERE id = $1`, p.ScriptRunID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	fail := func(msg string) error {
		_, err := q.Exec(ctx, `UPDATE script_runs SET status = 'error', error = $2, archive = NULL, started_at = coalesce(started_at, clock_timestamp()),
			finished_at = clock_timestamp() WHERE id = $1`, r.ID, cut(msg, 1000))
		return err
	}
	switch r.Status {
	case RunQueued:
	case RunRunning: // the worker stopped mid-run, and the lease ran out
		return fail("the worker stopped while the script ran; run it again")
	default:
		return nil
	}
	if s.Sandbox == nil {
		return fail("scripts run in sandboxd, which this server does not have (SANDBOX=on)")
	}
	host, err := hostOf(r.Base)
	if err != nil {
		return fail(err.Error())
	}
	if r.Kind == sandbox.KindK6 {
		msg, err := s.stillOwned(ctx, p.OrgID, host, r.Base)
		if err != nil {
			return err
		}
		if msg != "" {
			return fail(msg)
		}
	}
	var archive []byte
	if err := q.QueryRow(ctx, `SELECT archive FROM script_runs WHERE id = $1`, r.ID).Scan(&archive); err != nil {
		return err
	}
	if len(archive) == 0 {
		return fail("the script's archive is gone")
	}
	octx, cancel := outside()
	_, _ = s.DB.Q(octx).Exec(octx, `UPDATE script_runs SET status = 'running', started_at = clock_timestamp() WHERE id = $1`, r.ID)
	cancel()
	started := s.now().UTC()
	res, err := s.Sandbox.Run(ctx, sandbox.Job{ID: r.ID, Kind: r.Kind, Base: r.Base, Archive: archive, Entry: r.Entry, Project: r.Project, Grep: r.Grep,
		VUs: r.VUs, Seconds: r.Seconds, TimeoutSeconds: r.TimeoutSeconds})
	if errors.Is(err, sandbox.ErrBusy) && job.Attempts < job.MaxAttempts {
		octx, cancel := outside()
		_, _ = s.DB.Q(octx).Exec(octx, `UPDATE script_runs SET status = 'queued', started_at = NULL WHERE id = $1`, r.ID)
		cancel()
		return err // tried again after the backoff
	}
	if err != nil {
		return fail("sandboxd could not run the script: " + err.Error())
	}
	if res.Error != "" {
		return fail(res.Error)
	}
	framework, report := r.Kind, res.Files["junit.xml"]
	if r.Kind == sandbox.KindK6 {
		report = sandbox.K6JUnit(r.Entry, res.Files["summary.json"], res.ExitCode, res.Log)
	} else if len(report) == 0 {
		why := fmt.Sprintf("Playwright exited %d without a JUnit report", res.ExitCode)
		if res.TimedOut {
			why = "the run reached its time limit and was stopped"
		}
		report = sandbox.RunError(r.Kind, r.Entry, why, res.Log)
	}
	tests, failures := countJUnit(report)
	verdict := "passed"
	if res.ExitCode != 0 || res.TimedOut || failures > 0 {
		verdict = "failed"
	}
	testRunID := ""
	if s.Recorder != nil {
		testRunID, err = s.Recorder.RecordReport(ctx, Report{ProjectID: r.ProjectID, EnvironmentID: r.EnvironmentID, Framework: framework,
			LocalRunID: r.ID, StartedAt: started, Source: "script:" + r.RequestedBy, JUnit: report})
		if err != nil {
			return err
		}
	}
	_, err = q.Exec(ctx, `UPDATE script_runs SET status = 'done', verdict = $2, exit_code = $3, timed_out = $4, tests = $5, failures = $6, log = $7,
		test_run_id = $8, archive = NULL, finished_at = clock_timestamp() WHERE id = $1`,
		r.ID, verdict, res.ExitCode, res.TimedOut, tests, failures, sandbox.Tail(res.Log, sandbox.MaxLogBytes), nullable(testRunID))
	return err
}

// countJUnit counts testcases and those that failed or erred.
func countJUnit(report []byte) (tests, failures int) {
	dec := xml.NewDecoder(bytes.NewReader(report))
	inCase, bad := false, false
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) || err != nil {
			return tests, failures
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "testcase":
				inCase, bad = true, false
				tests++
			case "failure", "error":
				if inCase && !bad {
					bad = true
					failures++
				}
			}
		case xml.EndElement:
			if t.Name.Local == "testcase" {
				inCase = false
			}
		}
	}
}
