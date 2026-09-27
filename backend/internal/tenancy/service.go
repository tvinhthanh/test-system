package tenancy

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// service is the only implementation of Service. It is unexported: the
// contract in tenancy.go is the whole surface (§6.9.4).
type service struct {
	db *pg.DB
}

// New returns the Service backed by the tables in migrations/0001_tenancy.sql.
func New(db *pg.DB) Service { return &service{db: db} }

// uuidRe is the canonical text form. A malformed id cannot name a row, so
// it is reported as ErrNotFound before a round trip rather than as a driver
// encoding error.
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *service) CreateOrganization(ctx context.Context, name, slug string) (Organization, error) {
	if !ValidName(name) {
		return Organization{}, fmt.Errorf("%w: organization name", ErrInvalid)
	}
	if !ValidSlug(slug) {
		return Organization{}, fmt.Errorf("%w: slug %q", ErrInvalid, slug)
	}
	var o Organization
	err := s.db.Q(ctx).QueryRow(ctx,
		`INSERT INTO organizations (name, slug) VALUES ($1, $2)
		 RETURNING id, name, slug, plan_id, created_at`, name, slug,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.PlanID, &o.CreatedAt)
	return o, mapErr(err)
}

func (s *service) CreateWorkspace(ctx context.Context, org OrganizationID, name string) (Workspace, error) {
	if !ValidName(name) {
		return Workspace{}, fmt.Errorf("%w: workspace name", ErrInvalid)
	}
	if !uuidRe.MatchString(string(org)) {
		return Workspace{}, ErrNotFound
	}
	var w Workspace
	err := s.db.Q(ctx).QueryRow(ctx,
		`INSERT INTO workspaces (org_id, name) VALUES ($1, $2)
		 RETURNING id, org_id, name, created_at`, string(org), name,
	).Scan(&w.ID, &w.OrgID, &w.Name, &w.CreatedAt)
	return w, mapErr(err)
}

func (s *service) CreateProject(ctx context.Context, ws WorkspaceID, name, defaultBranch string) (Project, error) {
	if !ValidName(name) {
		return Project{}, fmt.Errorf("%w: project name", ErrInvalid)
	}
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	if len(defaultBranch) > 200 {
		return Project{}, fmt.Errorf("%w: default branch", ErrInvalid)
	}
	if !uuidRe.MatchString(string(ws)) {
		return Project{}, ErrNotFound
	}
	var p Project
	err := s.db.Q(ctx).QueryRow(ctx,
		`INSERT INTO projects (workspace_id, name, default_branch) VALUES ($1, $2, $3)
		 RETURNING id, workspace_id, name, default_branch, created_at`, string(ws), name, defaultBranch,
	).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.DefaultBranch, &p.CreatedAt)
	return p, mapErr(err)
}

func (s *service) GetProject(ctx context.Context, id ProjectID) (Project, error) {
	if !uuidRe.MatchString(string(id)) {
		return Project{}, ErrNotFound
	}
	var p Project
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT id, workspace_id, name, default_branch, created_at FROM projects WHERE id = $1`, string(id),
	).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.DefaultBranch, &p.CreatedAt)
	return p, mapErr(err)
}

func (s *service) ProjectScope(ctx context.Context, id ProjectID) (Scope, error) {
	if !uuidRe.MatchString(string(id)) {
		return Scope{}, ErrNotFound
	}
	var sc Scope
	// Both tables are this module's own (§6.9.5), so the join is legal.
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT w.org_id, p.workspace_id, p.id
		   FROM projects p JOIN workspaces w ON w.id = p.workspace_id
		  WHERE p.id = $1`, string(id),
	).Scan(&sc.OrgID, &sc.WorkspaceID, &sc.ProjectID)
	return sc, mapErr(err)
}

func (s *service) EnsureEnvironment(ctx context.Context, project ProjectID, key string) (Environment, error) {
	if !ValidSlug(key) {
		return Environment{}, fmt.Errorf("%w: environment key %q", ErrInvalid, key)
	}
	if !uuidRe.MatchString(string(project)) {
		return Environment{}, ErrNotFound
	}
	q := s.db.Q(ctx)
	var e Environment
	// DO NOTHING + RETURNING yields no row when the key already exists;
	// the second statement then reads it. Two reporters seeing a new key at
	// once both succeed: one inserts, the other reads what it inserted.
	err := q.QueryRow(ctx,
		`INSERT INTO environments (project_id, key) VALUES ($1, $2)
		 ON CONFLICT (project_id, key) DO NOTHING
		 RETURNING id, project_id, key, created_at`, string(project), key,
	).Scan(&e.ID, &e.ProjectID, &e.Key, &e.CreatedAt)
	if err == nil {
		return e, nil
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return Environment{}, mapErr(err)
	}
	err = q.QueryRow(ctx,
		`SELECT id, project_id, key, created_at FROM environments WHERE project_id = $1 AND key = $2`,
		string(project), key,
	).Scan(&e.ID, &e.ProjectID, &e.Key, &e.CreatedAt)
	return e, mapErr(err)
}

// mapErr turns driver errors into the module's sentinels. A foreign-key
// violation means the parent id is unknown: not found, not "invalid".
func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pg.ErrNoRows):
		return ErrNotFound
	case pg.IsUniqueViolation(err):
		return ErrConflict
	case pg.IsForeignKeyViolation(err):
		return ErrNotFound
	case pg.IsCheckViolation(err):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}

func (s *service) GetOrganization(ctx context.Context, id OrganizationID) (Organization, error) {
	if !uuidRe.MatchString(string(id)) {
		return Organization{}, ErrNotFound
	}
	var o Organization
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT id, name, slug, plan_id, created_at FROM organizations WHERE id = $1`, string(id),
	).Scan(&o.ID, &o.Name, &o.Slug, &o.PlanID, &o.CreatedAt)
	return o, mapErr(err)
}

func (s *service) GetWorkspace(ctx context.Context, id WorkspaceID) (Workspace, error) {
	if !uuidRe.MatchString(string(id)) {
		return Workspace{}, ErrNotFound
	}
	var w Workspace
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT id, org_id, name, created_at FROM workspaces WHERE id = $1`, string(id),
	).Scan(&w.ID, &w.OrgID, &w.Name, &w.CreatedAt)
	return w, mapErr(err)
}

func (s *service) ListWorkspaces(ctx context.Context, org OrganizationID) ([]Workspace, error) {
	if !uuidRe.MatchString(string(org)) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx,
		`SELECT id, org_id, name, created_at FROM workspaces WHERE org_id = $1 ORDER BY name, id`, string(org))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.OrgID, &w.Name, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *service) ListProjects(ctx context.Context, level string, id string) ([]Project, error) {
	if !uuidRe.MatchString(id) {
		return nil, ErrNotFound
	}
	var where string
	switch level {
	case "organization":
		where = `w.org_id = $1`
	case "workspace":
		where = `p.workspace_id = $1`
	case "project":
		where = `p.id = $1`
	default:
		return nil, fmt.Errorf("%w: level %q", ErrInvalid, level)
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT p.id, p.workspace_id, w.name, p.name, p.default_branch, p.created_at
		  FROM projects p JOIN workspaces w ON w.id = p.workspace_id
		 WHERE `+where+` ORDER BY p.name, p.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.WorkspaceID, &p.WorkspaceName, &p.Name, &p.DefaultBranch, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *service) ListEnvironments(ctx context.Context, project ProjectID) ([]Environment, error) {
	if !uuidRe.MatchString(string(project)) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT id, project_id, key, created_at FROM environments WHERE project_id = $1 ORDER BY key`, string(project))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		var e Environment
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Key, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
