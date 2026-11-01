package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/autotest"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
)

// recorder writes an auto run where people read results (QĐ-16): each case
// as an execution of the run's testmgmt plan, with its screenshots as
// evidence, and the finished run as a test run through the JUnit import —
// its tests linked to the cases, so the release gate and flaky detection
// read it like any CI run. A plan someone closed, or a case taken out of
// it, only skips the plan record.
type recorder struct {
	tm      testmgmt.Service
	art     artifact.Service
	tenancy tenancy.Service
	ingest  ingest.Deps
	jobs    *ingest.Jobs // the tier-2 resolve, run at once so the tests can be linked now
	results results.Service
	logger  *slog.Logger
}

var _ autotest.Recorder = recorder{}

func (r recorder) RecordCase(ctx context.Context, run autotest.Run, c autotest.RunCase, finalShot, failureShot []byte) (string, error) {
	if run.PlanID == "" {
		return "", nil
	}
	tc, err := r.tm.GetTestCase(ctx, run.ProjectID, c.CaseID)
	if errors.Is(err, testmgmt.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	status := map[string]testmgmt.ExecutionStatus{autotest.CasePassed: testmgmt.ExecPassed, autotest.CaseFailed: testmgmt.ExecFailed}[c.Status]
	if status == "" {
		status = testmgmt.ExecBlocked // it could not run
	}
	in := testmgmt.NewExecution{TestCaseID: c.CaseID, Status: status, Note: caseNote(run, c), ExecutedAt: c.FinishedAt}
	// Step results when the case still has the run's steps one for one.
	if len(tc.Steps) == len(c.Results) {
		for i, res := range c.Results {
			sr := testmgmt.StepResult{StepIndex: i, Status: testmgmt.ExecutionStatus(res.Status)}
			switch {
			case res.Healed != nil:
				sr.Actual = "Repaired by AI: " + res.Healed.Reason
			case res.Status == "failed":
				sr.Actual = res.Error
				if c.Status == autotest.CaseError {
					sr.Status = testmgmt.ExecBlocked
				}
			}
			if len(sr.Actual) > 4000 {
				sr.Actual = sr.Actual[:4000]
			}
			in.StepResults = append(in.StepResults, sr)
		}
	}
	actor := "ATP auto test · " + run.RequestedBy
	if len(actor) > 254 {
		actor = actor[:254]
	}
	exec, err := r.tm.RecordExecution(ctx, run.ProjectID, run.PlanID, in, actor)
	if errors.Is(err, testmgmt.ErrConflict) || errors.Is(err, testmgmt.ErrNotFound) {
		return "", nil // the plan was closed, or the case left it
	}
	if err != nil {
		return "", err
	}
	plan := "free"
	if scope, err := r.tenancy.ProjectScope(ctx, tenancy.ProjectID(run.ProjectID)); err == nil {
		if org, err := r.tenancy.GetOrganization(ctx, scope.OrgID); err == nil && org.PlanID != "" {
			plan = org.PlanID
		}
	}
	for _, shot := range []struct {
		name string
		b    []byte
	}{{"auto-test-failure.jpg", failureShot}, {"auto-test-end.jpg", finalShot}} {
		if len(shot.b) == 0 {
			continue
		}
		_, err := r.art.PutEvidence(ctx, artifact.Evidence{ProjectID: run.ProjectID, ExecutionID: exec.ID, Plan: plan, Name: shot.name, ContentType: "image/jpeg"}, shot.b)
		var tooLarge *artifact.TooLargeError
		switch {
		case err == nil:
		case errors.Is(err, artifact.ErrStore), errors.Is(err, artifact.ErrInvalid), errors.As(err, &tooLarge):
			r.logger.Warn("auto test evidence not kept", "run", run.ID, "case", c.CaseID, "err", err)
		default:
			return "", err
		}
	}
	return exec.ID, nil
}

// caseNote is the execution's note: what ran, and what went wrong.
func caseNote(run autotest.Run, c autotest.RunCase) string {
	note := fmt.Sprintf("Ran by ATP auto test (run %s, asked by %s).", run.ID[:8], run.RequestedBy)
	if c.Error != "" {
		note += " " + c.Error
	}
	if c.Healed > 0 {
		note += fmt.Sprintf(" %d step(s) repaired by AI — review them on the run page.", c.Healed)
	}
	if len(note) > 4000 {
		note = note[:4000]
	}
	return note
}

func (r recorder) RecordRun(ctx context.Context, run autotest.Run) (string, error) {
	envs, err := r.tenancy.ListEnvironments(ctx, tenancy.ProjectID(run.ProjectID))
	if err != nil {
		return "", err
	}
	key := ""
	for _, e := range envs {
		if string(e.ID) == run.EnvironmentID {
			key = e.Key
		}
	}
	if key == "" {
		return "", nil // the environment went
	}
	res, err := r.ingest.ImportJUnit(ctx, tenancy.ProjectID(run.ProjectID), ingest.JUnitImport{
		EnvironmentKey: key, Framework: autotest.Framework, LocalRunID: run.ID, StartedAt: run.StartedAt,
		Source: "autotest:" + run.RequestedBy,
	}, autotest.JUnit(run, key))
	if errors.Is(err, ingest.ErrJUnit) || errors.Is(err, ingest.ErrImportInvalid) || errors.Is(err, ingest.ErrImportConflict) {
		r.logger.Error("auto run not recorded as a test run", "run", run.ID, "err", err)
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// Link each case to its test (filed under autotest.CaseFile): its runs
	// then carry the case's history, flaky signal and gate rules. A first
	// run's tests have no identity until the tier-2 resolve: run it now (the
	// queued one finds nothing left to do).
	if r.jobs != nil {
		if _, err := r.jobs.ResolveRun(ctx, run.ProjectID, results.RunID(res.RunID)); err != nil {
			return "", err
		}
	}
	found, err := r.results.ListResults(ctx, results.RunID(res.RunID))
	if err != nil {
		return "", err
	}
	for _, tr := range found {
		if tr.StableTestID == "" {
			continue // the identity resolver gets to it later; linked next run
		}
		var sig identity.Signals
		if json.Unmarshal(tr.IdentityRaw, &sig) != nil || !strings.HasPrefix(sig.FilePath, autotest.CaseFile("")) {
			continue
		}
		caseID := strings.TrimPrefix(sig.FilePath, autotest.CaseFile(""))
		tc, err := r.tm.GetTestCase(ctx, run.ProjectID, caseID)
		if err != nil {
			continue
		}
		linked := false
		for _, l := range tc.Links {
			linked = linked || l.StableTestID == tr.StableTestID
		}
		if linked {
			continue
		}
		if _, err := r.tm.LinkAutomation(ctx, run.ProjectID, caseID, tr.StableTestID, testmgmt.LinkExact, "ATP auto test"); err != nil &&
			!errors.Is(err, testmgmt.ErrConflict) && !errors.Is(err, testmgmt.ErrNotFound) {
			return "", err
		}
	}
	return res.RunID, nil
}

// scanLogins hands a site scan an environment test account's sign-in
// (urlcheck.Logins over autotest).
type scanLogins struct{ auto autotest.Service }

func (s scanLogins) Login(ctx context.Context, projectID, environmentID, account string) (*urlcheck.Login, error) {
	l, err := s.auto.LoginFor(ctx, projectID, environmentID, account)
	if err != nil || l == nil {
		if errors.Is(err, autotest.ErrNotFound) {
			return nil, nil // the account went: the page is checked signed out
		}
		return nil, err
	}
	steps, err := json.Marshal(l.Steps)
	if err != nil {
		return nil, err
	}
	return &urlcheck.Login{URL: l.URL, Steps: steps, Vars: l.Vars}, nil
}
