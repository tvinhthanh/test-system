package autotest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// userError is a failure to tell the tester as it is: retrying cannot fix
// it. Anything else (the checker or the model unreachable) is retried by
// the queue, and told on the last attempt.
type userError struct{ msg string }

func (e *userError) Error() string { return e.msg }

func tell(format string, a ...any) error { return &userError{fmt.Sprintf(format, a...)} }

func told(err error) (string, bool) {
	var u *userError
	if errors.As(err, &u) {
		return u.msg, true
	}
	return "", false
}

// lastWord is what a job stores when it gives up: the user error, or what
// the last retry met.
func lastWord(err error, job queue.Job) (string, bool) {
	if msg, ok := told(err); ok {
		return msg, true
	}
	if job.Attempts >= job.MaxAttempts {
		return "The checker or the AI service could not be reached: " + err.Error(), true
	}
	return "", false
}

func randomText() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// outside is ctx without the job's transaction, for progress that should
// show at once (a run that started) — the job's own writes commit at its end.
func outside() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

func llmMessage(err error, acct llm.Account) string {
	switch {
	case errors.Is(err, llm.ErrKey):
		return "The API key was rejected by " + llm.Label(acct.Provider) + ". Check it under Team → AI settings (or LLM_API_KEY on the server)."
	case errors.Is(err, llm.ErrQuota):
		return "The " + llm.Label(acct.Provider) + " account behind this key is out of credit or over its spending limit. Add credit there, or use another key under Team → AI settings."
	case errors.Is(err, llm.ErrModel):
		return "This key cannot use " + acct.Model + ". Pick another model under Team → AI settings."
	case errors.Is(err, llm.ErrRefused):
		return "The model declined this request. Rephrase the description and try again."
	case errors.Is(err, llm.ErrTruncated):
		return "The answer was too long and got cut off. Ask for fewer cases."
	}
	return ""
}

// --------------------------------------------------------------- sign-in

func (s *service) markLogin(ctx context.Context, accountID, status, msg string, steps []Step) error {
	var raw []byte
	if steps != nil {
		raw = stepsJSON(steps)
	}
	_, err := s.DB.Q(ctx).Exec(ctx, `UPDATE environment_accounts SET login_status = $2, login_error = $3,
		login_steps = coalesce($4::jsonb, login_steps), login_checked_at = clock_timestamp() WHERE id = $1`, accountID, status, cut(msg, 1000), raw)
	return err
}

