package autotest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/secretbox"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

type service struct {
	Deps
	q   *queue.Queue
	box *secretbox.Box
	now func() time.Time
}

// New returns the service. In the api, Browser, LLM and Recorder are nil:
// it only stores and queues; cmd/worker runs the jobs.
func New(d Deps) Service {
	return &service{Deps: d, q: queue.New(d.DB), box: secretbox.New(d.Pepper, "atp environment account password v1"), now: time.Now}
}

var (
	uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	keyRe  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func validKey(k string) bool { return keyRe.MatchString(k) && len(k) <= 40 }

func validActor(a string) (string, error) {
	a = strings.TrimSpace(a)
	if a == "" || len(a) > 254 {
		return "", invalid("actor")
	}
	return a, nil
}

// environment returns the project's environment, ErrNotFound otherwise.
func (s *service) environment(ctx context.Context, projectID, environmentID string) (tenancy.Environment, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(environmentID) {
		return tenancy.Environment{}, ErrNotFound
	}
	envs, err := s.Tenancy.ListEnvironments(ctx, tenancy.ProjectID(projectID))
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			return tenancy.Environment{}, ErrNotFound
		}
		return tenancy.Environment{}, err
	}
	for _, e := range envs {
		if string(e.ID) == environmentID {
			return e, nil
		}
	}
	return tenancy.Environment{}, ErrNotFound
}

// siteEnvironment is environment, with an address to test (QĐ-14).
func (s *service) siteEnvironment(ctx context.Context, projectID, environmentID string) (tenancy.Environment, error) {
	e, err := s.environment(ctx, projectID, environmentID)
	if err == nil && e.BaseURL == "" {
		err = invalid("environment %s has no address: add one first", e.Key)
	}
	return e, err
}

// --------------------------------------------------------------- accounts

const accountColumns = `id, project_id, environment_id, key, username, login_url, login_steps, login_status, login_error,
	login_checked_at, updated_by, updated_at`

func scanAccount(row interface{ Scan(dest ...any) error }) (Account, error) {
	var a Account
	var steps []byte
	var checked *time.Time
	err := row.Scan(&a.ID, &a.ProjectID, &a.EnvironmentID, &a.Key, &a.Username, &a.LoginURL, &steps, &a.LoginStatus, &a.LoginError,
		&checked, &a.UpdatedBy, &a.UpdatedAt)
	if errors.Is(err, pg.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, err
	}
	if steps != nil {
		a.LoginSteps = []Step{}
		_ = json.Unmarshal(steps, &a.LoginSteps)
	}
	if checked != nil {
		a.LoginCheckedAt = checked.UTC()
	}
	a.UpdatedAt = a.UpdatedAt.UTC()
	return a, nil
}

func (s *service) Accounts(ctx context.Context, projectID, environmentID string) ([]Account, error) {
	if _, err := s.environment(ctx, projectID, environmentID); err != nil {
		return nil, err
	}
	rows, err := s.DB.Q(ctx).Query(ctx, `SELECT `+accountColumns+` FROM environment_accounts
		WHERE project_id = $1 AND environment_id = $2 ORDER BY key`, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *service) account(ctx context.Context, projectID, environmentID, key string) (Account, error) {
	if !validKey(key) {
		return Account{}, ErrNotFound
	}
	return scanAccount(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+accountColumns+` FROM environment_accounts
		WHERE project_id = $1 AND environment_id = $2 AND key = $3`, projectID, environmentID, key))
}

// validLoginURL accepts "" (the environment's address), a path, or an
// address on the environment's site.
func validLoginURL(base, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 {
		return "", invalid("loginUrl is too long")
	}
	if _, err := resolveTarget(base, raw); err != nil {
		return "", invalid("loginUrl: %v", err)
	}
	return raw, nil
}

func validSteps(steps []Step, max int) ([]Step, error) {
	if len(steps) > max {
		return nil, invalid("at most %d steps", max)
	}
	out := make([]Step, 0, len(steps))
	for i, st := range steps {
		st = st.clean()
		if err := st.Validate(); err != nil {
			return nil, invalid("step %d: %v", i+1, err)
		}
		out = append(out, st)
	}
	return out, nil
}

