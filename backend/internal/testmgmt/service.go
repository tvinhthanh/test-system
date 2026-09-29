package testmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

type service struct {
	db       *pg.DB
	identity identity.Service
	evidence Evidence
	policy   Policy
	now      func() time.Time
}

// New returns the Service over migrations/0013_testmgmt.sql. identity
// validates automation links; evidence feeds the release gate.
func New(db *pg.DB, idn identity.Service, ev Evidence, policy Policy) Service {
	if policy.FlakyRateWarn <= 0 {
		policy = DefaultPolicy
	}
	return &service{db: db, identity: idn, evidence: ev, policy: policy, now: time.Now}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

var priorities = map[Priority]bool{P0: true, P1: true, P2: true, P3: true}

func checkPriority(p *Priority) error {
	if *p == "" {
		*p = P2
	}
	if !priorities[*p] {
		return fmt.Errorf("%w: priority %q not in p0..p3", ErrInvalid, *p)
	}
	return nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pg.ErrNoRows):
		return ErrNotFound
	case pg.IsUniqueViolation(err):
		return fmt.Errorf("%w: already exists", ErrConflict)
	case pg.IsCheckViolation(err):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	case pg.IsForeignKeyViolation(err):
		return ErrNotFound
	}
	return err
}

// ---------------------------------------------------------------- suites

