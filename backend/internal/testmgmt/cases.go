package testmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
)

// ------------------------------------------------------------ test cases

const caseColumns = `c.id, c.project_id, c.suite_id, coalesce(s.name, ''), c.external_key, c.title, c.steps, c.expected, c.priority,
	c.is_critical, c.automation_status, c.owner_id, c.created_at, c.updated_at, c.preconditions, c.test_data, c.taxonomy`

const caseFilterWhere = `c.project_id = $1
	AND ($2 = '' OR c.suite_id = $2::uuid)
	AND ($3 = '' OR c.automation_status = $3)
	AND ($4::boolean IS NULL OR c.is_critical = $4)
	AND ($5 = '' OR c.title ILIKE '%' || $5 || '%' OR c.external_key ILIKE '%' || $5 || '%')
    AND ($6 = '' OR c.taxonomy->>'level' = $6)
    AND ($7 = '' OR c.taxonomy->'purposes' ? $7)
    AND ($8 = '' OR c.taxonomy->'tags' ? $8)
    AND ($9 = '' OR c.taxonomy->'executionModes' ? $9)
    AND ($10 = '' OR c.taxonomy->'performanceTypes' ? $10)`

func scanCase(row interface{ Scan(dest ...any) error }) (TestCase, error) {
	return scanCaseExtra(row)
}

func scanCaseExtra(row interface{ Scan(dest ...any) error }, extra ...any) (TestCase, error) {
	var c TestCase
	var suite, key, owner *string
	var steps, taxonomy []byte
	dest := []any{&c.ID, &c.ProjectID, &suite, &c.SuiteName, &key, &c.Title, &steps, &c.Expected, &c.Priority,
		&c.IsCritical, &c.AutomationStatus, &owner, &c.CreatedAt, &c.UpdatedAt, &c.Preconditions, &c.TestData, &taxonomy}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return TestCase{}, err
	}
	c.SuiteID, c.ExternalKey, c.OwnerID = deref(suite), deref(key), deref(owner)
	c.Steps = []Step{}
	_ = json.Unmarshal(steps, &c.Steps)
	if err := json.Unmarshal(taxonomy, &c.Taxonomy); err != nil {
		return TestCase{}, err
	}
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
	taxonomy, err := taxonomyJSON(in.Taxonomy)
	if err != nil {
		return TestCase{}, err
	}
	if !isUUID(projectID) {
		return TestCase{}, ErrNotFound
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Preconditions = strings.TrimSpace(in.Preconditions)
	in.TestData = strings.TrimSpace(in.TestData)
	if len(in.Preconditions) > 16000 || len(in.TestData) > 16000 {
		return TestCase{}, fmt.Errorf("%w: case context exceeds 16000 bytes", ErrInvalid)
	}
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
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.caseAuditActor(ctx, in.Actor, ""); err != nil {
			return err
		}
		return s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO test_cases (project_id, suite_id, external_key, title, steps, expected, priority, is_critical, owner_id, preconditions, test_data, taxonomy)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id`,
			projectID, nullable(in.SuiteID), nullable(in.ExternalKey), in.Title, steps, strings.TrimSpace(in.Expected), string(in.Priority), in.IsCritical, nullable(in.OwnerID), in.Preconditions, in.TestData, taxonomy).Scan(&id)
	})
	if err != nil {
		return TestCase{}, mapErr(err)
	}
	return s.GetTestCase(ctx, projectID, id)
}

func (s *service) UpdateTestCase(ctx context.Context, projectID, id string, p TestCasePatch) (TestCase, error) {
	var taxonomy []byte
	if p.Taxonomy != nil {
		var err error
		taxonomy, err = taxonomyJSON(*p.Taxonomy)
		if err != nil {
			return TestCase{}, err
		}
	}
	if !isUUID(projectID) || !isUUID(id) {
		return TestCase{}, ErrNotFound
	}
	for _, field := range []*string{p.Preconditions, p.TestData} {
		if field != nil {
			*field = strings.TrimSpace(*field)
			if len(*field) > 16000 {
				return TestCase{}, fmt.Errorf("%w: case context exceeds 16000 bytes", ErrInvalid)
			}
		}
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
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var version time.Time
		if err := q.QueryRow(ctx, `SELECT updated_at FROM test_cases WHERE id = $1 AND project_id = $2 FOR UPDATE`, id, projectID).Scan(&version); err != nil {
			return mapErr(err)
		}
		if !p.ExpectedUpdatedAt.IsZero() && !p.ExpectedUpdatedAt.Equal(version) {
			return fmt.Errorf("%w: case changed; refresh before saving", ErrConflict)
		}
		if err := s.caseAuditActor(ctx, p.Actor, ""); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `
		UPDATE test_cases
		   SET title = coalesce($3, title), steps = coalesce($4, steps), expected = coalesce($5, expected),
		       priority = coalesce($6, priority), is_critical = coalesce($7, is_critical),
		       automation_status = coalesce($8, automation_status),
		       suite_id = CASE WHEN $9 THEN $10 ELSE suite_id END,
		       owner_id = CASE WHEN $11 THEN $12 ELSE owner_id END,
		       preconditions = coalesce($13, preconditions), test_data = coalesce($14, test_data), taxonomy = coalesce($15, taxonomy), updated_at = clock_timestamp()
		 WHERE id = $1 AND project_id = $2`,
			id, projectID, p.Title, steps, expected, (*string)(p.Priority), p.IsCritical, (*string)(p.AutomationStatus), setSuite, suite, setOwner, owner, p.Preconditions, p.TestData, taxonomy)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return TestCase{}, err
	}
	return s.GetTestCase(ctx, projectID, id)
}

func (s *service) caseAuditActor(ctx context.Context, actor, action string) error {
	if actor == "" {
		actor = "system"
	}
	_, err := s.db.Q(ctx).Exec(ctx, `SELECT set_config('app.testmgmt_actor', $1, true), set_config('app.testmgmt_action', $2, true)`, actor, action)
	return err
}

func (s *service) ListTestCases(ctx context.Context, projectID string, f CaseFilter) ([]TestCase, error) {
	taxonomyArgs, err := f.taxonomyArgs()
	if err != nil {
		return nil, err
	}
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
		 WHERE `+caseFilterWhere+`
		 ORDER BY c.priority, c.created_at LIMIT $11`,
		append(append([]any{projectID, f.SuiteID, string(f.AutomationStatus), f.Critical, strings.TrimSpace(f.Query)}, taxonomyArgs...), f.Limit)...)
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
	rows.Close()
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
	if err := reqs.Err(); err != nil {
		return TestCase{}, err
	}
	reqs.Close()
	audit, err := q.Query(ctx, `SELECT id, actor, action, changed_fields, created_at FROM test_case_audit WHERE test_case_id = $1 AND project_id = $2 ORDER BY created_at DESC, id DESC LIMIT 51`, id, projectID)
	if err != nil {
		return TestCase{}, err
	}
	defer audit.Close()
	c.Audit = []CaseChange{}
	for audit.Next() {
		var item CaseChange
		if err := audit.Scan(&item.ID, &item.Actor, &item.Action, &item.ChangedFields, &item.CreatedAt); err != nil {
			return TestCase{}, err
		}
		if len(c.Audit) == 50 {
			c.AuditTruncated = true
			break
		}
		c.Audit = append(c.Audit, item)
	}
	return c, audit.Err()
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
		if err := s.caseAuditActor(ctx, actor, ""); err != nil {
			return err
		}
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