func (s *service) SaveAccount(ctx context.Context, projectID, environmentID string, in AccountInput) (Account, error) {
	env, err := s.environment(ctx, projectID, environmentID)
	if err != nil {
		return Account{}, err
	}
	in.Key = strings.TrimSpace(in.Key)
	if !validKey(in.Key) {
		return Account{}, invalid("key: lower-case letters, digits and single hyphens, at most 40")
	}
	in.Username = strings.TrimSpace(in.Username)
	if in.Username == "" || len(in.Username) > 254 {
		return Account{}, invalid("username: 1 to 254 characters")
	}
	if len(in.Password) > 1000 {
		return Account{}, invalid("password: at most 1000 bytes")
	}
	loginURL, err := validLoginURL(env.BaseURL, in.LoginURL)
	if err != nil {
		return Account{}, err
	}
	actor, err := validActor(in.Actor)
	if err != nil {
		return Account{}, err
	}
	var steps []byte
	keepSteps := in.LoginSteps == nil
	if !keepSteps && len(*in.LoginSteps) > 0 {
		clean, err := validSteps(*in.LoginSteps, MaxSteps)
		if err != nil {
			return Account{}, err
		}
		if err := checkVars("", clean, nil, true); err != nil {
			return Account{}, invalid("loginSteps: %v", err)
		}
		steps = stepsJSON(clean)
	}
	ad := environmentID + "/" + in.Key
	var out Account
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		cur, err := s.account(ctx, projectID, environmentID, in.Key)
		switch {
		case errors.Is(err, ErrNotFound):
			if in.Password == "" {
				return invalid("password is required for a new account")
			}
			var n int
			if err := q.QueryRow(ctx, `SELECT count(*) FROM environment_accounts WHERE environment_id = $1`, environmentID).Scan(&n); err != nil {
				return err
			}
			if n >= MaxAccounts {
				return invalid("an environment has at most %d test accounts", MaxAccounts)
			}
			out, err = scanAccount(q.QueryRow(ctx, `INSERT INTO environment_accounts
				(project_id, environment_id, key, username, password_sealed, login_url, login_steps, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+accountColumns,
				projectID, environmentID, in.Key, in.Username, s.box.Seal(ad, []byte(in.Password)), loginURL, steps, actor))
			return err
		case err != nil:
			return err
		}
		// Anything that changes how the account signs in makes its state
		// unknown again, until the next use proves it.
		changed := in.Password != "" || in.Username != cur.Username || loginURL != cur.LoginURL || !keepSteps
		status, errText := cur.LoginStatus, cur.LoginError
		if changed {
			status, errText = LoginUnknown, ""
		}
		var sealed []byte
		if in.Password != "" {
			sealed = s.box.Seal(ad, []byte(in.Password))
		}
		out, err = scanAccount(q.QueryRow(ctx, `UPDATE environment_accounts SET username = $2, login_url = $3,
			password_sealed = coalesce($4, password_sealed),
			login_steps = CASE WHEN $5 THEN login_steps ELSE $6::jsonb END,
			login_status = $7, login_error = $8, updated_by = $9, updated_at = now()
			WHERE id = $1 RETURNING `+accountColumns,
			cur.ID, in.Username, loginURL, sealed, keepSteps, steps, status, errText, actor))
		return err
	})
	return out, err
}

func (s *service) DeleteAccount(ctx context.Context, projectID, environmentID, key string) (bool, error) {
	if _, err := s.environment(ctx, projectID, environmentID); err != nil {
		return false, err
	}
	tag, err := s.DB.Q(ctx).Exec(ctx, `DELETE FROM environment_accounts WHERE project_id = $1 AND environment_id = $2 AND key = $3`,
		projectID, environmentID, key)
	return err == nil && tag.RowsAffected() > 0, err
}

func (s *service) FindLogin(ctx context.Context, projectID, orgID, environmentID, key string) (Account, error) {
	if !uuidRe.MatchString(orgID) {
		return Account{}, ErrNotFound
	}
	if _, err := s.siteEnvironment(ctx, projectID, environmentID); err != nil {
		return Account{}, err
	}
	if err := s.keysAvailable(ctx, orgID); err != nil {
		return Account{}, err
	}
	var out Account
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		a, err := scanAccount(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+accountColumns+` FROM environment_accounts
			WHERE project_id = $1 AND environment_id = $2 AND key = $3 FOR UPDATE`, projectID, environmentID, key))
		if err != nil {
			return err
		}
		if a.LoginStatus == LoginFinding {
			out = a
			return nil
		}
		out, err = scanAccount(s.DB.Q(ctx).QueryRow(ctx, `UPDATE environment_accounts SET login_status = 'finding', login_error = ''
			WHERE id = $1 RETURNING `+accountColumns, a.ID))
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobLogin, Key: a.ID, MaxAttempts: 2,
			Payload: map[string]string{"accountId": a.ID, "orgId": orgID}})
		return err
	})
	return out, err
}

// password opens an account's sealed password.
func (s *service) password(ctx context.Context, a Account) (string, error) {
	var sealed []byte
	if err := s.DB.Q(ctx).QueryRow(ctx, `SELECT password_sealed FROM environment_accounts WHERE id = $1`, a.ID).Scan(&sealed); err != nil {
		return "", err
	}
	p, err := s.box.Open(a.EnvironmentID+"/"+a.Key, sealed)
	if err != nil {
		return "", fmt.Errorf("the test account's password no longer opens (TOKEN_PEPPER changed?) — save it again")
	}
	return string(p), nil
}

// login is the checker's sign-in for a; nil steps → nil.
func (s *service) login(ctx context.Context, base string, a Account) (*Login, error) {
	if len(a.LoginSteps) == 0 {
		return nil, nil
	}
	pw, err := s.password(ctx, a)
	if err != nil {
		return nil, err
	}
	start := base
	if a.LoginURL != "" {
		if start, err = resolveTarget(base, a.LoginURL); err != nil {
			return nil, err
		}
	}
	return &Login{URL: start, Steps: a.LoginSteps, Vars: map[string]string{VarUsername: a.Username, VarPassword: pw}}, nil
}

func (s *service) LoginFor(ctx context.Context, projectID, environmentID, key string) (*Login, error) {
	env, err := s.siteEnvironment(ctx, projectID, environmentID)
	if err != nil {
		return nil, err
	}
	a, err := s.account(ctx, projectID, environmentID, key)
	if err != nil {
		return nil, err
	}
	return s.login(ctx, env.BaseURL, a)
}

func (s *service) keysAvailable(ctx context.Context, orgID string) error {
	if s.Keys == nil {
		return ErrNotConfigured
	}
	_, _, ok, err := s.Keys.Credentials(ctx, orgID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotConfigured
	}
	return nil
}

// ------------------------------------------------------------------ specs

type specJSON struct {
	Target  string            `json:"target"`
	Account string            `json:"account"`
	Inputs  map[string]string `json:"inputs"`
	Steps   []Step            `json:"steps"`
}

const specColumns = `case_id, project_id, target, account, inputs, steps, source, updated_by, updated_at`

func scanSpec(row interface{ Scan(dest ...any) error }) (Spec, error) {
	var sp Spec
	var inputs, steps []byte
	err := row.Scan(&sp.CaseID, &sp.ProjectID, &sp.Target, &sp.Account, &inputs, &steps, &sp.Source, &sp.UpdatedBy, &sp.UpdatedAt)
	if errors.Is(err, pg.ErrNoRows) {
		return Spec{}, ErrNotFound
	}
	if err != nil {
		return Spec{}, err
	}
	sp.Inputs, sp.Steps = map[string]string{}, []Step{}
	_ = json.Unmarshal(inputs, &sp.Inputs)
	_ = json.Unmarshal(steps, &sp.Steps)
	sp.UpdatedAt = sp.UpdatedAt.UTC()
	return sp, nil
}

func (s *service) Spec(ctx context.Context, projectID, caseID string) (Spec, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(caseID) {
		return Spec{}, ErrNotFound
	}
	return scanSpec(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+specColumns+` FROM auto_specs WHERE case_id = $1 AND project_id = $2`, caseID, projectID))
}

func (s *service) Specs(ctx context.Context, projectID string) ([]Spec, error) {
	if !uuidRe.MatchString(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.DB.Q(ctx).Query(ctx, `SELECT `+specColumns+` FROM auto_specs WHERE project_id = $1 ORDER BY updated_at DESC LIMIT 1000`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Spec{}
	for rows.Next() {
		sp, err := scanSpec(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// validTarget accepts a path or an http(s) address, with variables.
func validTarget(t string) (string, error) {
	t = strings.TrimSpace(t)
	if t == "" || len(t) > 2048 || strings.ContainsAny(t, " \t\r\n") {
		return "", invalid("target: a path such as /message?to={{recipient}} or an http(s) address")
	}
	probe := varRe.ReplaceAllString(t, "x")
	u, err := url.Parse(probe)
	if err != nil || (u.IsAbs() && u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (!u.IsAbs() && !strings.HasPrefix(t, "/")) {
		return "", invalid("target: a path starting with / or an http(s) address without credentials")
	}
	return t, nil
}

func cleanSpec(in SpecInput) (specJSON, error) {
	target, err := validTarget(in.Target)
	if err != nil {
		return specJSON{}, err
	}
	in.Account = strings.TrimSpace(in.Account)
	if in.Account != "" && !validKey(in.Account) {
		return specJSON{}, invalid("account: an account key of the environment")
	}
	if in.Inputs == nil {
		in.Inputs = map[string]string{}
	}
	if err := validInputs(in.Inputs); err != nil {
		return specJSON{}, invalid("inputs: %v", err)
	}
	if len(in.Steps) == 0 {
		return specJSON{}, invalid("steps: at least one")
	}
	steps, err := validSteps(in.Steps, MaxSteps)
	if err != nil {
		return specJSON{}, err
	}
	if err := checkVars(target, steps, in.Inputs, in.Account != ""); err != nil {
		return specJSON{}, invalid("%v", err)
	}
	return specJSON{Target: target, Account: in.Account, Inputs: in.Inputs, Steps: steps}, nil
}

func (s *service) SaveSpec(ctx context.Context, projectID, caseID string, in SpecInput) (Spec, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(caseID) {
		return Spec{}, ErrNotFound
	}
	sp, err := cleanSpec(in)
	if err != nil {
		return Spec{}, err
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	if in.Source != "manual" && in.Source != "ai" {
		return Spec{}, invalid("source: manual or ai")
	}
	actor, err := validActor(in.Actor)
	if err != nil {
		return Spec{}, err
	}
	inputs, _ := json.Marshal(sp.Inputs)
	return scanSpec(s.DB.Q(ctx).QueryRow(ctx, `INSERT INTO auto_specs (case_id, project_id, target, account, inputs, steps, source, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (case_id) DO UPDATE SET target = EXCLUDED.target, account = EXCLUDED.account, inputs = EXCLUDED.inputs,
			steps = EXCLUDED.steps, updated_by = EXCLUDED.updated_by, updated_at = now()
		WHERE auto_specs.project_id = EXCLUDED.project_id
		RETURNING `+specColumns, caseID, projectID, sp.Target, sp.Account, inputs, stepsJSON(sp.Steps), in.Source, actor))
}

func (s *service) DeleteSpec(ctx context.Context, projectID, caseID string) (bool, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(caseID) {
		return false, ErrNotFound
	}
	tag, err := s.DB.Q(ctx).Exec(ctx, `DELETE FROM auto_specs WHERE case_id = $1 AND project_id = $2`, caseID, projectID)
	return err == nil && tag.RowsAffected() > 0, err
}

func (s *service) ApplyHeal(ctx context.Context, projectID, runID, caseID string, index int, actor string) (Spec, error) {
	actor, err := validActor(actor)
	if err != nil {
		return Spec{}, err
	}
	var out Spec
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		rc, err := s.runCase(ctx, projectID, runID, caseID)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(rc.Results) || rc.Results[index].Healed == nil {
			return invalid("step %d of that run was not repaired", index+1)
		}
		cur, err := scanSpec(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+specColumns+` FROM auto_specs WHERE case_id = $1 AND project_id = $2 FOR UPDATE`, caseID, projectID))
		if err != nil {
			return err
		}
		if index >= len(cur.Steps) || cur.Steps[index] != rc.Spec.Steps[index] {
			return invalid("the case's step %d changed since that run", index+1)
		}
		heal := rc.Results[index].Healed.Steps
		old := cur.Steps[index]
		repl := slices.Clone(heal)
		// The first replacement keeps the words a person wrote for the step.
		if repl[0].Say == "" || old.Say != "" {
			repl[0].Say = old.Say
		}
		if repl[len(repl)-1].Expected == "" {
			repl[len(repl)-1].Expected = old.Expected
		}
		steps := slices.Concat(cur.Steps[:index], repl, cur.Steps[index+1:])
		out, err = s.SaveSpec(ctx, projectID, caseID, SpecInput{Target: cur.Target, Account: cur.Account, Inputs: cur.Inputs, Steps: steps, Source: cur.Source, Actor: actor})
		return err
	})
	return out, err
}

// ----------------------------------------------------------------- drafts

const draftColumns = `id, project_id, environment_id, target, account, inputs, context, count, test_type, status,
	coalesce(drafts, '[]'::jsonb), page_read, provider, model, key_source, input_tokens, output_tokens, coalesce(error, ''),
	requested_by, created_at, finished_at`

func scanDraft(row interface{ Scan(dest ...any) error }) (DraftRequest, error) {
	var d DraftRequest
	var inputs, drafts []byte
	var finished *time.Time
	err := row.Scan(&d.ID, &d.ProjectID, &d.EnvironmentID, &d.Target, &d.Account, &inputs, &d.Context, &d.Count, &d.TestType, &d.Status,
		&drafts, &d.PageRead, &d.Provider, &d.Model, &d.KeySource, &d.InputTokens, &d.OutputTokens, &d.Error, &d.RequestedBy, &d.CreatedAt, &finished)
	if errors.Is(err, pg.ErrNoRows) {
		return DraftRequest{}, ErrNotFound
	}
	if err != nil {
		return DraftRequest{}, err
	}
	d.Inputs, d.Drafts = map[string]string{}, []Draft{}
	_ = json.Unmarshal(inputs, &d.Inputs)
	_ = json.Unmarshal(drafts, &d.Drafts)
	d.CreatedAt = d.CreatedAt.UTC()
	if finished != nil {
		d.FinishedAt = finished.UTC()
	}
	return d, nil
}

// TestTypes a draft may be for: the plan test types (QĐ-15).
var TestTypes = []string{"smoke", "regression", "e2e", "functional", "performance", "security", "usability", "accessibility", "compatibility", "recovery"}

func (s *service) RequestDraft(ctx context.Context, in NewDraft) (DraftRequest, error) {
	if !uuidRe.MatchString(in.OrgID) {
		return DraftRequest{}, ErrNotFound
	}
	env, err := s.siteEnvironment(ctx, in.ProjectID, in.EnvironmentID)
	if err != nil {
		return DraftRequest{}, err
	}
	sp, err := cleanSpec(SpecInput{Target: in.Target, Account: in.Account, Inputs: in.Inputs,
		Steps: []Step{{Do: "expect_url", Value: "/"}}}) // the target, account and inputs; steps come later
	if err != nil {
		return DraftRequest{}, err
	}
	resolved, err := Substitute(sp.Target, withBuiltins(sp.Inputs, nil))
	if err == nil {
		_, err = resolveTarget(env.BaseURL, resolved)
	}
	if err != nil {
		return DraftRequest{}, invalid("target: %v", err)
	}
	if sp.Account != "" {
		if _, err := s.account(ctx, in.ProjectID, in.EnvironmentID, sp.Account); err != nil {
			return DraftRequest{}, invalid("account %q is not one of the environment's test accounts", sp.Account)
		}
	}
	if in.TestType != "" && !slices.Contains(TestTypes, in.TestType) {
		return DraftRequest{}, invalid("testType")
	}
	if in.Context = strings.TrimSpace(in.Context); len(in.Context) > 4000 {
		return DraftRequest{}, invalid("context is over 4000 bytes")
	}
	if in.Count == 0 {
		in.Count = 5
	}
	if in.Count < 1 || in.Count > MaxDraftCount {
		return DraftRequest{}, invalid("count is 1 to %d", MaxDraftCount)
	}
	actor, err := validActor(in.Actor)
	if err != nil {
		return DraftRequest{}, err
	}
	if err := s.keysAvailable(ctx, in.OrgID); err != nil {
		return DraftRequest{}, err
	}
	inputs, _ := json.Marshal(sp.Inputs)
	var out DraftRequest
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('autotest.draft:' || $1, 0))`, in.ProjectID); err != nil {
			return err
		}
		var waiting, today int
		if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'queued'), count(*) FILTER (WHERE created_at > now() - interval '24 hours')
			FROM auto_drafts WHERE project_id = $1`, in.ProjectID).Scan(&waiting, &today); err != nil {
			return err
		}
		if waiting >= MaxQueuedDrafts || today >= DailyDrafts {
			return ErrBusy
		}
		var err error
		out, err = scanDraft(q.QueryRow(ctx, `INSERT INTO auto_drafts (project_id, org_id, environment_id, target, account, inputs, context, count, test_type, requested_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING `+draftColumns,
			in.ProjectID, in.OrgID, in.EnvironmentID, sp.Target, sp.Account, inputs, in.Context, in.Count, in.TestType, actor))
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobDraft, Key: out.ID, MaxAttempts: 3,
			Payload: map[string]string{"draftId": out.ID, "projectId": in.ProjectID, "orgId": in.OrgID}})
		return err
	})
	return out, err
}

func (s *service) Draft(ctx context.Context, projectID, id string) (DraftRequest, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return DraftRequest{}, ErrNotFound
	}
	return scanDraft(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+draftColumns+` FROM auto_drafts WHERE id = $1 AND project_id = $2`, id, projectID))
}

// withBuiltins adds {{random}} and {{timestamp}} (fresh) and, when given,
// the account's sign-in values to a case's inputs. An input's value may
// use the two fresh ones itself (test+{{random}}@example.com).
func withBuiltins(inputs map[string]string, account map[string]string) map[string]string {
	vars := make(map[string]string, len(inputs)+4)
	fresh := map[string]string{VarRandom: randomText(), VarTimestamp: fmt.Sprint(time.Now().Unix())}
	for k, v := range inputs {
		vars[k] = varRe.ReplaceAllStringFunc(v, func(m string) string {
			if f, ok := fresh[varRe.FindStringSubmatch(m)[1]]; ok {
				return f
			}
			return m
		})
	}
	for k, v := range fresh {
		vars[k] = v
	}
	for k, v := range account {
		vars[k] = v
	}
	return vars
}

// ------------------------------------------------------------------- runs

const runColumns = `id, project_id, org_id, environment_id, coalesce(plan_id::text, ''), status, trigger, requested_by, heal,
	total, passed, failed, errored, healed, input_tokens, output_tokens, coalesce(test_run_id::text, ''), coalesce(error, ''),
	created_at, started_at, finished_at`

func scanRun(row interface{ Scan(dest ...any) error }) (Run, error) {
	var r Run
	var started, finished *time.Time
	err := row.Scan(&r.ID, &r.ProjectID, &r.OrgID, &r.EnvironmentID, &r.PlanID, &r.Status, &r.Trigger, &r.RequestedBy, &r.Heal,
		&r.Total, &r.Passed, &r.Failed, &r.Errored, &r.Healed, &r.InputTokens, &r.OutputTokens, &r.TestRunID, &r.Error,
		&r.CreatedAt, &started, &finished)
	if errors.Is(err, pg.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
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

const runCaseColumns = `case_id, position, title, spec, status, results, console, healed, duration_ms, coalesce(error, ''),
	final_shot IS NOT NULL, failure_shot IS NOT NULL, coalesce(execution_id::text, ''), finished_at`

func scanRunCase(row interface{ Scan(dest ...any) error }, projectID string) (RunCase, error) {
	var c RunCase
	var spec, results, console []byte
	var finished *time.Time
	err := row.Scan(&c.CaseID, &c.Position, &c.Title, &spec, &c.Status, &results, &console, &c.Healed, &c.DurationMs, &c.Error,
		&c.HasFinalShot, &c.HasFailureShot, &c.ExecutionID, &finished)
	if errors.Is(err, pg.ErrNoRows) {
		return RunCase{}, ErrNotFound
	}
	if err != nil {
		return RunCase{}, err
	}
	var sj specJSON
	_ = json.Unmarshal(spec, &sj)
	c.Spec = Spec{CaseID: c.CaseID, ProjectID: projectID, Target: sj.Target, Account: sj.Account, Inputs: sj.Inputs, Steps: sj.Steps}
	if c.Spec.Inputs == nil {
		c.Spec.Inputs = map[string]string{}
	}
	c.Results, c.Console = []StepResult{}, []string{}
	_ = json.Unmarshal(results, &c.Results)
	_ = json.Unmarshal(console, &c.Console)
	if finished != nil {
		c.FinishedAt = finished.UTC()
	}
	return c, nil
}

func (s *service) CreateRun(ctx context.Context, in NewRun) (Run, error) {
	if !uuidRe.MatchString(in.OrgID) {
		return Run{}, ErrNotFound
	}
	if in.PlanID != "" && !uuidRe.MatchString(in.PlanID) {
		return Run{}, invalid("planId")
	}
	if _, err := s.siteEnvironment(ctx, in.ProjectID, in.EnvironmentID); err != nil {
		return Run{}, err
	}
	if len(in.Cases) == 0 || len(in.Cases) > MaxRunCases {
		return Run{}, invalid("a run has 1 to %d cases", MaxRunCases)
	}
	if in.Trigger != TriggerDashboard && in.Trigger != TriggerCI {
		return Run{}, invalid("trigger")
	}
	actor, err := validActor(in.Actor)
	if err != nil {
		return Run{}, err
	}
	var out Run
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('autotest.run:' || $1, 0))`, in.ProjectID); err != nil {
			return err
		}
		var active, today int
		if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status IN ('queued', 'running')), count(*) FILTER (WHERE created_at > now() - interval '24 hours')
			FROM auto_runs WHERE project_id = $1`, in.ProjectID).Scan(&active, &today); err != nil {
			return err
		}
		if active >= MaxActiveRuns || today >= DailyRuns {
			return ErrBusy
		}
		specs := map[string]Spec{}
		rows, err := q.Query(ctx, `SELECT `+specColumns+` FROM auto_specs WHERE project_id = $1 AND case_id = ANY($2::uuid[])`, in.ProjectID, in.Cases)
		if err != nil {
			return err
		}
		for rows.Next() {
			sp, err := scanSpec(rows)
			if err != nil {
				rows.Close()
				return err
			}
			specs[sp.CaseID] = sp
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range in.Cases {
			if _, ok := specs[id]; !ok || seen[id] {
				return invalid("case %s has no runnable steps (or is listed twice)", id)
			}
			seen[id] = true
		}
		out, err = scanRun(q.QueryRow(ctx, `INSERT INTO auto_runs (project_id, org_id, environment_id, plan_id, trigger, requested_by, heal, total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+runColumns,
			in.ProjectID, in.OrgID, in.EnvironmentID, nullable(in.PlanID), in.Trigger, actor, in.Heal, len(in.Cases)))
		if err != nil {
			return err
		}
		for i, id := range in.Cases {
			sp := specs[id]
			snap, _ := json.Marshal(specJSON{Target: sp.Target, Account: sp.Account, Inputs: sp.Inputs, Steps: sp.Steps})
			title := cut(in.Titles[id], 500)
			if title == "" {
				title = "Case " + id[:8]
			}
			if _, err := q.Exec(ctx, `INSERT INTO auto_run_cases (run_id, case_id, position, title, spec) VALUES ($1, $2, $3, $4, $5)`,
				out.ID, id, i, title, snap); err != nil {
				return err
			}
			if _, _, err := s.q.Add(ctx, queue.Enqueue{Kind: JobCase, Key: out.ID + "/" + id, MaxAttempts: 3,
				Payload: map[string]string{"runId": out.ID, "caseId": id, "projectId": in.ProjectID}}); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func (s *service) Runs(ctx context.Context, projectID string, limit int) ([]Run, error) {
	if !uuidRe.MatchString(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.DB.Q(ctx).Query(ctx, `SELECT `+runColumns+` FROM auto_runs WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *service) GetRun(ctx context.Context, projectID, id string) (Run, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return Run{}, ErrNotFound
	}
	q := s.DB.Q(ctx)
	r, err := scanRun(q.QueryRow(ctx, `SELECT `+runColumns+` FROM auto_runs WHERE id = $1 AND project_id = $2`, id, projectID))
	if err != nil {
		return Run{}, err
	}
	rows, err := q.Query(ctx, `SELECT `+runCaseColumns+` FROM auto_run_cases WHERE run_id = $1 ORDER BY position`, id)
	if err != nil {
		return Run{}, err
	}
	defer rows.Close()
	r.Cases = []RunCase{}
	for rows.Next() {
		c, err := scanRunCase(rows, projectID)
		if err != nil {
			return Run{}, err
		}
		r.Cases = append(r.Cases, c)
	}
	return r, rows.Err()
}

func (s *service) runCase(ctx context.Context, projectID, runID, caseID string) (RunCase, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(runID) || !uuidRe.MatchString(caseID) {
		return RunCase{}, ErrNotFound
	}
	return scanRunCase(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+runCaseColumns+` FROM auto_run_cases c
		WHERE c.run_id = $1 AND c.case_id = $2 AND EXISTS (SELECT 1 FROM auto_runs r WHERE r.id = c.run_id AND r.project_id = $3)`,
		runID, caseID, projectID), projectID)
}

func (s *service) Shot(ctx context.Context, projectID, runID, caseID, which string) ([]byte, error) {
	col := map[string]string{"final": "final_shot", "failure": "failure_shot"}[which]
	if col == "" || !uuidRe.MatchString(projectID) || !uuidRe.MatchString(runID) || !uuidRe.MatchString(caseID) {
		return nil, ErrNotFound
	}
	var b []byte
	err := s.DB.Q(ctx).QueryRow(ctx, `SELECT c.`+col+` FROM auto_run_cases c JOIN auto_runs r ON r.id = c.run_id
		WHERE c.run_id = $1 AND c.case_id = $2 AND r.project_id = $3`, runID, caseID, projectID).Scan(&b)
	if errors.Is(err, pg.ErrNoRows) || (err == nil && b == nil) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *service) LastResults(ctx context.Context, projectID string) (map[string]LastResult, error) {
	if !uuidRe.MatchString(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.DB.Q(ctx).Query(ctx, `SELECT DISTINCT ON (c.case_id) c.case_id, c.run_id, c.status, c.finished_at
		FROM auto_run_cases c JOIN auto_runs r ON r.id = c.run_id
		WHERE r.project_id = $1 AND c.status <> 'pending'
		ORDER BY c.case_id, c.finished_at DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]LastResult{}
	for rows.Next() {
		var id string
		var lr LastResult
		if err := rows.Scan(&id, &lr.RunID, &lr.Status, &lr.FinishedAt); err != nil {
			return nil, err
		}
		lr.FinishedAt = lr.FinishedAt.UTC()
		out[id] = lr
	}
	return out, rows.Err()
}

// --------------------------------------------------------------- upkeep

func (s *service) Prune(ctx context.Context, before time.Time, limit int) (int, error) {
	q := s.DB.Q(ctx)
	total := 0
	for _, table := range []string{"auto_drafts", "auto_runs"} {
		// Runs still going are kept whatever their age.
		extra := ""
		if table == "auto_runs" {
			extra = ` AND status NOT IN ('queued', 'running')`
		}
		tag, err := q.Exec(ctx, `DELETE FROM `+table+` WHERE id IN (
			SELECT id FROM `+table+` WHERE created_at < $1`+extra+` ORDER BY created_at LIMIT $2)`, before.UTC(), limit)
		if err != nil {
			return total, err
		}
		total += int(tag.RowsAffected())
	}
	return total, nil
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !uuidRe.MatchString(projectID) {
		return nil
	}
	q := s.DB.Q(ctx)
	for _, table := range []string{"auto_runs", "auto_drafts", "auto_specs", "environment_accounts"} {
		if _, err := q.Exec(ctx, `DELETE FROM `+table+` WHERE project_id = $1`, projectID); err != nil {
			return err
		}
	}
	return nil
}