// findLogin asks a model to write the sign-in steps from the sign-in page,
// then proves them: a session must sign in with them.
func (s *service) findLogin(ctx context.Context, orgID string, env tenancy.Environment, a Account) ([]Step, error) {
	if s.Browser == nil || s.LLM == nil || s.Keys == nil {
		return nil, tell("the account has no sign-in steps and this server cannot find them: write them on the account")
	}
	acct, _, ok, err := s.Keys.Credentials(ctx, orgID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, tell("the account %q has no sign-in steps: write them, or set up AI (Team → AI settings) so they are found", a.Key)
	}
	start := env.BaseURL
	if a.LoginURL != "" {
		if start, err = resolveTarget(env.BaseURL, a.LoginURL); err != nil {
			return nil, tell("sign-in page: %v", err)
		}
	}
	sess, _, err := s.Browser.Open(ctx, env.BaseURL, nil)
	if err != nil {
		return nil, err
	}
	o, err := sess.Run(ctx, []Step{{Do: "goto", Value: start}}, nil)
	var view View
	if err == nil && o.OK {
		view, err = sess.Look(ctx, false)
	}
	_ = sess.Close(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	if !o.OK {
		return nil, tell("the sign-in page %s did not open: %s", start, o.Results[len(o.Results)-1].Error)
	}
	res, err := s.LLM.JSON(ctx, llm.Request{Account: acct, System: loginSystem, User: loginPrompt(view), Schema: loginSchema, MaxTokens: 8000})
	if err != nil {
		if msg := llmMessage(err, acct); msg != "" {
			return nil, tell("%s", msg)
		}
		return nil, err
	}
	steps, note, err := parseLogin(res.JSON)
	if err != nil {
		return nil, tell("the model's answer did not have the sign-in shape")
	}
	if steps == nil {
		return nil, tell("no sign-in steps found on %s: %s", view.URL, note)
	}
	pw, err := s.password(ctx, a)
	if err != nil {
		return nil, tell("%v", err)
	}
	proof, lr, err := s.Browser.Open(ctx, env.BaseURL, &Login{URL: start, Steps: steps, Vars: map[string]string{VarUsername: a.Username, VarPassword: pw}})
	if err != nil {
		return nil, err
	}
	_ = proof.Close(context.WithoutCancel(ctx))
	if !lr.LoggedIn {
		return nil, tell("the sign-in steps found did not sign in: %s", lr.Error)
	}
	return steps, nil
}

// ensureLogin returns how a session signs in as a — finding the steps first
// when there are none.
func (s *service) ensureLogin(ctx context.Context, orgID string, env tenancy.Environment, a Account) (*Login, error) {
	if len(a.LoginSteps) == 0 {
		steps, err := s.findLogin(ctx, orgID, env, a)
		if err != nil {
			if msg, ok := told(err); ok {
				if merr := s.markLogin(ctx, a.ID, LoginFailed, msg, nil); merr != nil {
					return nil, merr
				}
			}
			return nil, err
		}
		if err := s.markLogin(ctx, a.ID, LoginOK, "", steps); err != nil {
			return nil, err
		}
		a.LoginSteps = steps
	}
	return s.login(ctx, env.BaseURL, a)
}

func (s *service) LoginWorker(concurrency int) *queue.Worker {
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobLogin, Handle: s.runLogin, Concurrency: concurrency, Lease: 15 * time.Minute, Poll: 2 * time.Second}
}