func (s *service) CreateSuite(ctx context.Context, projectID, parentID, name string) (Suite, error) {
	if !isUUID(projectID) {
		return Suite{}, ErrNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 {
		return Suite{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	if parentID != "" {
		if !isUUID(parentID) {
			return Suite{}, ErrNotFound
		}
		var n int
		if err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM test_suites WHERE id = $1 AND project_id = $2`, parentID, projectID).Scan(&n); err != nil {
			return Suite{}, err
		}
		if n == 0 {
			return Suite{}, ErrNotFound
		}
	}
	var out Suite
	var parent *string
	err := s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO test_suites (project_id, parent_id, name) VALUES ($1, $2, $3)
		RETURNING id, project_id, parent_id, name, created_at`, projectID, nullable(parentID), name).
		Scan(&out.ID, &out.ProjectID, &parent, &out.Name, &out.CreatedAt)
	if err != nil {
		return Suite{}, mapErr(err)
	}
	out.ParentID = deref(parent)
	return out, nil
}

func (s *service) ListSuites(ctx context.Context, projectID string) ([]Suite, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT s.id, s.project_id, s.parent_id, s.name, s.created_at,
		       (SELECT count(*) FROM test_cases c WHERE c.suite_id = s.id)
		  FROM test_suites s WHERE s.project_id = $1 ORDER BY s.name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Suite{}
	for rows.Next() {
		var x Suite
		var parent *string
		if err := rows.Scan(&x.ID, &x.ProjectID, &parent, &x.Name, &x.CreatedAt, &x.Cases); err != nil {
			return nil, err
		}
		x.ParentID = deref(parent)
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------- requirements

const reqColumns = `r.id, r.project_id, r.external_key, r.title, r.priority, r.status, r.created_at, r.updated_at,
	(SELECT count(*) FROM requirement_coverage c WHERE c.requirement_id = r.id)`

func scanReq(row interface{ Scan(dest ...any) error }) (Requirement, error) {
	var r Requirement
	var key *string
	if err := row.Scan(&r.ID, &r.ProjectID, &key, &r.Title, &r.Priority, &r.Status, &r.CreatedAt, &r.UpdatedAt, &r.CoveredBy); err != nil {
		return Requirement{}, err
	}
	r.ExternalKey = deref(key)
	return r, nil
}

func (s *service) CreateRequirement(ctx context.Context, projectID string, in NewRequirement) (Requirement, error) {
	if !isUUID(projectID) {
		return Requirement{}, ErrNotFound
	}
	in.Title = strings.TrimSpace(in.Title)
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	if in.Title == "" || len(in.Title) > 500 {
		return Requirement{}, fmt.Errorf("%w: title", ErrInvalid)
	}
	if len(in.ExternalKey) > 100 {
		return Requirement{}, fmt.Errorf("%w: externalKey", ErrInvalid)
	}
	if err := checkPriority(&in.Priority); err != nil {
		return Requirement{}, err
	}
	var id string
	err := s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO requirements (project_id, external_key, title, priority) VALUES ($1, $2, $3, $4) RETURNING id`,
		projectID, nullable(in.ExternalKey), in.Title, string(in.Priority)).Scan(&id)
	if err != nil {
		return Requirement{}, mapErr(err)
	}
	return s.GetRequirement(ctx, projectID, id)
}

func (s *service) UpdateRequirement(ctx context.Context, projectID, id string, p RequirementPatch) (Requirement, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Requirement{}, ErrNotFound
	}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" || len(t) > 500 {
			return Requirement{}, fmt.Errorf("%w: title", ErrInvalid)
		}
		p.Title = &t
	}
	if p.Priority != nil {
		if err := checkPriority(p.Priority); err != nil {
			return Requirement{}, err
		}
	}
	if p.Status != nil {
		switch *p.Status {
		case ReqDraft, ReqActive, ReqDone, ReqDropped:
		default:
			return Requirement{}, fmt.Errorf("%w: status %q", ErrInvalid, *p.Status)
		}
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `
		UPDATE requirements SET title = coalesce($3, title), priority = coalesce($4, priority), status = coalesce($5, status), updated_at = now()
		 WHERE id = $1 AND project_id = $2`, id, projectID, p.Title, (*string)(p.Priority), (*string)(p.Status))
	if err != nil {
		return Requirement{}, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return Requirement{}, ErrNotFound
	}
	return s.GetRequirement(ctx, projectID, id)
}

func (s *service) ListRequirements(ctx context.Context, projectID string, limit int) ([]Requirement, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+reqColumns+` FROM requirements r WHERE r.project_id = $1 ORDER BY r.priority, r.created_at LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Requirement{}
	for rows.Next() {
		r, err := scanReq(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *service) GetRequirement(ctx context.Context, projectID, id string) (Requirement, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Requirement{}, ErrNotFound
	}
	r, err := scanReq(s.db.Q(ctx).QueryRow(ctx, `SELECT `+reqColumns+` FROM requirements r WHERE r.id = $1 AND r.project_id = $2`, id, projectID))
	return r, mapErr(err)
}

// ------------------------------------------------------------ test cases

const caseColumns = `c.id, c.project_id, c.suite_id, coalesce(s.name, ''), c.external_key, c.title, c.steps, c.expected, c.priority,
	c.is_critical, c.automation_status, c.owner_id, c.created_at, c.updated_at`

func scanCase(row interface{ Scan(dest ...any) error }) (TestCase, error) {
	var c TestCase
	var suite, key, owner *string
	var steps []byte
	if err := row.Scan(&c.ID, &c.ProjectID, &suite, &c.SuiteName, &key, &c.Title, &steps, &c.Expected, &c.Priority,
		&c.IsCritical, &c.AutomationStatus, &owner, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return TestCase{}, err
	}
	c.SuiteID, c.ExternalKey, c.OwnerID = deref(suite), deref(key), deref(owner)
	c.Steps = []Step{}
	_ = json.Unmarshal(steps, &c.Steps)
	return c, nil
}

func normalizeSteps(steps []Step) ([]byte, error) {
	if steps == nil {
		steps = []Step{}
	}
	if len(steps) > 200 {
		return nil, fmt.Errorf("%w: more than 200 steps", ErrInvalid)
	}
	for i := range steps {
		steps[i].Action = strings.TrimSpace(steps[i].Action)
		steps[i].Expected = strings.TrimSpace(steps[i].Expected)
		if len(steps[i].Action) > 2000 || len(steps[i].Expected) > 2000 {
			return nil, fmt.Errorf("%w: step %d too long", ErrInvalid, i+1)
		}
	}
	return json.Marshal(steps)
}

func (s *service) suiteInProject(ctx context.Context, projectID, suiteID string) error {
	if suiteID == "" {
		return nil
	}
	if !isUUID(suiteID) {
		return ErrNotFound
	}
	var n int
	if err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM test_suites WHERE id = $1 AND project_id = $2`, suiteID, projectID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *service) CreateTestCase(ctx context.Context, projectID string, in NewTestCase) (TestCase, error) {
	if !isUUID(projectID) {
		return TestCase{}, ErrNotFound
	}
	in.Title = strings.TrimSpace(in.Title)
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	if in.Title == "" || len(in.Title) > 500 {
		return TestCase{}, fmt.Errorf("%w: title", ErrInvalid)
	}
	if len(in.ExternalKey) > 100 {
		return TestCase{}, fmt.Errorf("%w: externalKey", ErrInvalid)
	}
	if err := checkPriority(&in.Priority); err != nil {
		return TestCase{}, err
	}
	if in.OwnerID != "" && !isUUID(in.OwnerID) {
		return TestCase{}, fmt.Errorf("%w: ownerId", ErrInvalid)
	}
	if err := s.suiteInProject(ctx, projectID, in.SuiteID); err != nil {
		return TestCase{}, err
	}
	steps, err := normalizeSteps(in.Steps)
	if err != nil {
		return TestCase{}, err
	}
	var id string
	err = s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO test_cases (project_id, suite_id, external_key, title, steps, expected, priority, is_critical, owner_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		projectID, nullable(in.SuiteID), nullable(in.ExternalKey), in.Title, steps, strings.TrimSpace(in.Expected), string(in.Priority), in.IsCritical, nullable(in.OwnerID)).Scan(&id)
	if err != nil {
		return TestCase{}, mapErr(err)
	}
	return s.GetTestCase(ctx, projectID, id)
}

func (s *service) UpdateTestCase(ctx context.Context, projectID, id string, p TestCasePatch) (TestCase, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return TestCase{}, ErrNotFound
	}
	if p.Title != nil {
		t := strings.TrimSpace(*p.Title)
		if t == "" || len(t) > 500 {
			return TestCase{}, fmt.Errorf("%w: title", ErrInvalid)
		}
		p.Title = &t
	}
	if p.Priority != nil {
		if err := checkPriority(p.Priority); err != nil {
			return TestCase{}, err
		}
	}
	if p.AutomationStatus != nil {
		switch *p.AutomationStatus {
		case ManualOnly, AutomationCandidate, Automated, Obsolete:
		default:
			return TestCase{}, fmt.Errorf("%w: automationStatus %q", ErrInvalid, *p.AutomationStatus)
		}
	}
	if p.OwnerID != nil && *p.OwnerID != "" && !isUUID(*p.OwnerID) {
		return TestCase{}, fmt.Errorf("%w: ownerId", ErrInvalid)
	}
	var steps []byte
	if p.Steps != nil {
		var err error
		if steps, err = normalizeSteps(*p.Steps); err != nil {
			return TestCase{}, err
		}
	}
	// suite_id and owner_id: a patch with "" clears; absent keeps.
	setSuite, setOwner := p.SuiteID != nil, p.OwnerID != nil
	var suite, owner *string
	if setSuite {
		if err := s.suiteInProject(ctx, projectID, *p.SuiteID); err != nil {
			return TestCase{}, err
		}
		suite = nullable(*p.SuiteID)
	}
	if setOwner {
		owner = nullable(*p.OwnerID)
	}
	var expected *string
	if p.Expected != nil {
		e := strings.TrimSpace(*p.Expected)
		expected = &e
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `
		UPDATE test_cases
		   SET title = coalesce($3, title), steps = coalesce($4, steps), expected = coalesce($5, expected),
		       priority = coalesce($6, priority), is_critical = coalesce($7, is_critical),
		       automation_status = coalesce($8, automation_status),
		       suite_id = CASE WHEN $9 THEN $10 ELSE suite_id END,
		       owner_id = CASE WHEN $11 THEN $12 ELSE owner_id END,
		       updated_at = now()
		 WHERE id = $1 AND project_id = $2`,
		id, projectID, p.Title, steps, expected, (*string)(p.Priority), p.IsCritical, (*string)(p.AutomationStatus), setSuite, suite, setOwner, owner)
	if err != nil {
		return TestCase{}, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return TestCase{}, ErrNotFound
	}
	return s.GetTestCase(ctx, projectID, id)
}

func (s *service) ListTestCases(ctx context.Context, projectID string, f CaseFilter) ([]TestCase, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	if f.SuiteID != "" && !isUUID(f.SuiteID) {
		return []TestCase{}, nil
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 200
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+caseColumns+`
		  FROM test_cases c LEFT JOIN test_suites s ON s.id = c.suite_id
		 WHERE c.project_id = $1
		   AND ($2 = '' OR c.suite_id = $2::uuid)
		   AND ($3 = '' OR c.automation_status = $3)
		   AND ($4::boolean IS NULL OR c.is_critical = $4)
		   AND ($5 = '' OR c.title ILIKE '%' || $5 || '%' OR c.external_key ILIKE '%' || $5 || '%')
		 ORDER BY c.priority, c.created_at LIMIT $6`,
		projectID, f.SuiteID, string(f.AutomationStatus), f.Critical, strings.TrimSpace(f.Query), f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TestCase{}
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *service) GetTestCase(ctx context.Context, projectID, id string) (TestCase, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return TestCase{}, ErrNotFound
	}
	q := s.db.Q(ctx)
	c, err := scanCase(q.QueryRow(ctx, `SELECT `+caseColumns+` FROM test_cases c LEFT JOIN test_suites s ON s.id = c.suite_id WHERE c.id = $1 AND c.project_id = $2`, id, projectID))
	if err != nil {
		return TestCase{}, mapErr(err)
	}
	rows, err := q.Query(ctx, `SELECT id, test_case_id, stable_test_id, link_type, created_by, created_at FROM test_case_automation_links WHERE test_case_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return TestCase{}, err
	}
	defer rows.Close()
	c.Links = []Link{}
	for rows.Next() {
		var l Link
		if err := rows.Scan(&l.ID, &l.TestCaseID, &l.StableTestID, &l.LinkType, &l.CreatedBy, &l.CreatedAt); err != nil {
			return TestCase{}, err
		}
		c.Links = append(c.Links, l)
	}
	if err := rows.Err(); err != nil {
		return TestCase{}, err
	}
	reqs, err := q.Query(ctx, `SELECT requirement_id FROM requirement_coverage WHERE test_case_id = $1 ORDER BY created_at`, id)
	if err != nil {
		return TestCase{}, err
	}
	defer reqs.Close()
	c.Requirements = []string{}
	for reqs.Next() {
		var r string
		if err := reqs.Scan(&r); err != nil {
			return TestCase{}, err
		}
		c.Requirements = append(c.Requirements, r)
	}
	return c, reqs.Err()
}

func (s *service) LinkAutomation(ctx context.Context, projectID, testCaseID, stableTestID string, t LinkType, actor string) (Link, error) {
	if !isUUID(projectID) || !isUUID(testCaseID) || !isUUID(stableTestID) {
		return Link{}, ErrNotFound
	}
	if t == "" {
		t = LinkExact
	}
	switch t {
	case LinkExact, LinkPartial, LinkSuperset:
	default:
		return Link{}, fmt.Errorf("%w: linkType %q", ErrInvalid, t)
	}
	if actor == "" {
		return Link{}, fmt.Errorf("%w: actor required", ErrInvalid)
	}
	// The identity is checked through identity's contract (§6.9.5): same
	// project, and active — a merged or retired identity is history, and a
	// link to it would silently point at nothing the next run touches.
	idn, err := s.identity.Get(ctx, stableTestID)
	if err != nil {
		if errors.Is(err, identity.ErrNotFound) {
			return Link{}, ErrNotFound
		}
		return Link{}, err
	}
	if idn.ProjectID != projectID {
		return Link{}, ErrNotFound
	}
	if idn.Status != identity.StatusActive {
		return Link{}, fmt.Errorf("%w: identity is %s", ErrConflict, idn.Status)
	}
	var out Link
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM test_cases WHERE id = $1 AND project_id = $2`, testCaseID, projectID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		err := q.QueryRow(ctx, `
			INSERT INTO test_case_automation_links (project_id, test_case_id, stable_test_id, link_type, created_by)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, test_case_id, stable_test_id, link_type, created_by, created_at`,
			projectID, testCaseID, stableTestID, string(t), actor).
			Scan(&out.ID, &out.TestCaseID, &out.StableTestID, &out.LinkType, &out.CreatedBy, &out.CreatedAt)
		if err != nil {
			return mapErr(err)
		}
		// First link: the case is automated now (§6.8.2 automationStatus).
		_, err = q.Exec(ctx, `UPDATE test_cases SET automation_status = 'automated', updated_at = now()
			 WHERE id = $1 AND automation_status IN ('manual_only', 'automation_candidate')`, testCaseID)
		return err
	})
	if err != nil {
		return Link{}, err
	}
	return out, nil
}

func (s *service) UnlinkAutomation(ctx context.Context, projectID, testCaseID, linkID string) error {
	if !isUUID(projectID) || !isUUID(testCaseID) || !isUUID(linkID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		tag, err := q.Exec(ctx, `DELETE FROM test_case_automation_links WHERE id = $1 AND test_case_id = $2 AND project_id = $3`, linkID, testCaseID, projectID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		// Last link gone: automated → candidate, so the case is not reported
		// as covered by automation it no longer has.
		_, err = q.Exec(ctx, `UPDATE test_cases SET automation_status = 'automation_candidate', updated_at = now()
			 WHERE id = $1 AND automation_status = 'automated'
			   AND NOT EXISTS (SELECT 1 FROM test_case_automation_links l WHERE l.test_case_id = $1)`, testCaseID)
		return err
	})
}

func (s *service) Cover(ctx context.Context, projectID, requirementID, testCaseID string) error {
	if !isUUID(projectID) || !isUUID(requirementID) || !isUUID(testCaseID) {
		return ErrNotFound
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO requirement_coverage (project_id, requirement_id, test_case_id)
		SELECT $1, r.id, c.id FROM requirements r, test_cases c
		 WHERE r.id = $2 AND r.project_id = $1 AND c.id = $3 AND c.project_id = $1
		ON CONFLICT DO NOTHING`, projectID, requirementID, testCaseID)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		// Either the pair exists (fine, idempotent) or a side is foreign.
		var n int
		if err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM requirement_coverage WHERE requirement_id = $1 AND test_case_id = $2 AND project_id = $3`, requirementID, testCaseID, projectID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
	}
	return nil
}

func (s *service) Uncover(ctx context.Context, projectID, requirementID, testCaseID string) error {
	if !isUUID(projectID) || !isUUID(requirementID) || !isUUID(testCaseID) {
		return ErrNotFound
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM requirement_coverage WHERE requirement_id = $1 AND test_case_id = $2 AND project_id = $3`, requirementID, testCaseID, projectID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ------------------------------------------------------------------ plans

const planColumns = `id, project_id, name, environment_id, status, created_at, updated_at`

func scanPlan(row interface{ Scan(dest ...any) error }) (Plan, error) {
	var p Plan
	var env *string
	if err := row.Scan(&p.ID, &p.ProjectID, &p.Name, &env, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Plan{}, err
	}
	p.EnvironmentID = deref(env)
	return p, nil
}

func (s *service) CreatePlan(ctx context.Context, projectID string, in NewPlan) (Plan, error) {
	if !isUUID(projectID) {
		return Plan{}, ErrNotFound
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return Plan{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	if in.EnvironmentID != "" && !isUUID(in.EnvironmentID) {
		return Plan{}, fmt.Errorf("%w: environmentId", ErrInvalid)
	}
	p, err := scanPlan(s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO test_plans (project_id, name, environment_id) VALUES ($1, $2, $3) RETURNING `+planColumns,
		projectID, in.Name, nullable(in.EnvironmentID)))
	if err != nil {
		return Plan{}, mapErr(err)
	}
	p.Executions = []Execution{}
	return p, nil
}

func (s *service) ListPlans(ctx context.Context, projectID string) ([]Plan, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+planColumns+` FROM test_plans WHERE project_id = $1 ORDER BY created_at DESC LIMIT 200`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *service) GetPlan(ctx context.Context, projectID, id string) (Plan, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Plan{}, ErrNotFound
	}
	q := s.db.Q(ctx)
	p, err := scanPlan(q.QueryRow(ctx, `SELECT `+planColumns+` FROM test_plans WHERE id = $1 AND project_id = $2`, id, projectID))
	if err != nil {
		return Plan{}, mapErr(err)
	}
	// Latest verdict per case (append-only rows, §6.8.2 ManualExecution).
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (e.test_case_id) e.id, e.test_plan_id, e.test_case_id, c.title, e.executed_by, e.status, e.note, e.executed_at
		  FROM manual_executions e JOIN test_cases c ON c.id = e.test_case_id
		 WHERE e.test_plan_id = $1
		 ORDER BY e.test_case_id, e.executed_at DESC`, id)
	if err != nil {
		return Plan{}, err
	}
	defer rows.Close()
	p.Executions = []Execution{}
	for rows.Next() {
		var e Execution
		if err := rows.Scan(&e.ID, &e.PlanID, &e.TestCaseID, &e.CaseTitle, &e.ExecutedBy, &e.Status, &e.Note, &e.ExecutedAt); err != nil {
			return Plan{}, err
		}
		p.Executions = append(p.Executions, e)
	}
	return p, rows.Err()
}

func (s *service) SetPlanStatus(ctx context.Context, projectID, id string, status PlanStatus) (Plan, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Plan{}, ErrNotFound
	}
	switch status {
	case PlanDraft, PlanActive, PlanClosed:
	default:
		return Plan{}, fmt.Errorf("%w: status %q", ErrInvalid, status)
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `UPDATE test_plans SET status = $3, updated_at = now() WHERE id = $1 AND project_id = $2`, id, projectID, string(status))
	if err != nil {
		return Plan{}, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return Plan{}, ErrNotFound
	}
	return s.GetPlan(ctx, projectID, id)
}

func (s *service) RecordExecution(ctx context.Context, projectID, planID string, in NewExecution, actor string) (Execution, error) {
	if !isUUID(projectID) || !isUUID(planID) || !isUUID(in.TestCaseID) {
		return Execution{}, ErrNotFound
	}
	switch in.Status {
	case ExecPassed, ExecFailed, ExecBlocked, ExecSkipped:
	default:
		return Execution{}, fmt.Errorf("%w: status %q", ErrInvalid, in.Status)
	}
	if actor == "" {
		return Execution{}, fmt.Errorf("%w: actor required", ErrInvalid)
	}
	if len(in.Note) > 4000 {
		return Execution{}, fmt.Errorf("%w: note too long", ErrInvalid)
	}
	at := in.ExecutedAt
	if at.IsZero() {
		at = s.now()
	}
	var out Execution
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var status string
		if err := q.QueryRow(ctx, `SELECT status FROM test_plans WHERE id = $1 AND project_id = $2`, planID, projectID).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status == string(PlanClosed) {
			return fmt.Errorf("%w: plan is closed", ErrConflict)
		}
		var title string
		if err := q.QueryRow(ctx, `SELECT title FROM test_cases WHERE id = $1 AND project_id = $2`, in.TestCaseID, projectID).Scan(&title); err != nil {
			return mapErr(err)
		}
		err := q.QueryRow(ctx, `
			INSERT INTO manual_executions (project_id, test_plan_id, test_case_id, executed_by, status, note, executed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id, test_plan_id, test_case_id, executed_by, status, note, executed_at`,
			projectID, planID, in.TestCaseID, actor, string(in.Status), strings.TrimSpace(in.Note), at.UTC()).
			Scan(&out.ID, &out.PlanID, &out.TestCaseID, &out.ExecutedBy, &out.Status, &out.Note, &out.ExecutedAt)
		if err != nil {
			return mapErr(err)
		}
		out.CaseTitle = title
		// A plan with its first execution is active by definition.
		_, err = q.Exec(ctx, `UPDATE test_plans SET status = 'active', updated_at = now() WHERE id = $1 AND status = 'draft'`, planID)
		return err
	})
	if err != nil {
		return Execution{}, err
	}
	return out, nil
}

func (s *service) ExecutionForEvidence(ctx context.Context, projectID, planID, id string) (Execution, error) {
	if !isUUID(projectID) || !isUUID(planID) || !isUUID(id) {
		return Execution{}, ErrNotFound
	}
	var e Execution
	var planStatus string
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT e.id, e.test_plan_id, e.test_case_id, c.title, e.executed_by, e.status, e.note, e.executed_at, p.status
		  FROM manual_executions e
		  JOIN test_cases c ON c.id = e.test_case_id
		  JOIN test_plans p ON p.id = e.test_plan_id
		 WHERE e.id = $1 AND e.test_plan_id = $2 AND e.project_id = $3`, id, planID, projectID).
		Scan(&e.ID, &e.PlanID, &e.TestCaseID, &e.CaseTitle, &e.ExecutedBy, &e.Status, &e.Note, &e.ExecutedAt, &planStatus)
	if err != nil {
		return Execution{}, mapErr(err)
	}
	if planStatus == string(PlanClosed) {
		return Execution{}, fmt.Errorf("%w: plan is closed", ErrConflict)
	}
	return e, nil
}

// --------------------------------------------------------------- releases

const releaseColumns = `id, project_id, version, branch, status, decided_by, decided_at, decision_note, last_gate, evaluated_at, created_at, updated_at`

func scanRelease(row interface{ Scan(dest ...any) error }) (Release, error) {
	var r Release
	var by *string
	var decidedAt, evaluatedAt *time.Time
	var gate []byte
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Version, &r.Branch, &r.Status, &by, &decidedAt, &r.DecisionNote, &gate, &evaluatedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return Release{}, err
	}
	r.DecidedBy = deref(by)
	if decidedAt != nil {
		r.DecidedAt = decidedAt.UTC()
	}
	if evaluatedAt != nil {
		r.EvaluatedAt = evaluatedAt.UTC()
	}
	if len(gate) > 0 {
		var g GateResult
		if json.Unmarshal(gate, &g) == nil {
			r.Gate = &g
		}
	}
	return r, nil
}

func (s *service) CreateRelease(ctx context.Context, projectID string, in NewRelease) (Release, error) {
	if !isUUID(projectID) {
		return Release{}, ErrNotFound
	}
	in.Version = strings.TrimSpace(in.Version)
	in.Branch = strings.TrimSpace(in.Branch)
	if in.Version == "" || len(in.Version) > 100 {
		return Release{}, fmt.Errorf("%w: version", ErrInvalid)
	}
	if len(in.Branch) > 200 {
		return Release{}, fmt.Errorf("%w: branch", ErrInvalid)
	}
	r, err := scanRelease(s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO releases (project_id, version, branch) VALUES ($1, $2, $3) RETURNING `+releaseColumns, projectID, in.Version, in.Branch))
	return r, mapErr(err)
}

func (s *service) ListReleases(ctx context.Context, projectID string) ([]Release, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+releaseColumns+` FROM releases WHERE project_id = $1 ORDER BY created_at DESC LIMIT 200`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *service) GetRelease(ctx context.Context, projectID, id string) (Release, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Release{}, ErrNotFound
	}
	r, err := scanRelease(s.db.Q(ctx).QueryRow(ctx, `SELECT `+releaseColumns+` FROM releases WHERE id = $1 AND project_id = $2`, id, projectID))
	return r, mapErr(err)
}

func (s *service) Decide(ctx context.Context, projectID, id string, d Decision, actor string) (Release, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Release{}, ErrNotFound
	}
	switch d.Status {
	case ReleaseBlocked, ReleaseApproved, ReleaseShipped:
	default:
		return Release{}, fmt.Errorf("%w: status %q (blocked|approved|shipped)", ErrInvalid, d.Status)
	}
	if actor == "" {
		return Release{}, fmt.Errorf("%w: actor required", ErrInvalid)
	}
	if len(d.Note) > 4000 {
		return Release{}, fmt.Errorf("%w: note too long", ErrInvalid)
	}
	var out Release
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		cur, err := scanRelease(s.db.Q(ctx).QueryRow(ctx, `SELECT `+releaseColumns+` FROM releases WHERE id = $1 AND project_id = $2 FOR UPDATE`, id, projectID))
		if err != nil {
			return mapErr(err)
		}
		if cur.Status == ReleaseShipped {
			return fmt.Errorf("%w: release already shipped", ErrConflict)
		}
		// §6.8.4 #1: the gate may say blocked and a named person may still
		// approve — that is the override, and the name goes on the row.
		out, err = scanRelease(s.db.Q(ctx).QueryRow(ctx, `
			UPDATE releases SET status = $3, decided_by = $4, decided_at = $5, decision_note = $6, updated_at = now()
			 WHERE id = $1 AND project_id = $2 RETURNING `+releaseColumns,
			id, projectID, string(d.Status), actor, s.now().UTC(), strings.TrimSpace(d.Note)))
		return mapErr(err)
	})
	if err != nil {
		return Release{}, err
	}
	return out, nil
}

func (s *service) ProjectOfRelease(ctx context.Context, releaseID string) (string, error) {
	if !isUUID(releaseID) {
		return "", ErrNotFound
	}
	var project string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT project_id FROM releases WHERE id = $1`, releaseID).Scan(&project)
	return project, mapErr(err)
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !purgeUUID.MatchString(projectID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, q := range []string{
			"DELETE FROM manual_executions WHERE project_id = $1",
			"DELETE FROM test_plans WHERE project_id = $1",
			"DELETE FROM releases WHERE project_id = $1",
			"DELETE FROM requirement_coverage WHERE project_id = $1",
			"DELETE FROM test_case_automation_links WHERE project_id = $1",
			"DELETE FROM test_cases WHERE project_id = $1",
			"DELETE FROM requirements WHERE project_id = $1",
			"DELETE FROM test_suites WHERE project_id = $1",
			"DELETE FROM ttrc_baselines WHERE project_id = $1",
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, projectID); err != nil {
				return fmt.Errorf("testmgmt: purge project: %w", err)
			}
		}
		return nil
	})
}

var purgeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
