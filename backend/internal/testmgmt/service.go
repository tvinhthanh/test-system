package testmgmt

import (
	"context"
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