func (s *service) runLogin(ctx context.Context, job queue.Job) error {
	var p struct {
		AccountID string `json:"accountId"`
		OrgID     string `json:"orgId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.AccountID) || !uuidRe.MatchString(p.OrgID) {
		return nil
	}
	a, err := scanAccount(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+accountColumns+` FROM environment_accounts WHERE id = $1 FOR UPDATE`, p.AccountID))
	if errors.Is(err, ErrNotFound) || (err == nil && a.LoginStatus != LoginFinding) {
		return nil
	}
	if err != nil {
		return err
	}
	env, err := s.siteEnvironment(ctx, a.ProjectID, a.EnvironmentID)
	if err != nil {
		return s.markLogin(ctx, a.ID, LoginFailed, "the environment has no address any more", nil)
	}
	steps, err := s.findLogin(ctx, p.OrgID, env, a)
	if err != nil {
		if msg, final := lastWord(err, job); final {
			return s.markLogin(ctx, a.ID, LoginFailed, msg, nil)
		}
		return err
	}
	return s.markLogin(ctx, a.ID, LoginOK, "", steps)
}

// open starts a session on the case's start page, signed in when account
// is set. vars gets the account's sign-in values.
func (s *service) open(ctx context.Context, orgID string, env tenancy.Environment, target, account string, vars map[string]string) (Session, Outcome, error) {
	start, err := Substitute(target, vars)
	if err == nil {
		start, err = resolveTarget(env.BaseURL, start)
	}
	if err != nil {
		return nil, Outcome{}, tell("the start page: %v", err)
	}
	var login *Login
	var acct Account
	if account != "" {
		acct, err = s.account(ctx, string(env.ProjectID), string(env.ID), account)
		if errors.Is(err, ErrNotFound) {
			return nil, Outcome{}, tell("the test account %q is no longer on %s", account, env.Key)
		}
		if err != nil {
			return nil, Outcome{}, err
		}
		if login, err = s.ensureLogin(ctx, orgID, env, acct); err != nil {
			if msg, ok := told(err); ok {
				return nil, Outcome{}, tell("could not sign in as %q: %s", account, msg)
			}
			return nil, Outcome{}, err
		}
		for k, v := range login.Vars {
			vars[k] = v
		}
	}
	if s.Browser == nil {
		return nil, Outcome{}, errors.New("autotest: no checker configured in this process")
	}
	sess, lr, err := s.Browser.Open(ctx, env.BaseURL, login)
	if err != nil {
		return nil, Outcome{}, err
	}
	if !lr.LoggedIn {
		_ = sess.Close(context.WithoutCancel(ctx))
		if acct.ID == "" {
			return nil, Outcome{}, tell("the checker did not sign in: %s", lr.Error)
		}
		if err := s.markLogin(ctx, acct.ID, LoginFailed, lr.Error, nil); err != nil {
			return nil, Outcome{}, err
		}
		return nil, Outcome{}, tell("could not sign in as %q: %s", account, lr.Error)
	}
	if account != "" && acct.LoginStatus != LoginOK {
		if err := s.markLogin(ctx, acct.ID, LoginOK, "", nil); err != nil {
			return nil, Outcome{}, err
		}
	}
	o, err := sess.Run(ctx, []Step{{Do: "goto", Value: start}}, vars)
	if err != nil {
		_ = sess.Close(context.WithoutCancel(ctx))
		return nil, Outcome{}, err
	}
	return sess, o, nil
}

// ------------------------------------------------------------------ drafts

func (s *service) DraftWorker(concurrency int) *queue.Worker {
	// Reading the page and a streamed answer of up to 32k tokens.
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobDraft, Handle: s.runDraft, Concurrency: concurrency, Lease: 15 * time.Minute, Poll: 2 * time.Second}
}

func (s *service) runDraft(ctx context.Context, job queue.Job) error {
	var p struct {
		DraftID   string `json:"draftId"`
		ProjectID string `json:"projectId"`
		OrgID     string `json:"orgId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.DraftID) || !uuidRe.MatchString(p.ProjectID) || !uuidRe.MatchString(p.OrgID) {
		return nil
	}
	q := s.DB.Q(ctx)
	d, err := scanDraft(q.QueryRow(ctx, `SELECT `+draftColumns+` FROM auto_drafts WHERE id = $1 AND project_id = $2 FOR UPDATE`, p.DraftID, p.ProjectID))
	if errors.Is(err, ErrNotFound) || (err == nil && d.Status != StatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	fail := func(msg string) error {
		_, err := q.Exec(ctx, `UPDATE auto_drafts SET status = 'error', error = $2, finished_at = clock_timestamp() WHERE id = $1`, d.ID, cut(msg, 1000))
		return err
	}
	if s.LLM == nil || s.Keys == nil {
		return errors.New("autotest: no model configured in this process")
	}
	acct, source, ok, err := s.Keys.Credentials(ctx, p.OrgID)
	if err != nil {
		return err
	}
	if !ok {
		return fail("No API key: an organization admin adds one under Team → AI settings, or the server operator sets LLM_API_KEY.")
	}
	env, err := s.siteEnvironment(ctx, d.ProjectID, d.EnvironmentID)
	if err != nil {
		return fail("The environment is gone or has no address.")
	}
	var view *View
	if s.Browser != nil {
		vars := withBuiltins(d.Inputs, nil)
		sess, o, err := s.open(ctx, p.OrgID, env, d.Target, d.Account, vars)
		if err == nil {
			if o.OK {
				v, lerr := sess.Look(ctx, false)
				if lerr == nil {
					view = &v
				}
				err = lerr
			}
			_ = sess.Close(context.WithoutCancel(ctx))
		}
		if err != nil {
			// A sign-in that does not work would have the model draft cases
			// for the sign-in page: say so instead.
			if msg, ok := told(err); ok && d.Account != "" && strings.Contains(msg, "sign in") {
				return fail(msg)
			}
			if _, ok := told(err); !ok && job.Attempts < job.MaxAttempts {
				return err
			}
			view = nil // drafted from the description; page_read says so
		}
	}
	res, err := s.LLM.JSON(ctx, llm.Request{Account: acct, System: draftSystem, User: draftPrompt(d, view), Schema: draftSchema, MaxTokens: 32000})
	if err != nil {
		if msg := llmMessage(err, acct); msg != "" {
			return fail(msg)
		}
		if job.Attempts < job.MaxAttempts {
			return err
		}
		return fail("The AI service could not be reached: " + err.Error())
	}
	drafts, err := parseDrafts(res.JSON, d.Count, d.Inputs, d.Account != "")
	if err != nil {
		return fail(err.Error())
	}
	b, err := json.Marshal(drafts)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE auto_drafts SET status = 'done', drafts = $2, page_read = $3, provider = $4, model = $5, key_source = $6,
		input_tokens = $7, output_tokens = $8, finished_at = clock_timestamp() WHERE id = $1`,
		d.ID, b, view != nil, acct.Provider, res.ServedBy(acct.Model), source, res.InputTokens, res.OutputTokens)
	return err
}

// ------------------------------------------------------------------- cases

func (s *service) CaseWorker(concurrency int) *queue.Worker {
	// Forty steps of up to ten seconds, a sign-in, and a few repairs.
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobCase, Handle: s.runCaseJob, Concurrency: concurrency, Lease: 20 * time.Minute, Poll: 2 * time.Second}
}

// caseOutcome is what a case run produced.
type caseOutcome struct {
	status      string
	results     []StepResult
	console     []string
	healed      int
	err         string
	finalShot   []byte
	failureShot []byte
	tokensIn    int
	tokensOut   int
}

func (s *service) runCaseJob(ctx context.Context, job queue.Job) error {
	var p struct {
		RunID     string `json:"runId"`
		CaseID    string `json:"caseId"`
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.RunID) || !uuidRe.MatchString(p.CaseID) || !uuidRe.MatchString(p.ProjectID) {
		return nil
	}
	q := s.DB.Q(ctx)
	rc, err := scanRunCase(q.QueryRow(ctx, `SELECT `+runCaseColumns+` FROM auto_run_cases c
		WHERE c.run_id = $1 AND c.case_id = $2 AND EXISTS (SELECT 1 FROM auto_runs r WHERE r.id = c.run_id AND r.project_id = $3)
		FOR UPDATE`, p.RunID, p.CaseID, p.ProjectID), p.ProjectID)
	if errors.Is(err, ErrNotFound) {
		return nil // pruned or purged
	}
	if err != nil {
		return err
	}
	if rc.Status != CasePending {
		return s.finishRun(ctx, p.ProjectID, p.RunID)
	}
	run, err := scanRun(q.QueryRow(ctx, `SELECT `+runColumns+` FROM auto_runs WHERE id = $1`, p.RunID))
	if err != nil {
		return err
	}
	octx, cancel := outside()
	_, _ = s.DB.Q(octx).Exec(octx, `UPDATE auto_runs SET status = 'running', started_at = coalesce(started_at, now()) WHERE id = $1 AND status = 'queued'`, run.ID)
	cancel()
	started := time.Now()
	out, err := s.execute(ctx, run, rc)
	if err != nil {
		msg, final := lastWord(err, job)
		if !final {
			return err
		}
		out = caseOutcome{status: CaseError, err: msg}
	}
	if out.results == nil {
		out.results = []StepResult{}
	}
	for len(out.results) < len(rc.Spec.Steps) {
		out.results = append(out.results, StepResult{Status: "skipped"})
	}
	if out.console == nil {
		out.console = []string{}
	}
	results, _ := json.Marshal(out.results)
	console, _ := json.Marshal(out.console)
	if _, err := q.Exec(ctx, `UPDATE auto_run_cases SET status = $3, results = $4, console = $5, healed = $6, duration_ms = $7,
		error = $8, final_shot = $9, failure_shot = $10, finished_at = clock_timestamp() WHERE run_id = $1 AND case_id = $2`,
		run.ID, rc.CaseID, out.status, results, console, out.healed, int(time.Since(started).Milliseconds()),
		nullable(cut(out.err, 1000)), jpegOrNil(out.finalShot), jpegOrNil(out.failureShot)); err != nil {
		return err
	}
	if out.tokensIn > 0 || out.tokensOut > 0 {
		if _, err := q.Exec(ctx, `UPDATE auto_runs SET input_tokens = input_tokens + $2, output_tokens = output_tokens + $3 WHERE id = $1`,
			run.ID, out.tokensIn, out.tokensOut); err != nil {
			return err
		}
	}
	if s.Recorder != nil {
		done, err := scanRunCase(q.QueryRow(ctx, `SELECT `+runCaseColumns+` FROM auto_run_cases WHERE run_id = $1 AND case_id = $2`, run.ID, rc.CaseID), p.ProjectID)
		if err != nil {
			return err
		}
		execID, err := s.Recorder.RecordCase(ctx, run, done, jpegOrNil(out.finalShot), jpegOrNil(out.failureShot))
		if err != nil {
			return err
		}
		if execID != "" {
			if _, err := q.Exec(ctx, `UPDATE auto_run_cases SET execution_id = $3 WHERE run_id = $1 AND case_id = $2`, run.ID, rc.CaseID, execID); err != nil {
				return err
			}
		}
	}
	return s.finishRun(ctx, p.ProjectID, run.ID)
}

// healable: the element was not found or could not be used — the page may
// have changed. An expectation that failed is the case's finding, never
// repaired; neither is a page off the site or a bad variable.
func healable(kind string) bool { return kind == "locator" }

// execute runs one case. An error is the checker or a model out of reach
// (retried); everything about the case itself is in the outcome.
func (s *service) execute(ctx context.Context, run Run, rc RunCase) (caseOutcome, error) {
	env, err := s.siteEnvironment(ctx, run.ProjectID, run.EnvironmentID)
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalid) {
			return caseOutcome{status: CaseError, err: "The environment is gone or has no address."}, nil
		}
		return caseOutcome{}, err
	}
	spec := rc.Spec
	vars := withBuiltins(spec.Inputs, nil)
	sess, opened, err := s.open(ctx, run.OrgID, env, spec.Target, spec.Account, vars)
	if err != nil {
		if msg, ok := told(err); ok {
			return caseOutcome{status: CaseError, err: msg}, nil
		}
		return caseOutcome{}, err
	}
	defer func() { _ = sess.Close(context.WithoutCancel(ctx)) }()
	out := caseOutcome{status: CasePassed, console: opened.Console}
	if !opened.OK {
		out.status, out.err = CaseError, "The start page did not open: "+opened.Results[len(opened.Results)-1].Error
		if opened.View != nil {
			out.failureShot = opened.View.Screenshot
		}
		return out, nil
	}
	healsLeft := 0
	if run.Heal && s.LLM != nil && s.Keys != nil {
		var used int
		if err := s.DB.Q(ctx).QueryRow(ctx, `SELECT coalesce(sum(healed), 0) FROM auto_run_cases WHERE run_id = $1`, run.ID).Scan(&used); err != nil {
			return caseOutcome{}, err
		}
		healsLeft = min(MaxHealsPerCase, MaxHealsPerRun-used)
	}
	for i, st := range spec.Steps {
		o, err := sess.Run(ctx, []Step{st}, vars)
		if err != nil {
			return caseOutcome{}, err
		}
		out.console = append(out.console, o.Console...)
		r := o.Results[0]
		if o.OK {
			out.results = append(out.results, StepResult{Status: "passed", Ms: r.Ms})
			continue
		}
		if healable(r.Kind) && healsLeft > 0 && o.View != nil {
			h, in, outTok := s.heal(ctx, run.OrgID, rc.Title, spec.Steps[:i], st, r.Error, *o.View, spec.Inputs, spec.Account != "")
			out.tokensIn, out.tokensOut = out.tokensIn+in, out.tokensOut+outTok
			if h != nil {
				healsLeft--
				o2, err := sess.Run(ctx, h.Steps, vars)
				if err != nil {
					return caseOutcome{}, err
				}
				out.console = append(out.console, o2.Console...)
				if o2.OK {
					ms := r.Ms
					for _, x := range o2.Results {
						ms += x.Ms
					}
					out.results = append(out.results, StepResult{Status: "passed", Ms: ms, Error: r.Error, Healed: h})
					out.healed++
					continue
				}
				last := o2.Results[len(o2.Results)-1]
				r.Error = fmt.Sprintf("%s (an AI repair — %s — failed too: %s)", r.Error, h.Reason, last.Error)
				if o2.View != nil {
					o.View = o2.View
				}
			}
		}
		out.results = append(out.results, StepResult{Status: "failed", Kind: r.Kind, Error: r.Error, Ms: r.Ms})
		out.status = CaseFailed
		if r.Kind == "input" || r.Kind == "page" {
			out.status = CaseError
		}
		out.err = fmt.Sprintf("Step %d: %s", i+1, r.Error)
		if o.View != nil {
			out.failureShot = o.View.Screenshot
		}
		break
	}
	if v, err := sess.Look(ctx, true); err == nil {
		out.finalShot = v.Screenshot
	}
	if len(out.console) > 50 {
		out.console = out.console[:50]
	}
	return out, nil
}

// heal asks the model for steps to put in place of one that lost its
// element; nil when it gives up, or on any trouble (a repair is a bonus).
func (s *service) heal(ctx context.Context, orgID, title string, done []Step, failed Step, errText string, view View, inputs map[string]string, account bool) (*Heal, int, int) {
	acct, _, ok, err := s.Keys.Credentials(ctx, orgID)
	if err != nil || !ok {
		return nil, 0, 0
	}
	res, err := s.LLM.JSON(ctx, llm.Request{Account: acct, System: healSystem, User: healPrompt(title, done, failed, errText, view), Schema: healSchema, MaxTokens: 8000})
	if err != nil {
		return nil, 0, 0
	}
	h, err := parseHeal(res.JSON, inputs, account)
	if err != nil {
		h = nil
	}
	return h, int(res.InputTokens), int(res.OutputTokens)
}

// finishRun closes the run when no case is left pending. The run row is
// locked first, so of two cases finishing together exactly one sees the
// other's result and closes it.
func (s *service) finishRun(ctx context.Context, projectID, runID string) error {
	q := s.DB.Q(ctx)
	run, err := scanRun(q.QueryRow(ctx, `SELECT `+runColumns+` FROM auto_runs WHERE id = $1 AND project_id = $2 FOR UPDATE`, runID, projectID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if run.Status != RunQueued && run.Status != RunRunning {
		return nil
	}
	var pending, passed, failed, errored, healed int
	if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'pending'), count(*) FILTER (WHERE status = 'passed'),
		count(*) FILTER (WHERE status = 'failed'), count(*) FILTER (WHERE status = 'error'), coalesce(sum(healed), 0)
		FROM auto_run_cases WHERE run_id = $1`, runID).Scan(&pending, &passed, &failed, &errored, &healed); err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	if _, err := q.Exec(ctx, `UPDATE auto_runs SET status = 'done', passed = $2, failed = $3, errored = $4, healed = $5,
		started_at = coalesce(started_at, clock_timestamp()), finished_at = clock_timestamp() WHERE id = $1`, runID, passed, failed, errored, healed); err != nil {
		return err
	}
	if s.Recorder == nil {
		return nil
	}
	full, err := s.GetRun(ctx, projectID, runID)
	if err != nil {
		return err
	}
	testRunID, err := s.Recorder.RecordRun(ctx, full)
	if err != nil {
		return err
	}
	if testRunID != "" {
		_, err = q.Exec(ctx, `UPDATE auto_runs SET test_run_id = $2 WHERE id = $1`, runID, testRunID)
	}
	return err
}
