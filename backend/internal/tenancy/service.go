package tenancy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

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

// liveOrg is true for a project (aliased p) whose organization is not
// deleted: every read that hands out a project carries it, so deleting an
// organization hides its whole tree at once (§11.3.2 "xóa org").
const liveOrg = `EXISTS (SELECT 1 FROM workspaces lw JOIN organizations lo ON lo.id = lw.org_id
	 WHERE lw.id = p.workspace_id AND lo.deleted_at IS NULL)`

func (s *service) GetProject(ctx context.Context, id ProjectID) (Project, error) {
	if !uuidRe.MatchString(string(id)) {
		return Project{}, ErrNotFound
	}
	var p Project
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT id, workspace_id, name, default_branch, created_at FROM projects p WHERE id = $1 AND deleted_at IS NULL AND `+liveOrg, string(id),
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
		  WHERE p.id = $1 AND p.deleted_at IS NULL AND `+liveOrg, string(id),
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
		`SELECT id, name, slug, plan_id, created_at FROM organizations WHERE id = $1 AND deleted_at IS NULL`, string(id),
	).Scan(&o.ID, &o.Name, &o.Slug, &o.PlanID, &o.CreatedAt)
	return o, mapErr(err)
}

func (s *service) GetWorkspace(ctx context.Context, id WorkspaceID) (Workspace, error) {
	if !uuidRe.MatchString(string(id)) {
		return Workspace{}, ErrNotFound
	}
	var w Workspace
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT w.id, w.org_id, w.name, w.created_at FROM workspaces w JOIN organizations o ON o.id = w.org_id
		  WHERE w.id = $1 AND o.deleted_at IS NULL`, string(id),
	).Scan(&w.ID, &w.OrgID, &w.Name, &w.CreatedAt)
	return w, mapErr(err)
}

func (s *service) ListWorkspaces(ctx context.Context, org OrganizationID) ([]Workspace, error) {
	if !uuidRe.MatchString(string(org)) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx,
		`SELECT w.id, w.org_id, w.name, w.created_at FROM workspaces w JOIN organizations o ON o.id = w.org_id
		  WHERE w.org_id = $1 AND o.deleted_at IS NULL ORDER BY w.name, w.id`, string(org))
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
		 WHERE `+where+` AND p.deleted_at IS NULL AND `+liveOrg+` ORDER BY p.name, p.id`, id)
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

func (s *service) UpdateProject(ctx context.Context, id ProjectID, name, defaultBranch string) (Project, error) {
	if name != "" && !ValidName(name) {
		return Project{}, fmt.Errorf("%w: project name", ErrInvalid)
	}
	if len(defaultBranch) > 200 {
		return Project{}, fmt.Errorf("%w: default branch", ErrInvalid)
	}
	if !uuidRe.MatchString(string(id)) {
		return Project{}, ErrNotFound
	}
	var p Project
	err := s.db.Q(ctx).QueryRow(ctx, `
		UPDATE projects p SET name = coalesce(nullif($2, ''), name), default_branch = coalesce(nullif($3, ''), default_branch)
		 WHERE id = $1 AND deleted_at IS NULL AND `+liveOrg+`
		RETURNING id, workspace_id, name, default_branch, created_at`, string(id), name, defaultBranch,
	).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.DefaultBranch, &p.CreatedAt)
	return p, mapErr(err)
}

// ---- soft delete (§11.3.2 "Tạo/xóa project") --------------------------------

const deletedColumns = `p.id, p.workspace_id, w.name, p.name, p.default_branch, p.created_at, w.org_id, p.deleted_at, p.deleted_by, p.purge_after`

func scanDeleted(row interface{ Scan(...any) error }) (DeletedProject, error) {
	var d DeletedProject
	err := row.Scan(&d.ID, &d.WorkspaceID, &d.WorkspaceName, &d.Name, &d.DefaultBranch, &d.CreatedAt, &d.OrgID, &d.DeletedAt, &d.DeletedBy, &d.PurgeAfter)
	d.DeletedAt, d.PurgeAfter = d.DeletedAt.UTC(), d.PurgeAfter.UTC()
	return d, err
}

func (s *service) DeleteProject(ctx context.Context, id ProjectID, actor string, keep time.Duration) (DeletedProject, error) {
	if !uuidRe.MatchString(string(id)) {
		return DeletedProject{}, ErrNotFound
	}
	if strings.TrimSpace(actor) == "" || keep < time.Hour {
		return DeletedProject{}, fmt.Errorf("%w: actor and a restore window of at least an hour", ErrInvalid)
	}
	now := time.Now().UTC()
	d, err := scanDeleted(s.db.Q(ctx).QueryRow(ctx, `
		WITH d AS (
			UPDATE projects SET deleted_at = $2, deleted_by = $3, purge_after = $4
			 WHERE id = $1 AND deleted_at IS NULL
			RETURNING *)
		SELECT `+deletedColumns+` FROM d p JOIN workspaces w ON w.id = p.workspace_id`,
		string(id), now, strings.TrimSpace(actor), now.Add(keep)))
	return d, mapErr(err)
}

func (s *service) RestoreProject(ctx context.Context, id ProjectID) (Project, error) {
	if !uuidRe.MatchString(string(id)) {
		return Project{}, ErrNotFound
	}
	var p Project
	err := s.db.Q(ctx).QueryRow(ctx, `
		UPDATE projects SET deleted_at = NULL, deleted_by = NULL, purge_after = NULL
		 WHERE id = $1 AND deleted_at IS NOT NULL
		RETURNING id, workspace_id, name, default_branch, created_at`, string(id),
	).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.DefaultBranch, &p.CreatedAt)
	// A live project took the name meanwhile: projects_live_name says so.
	return p, mapErr(err)
}

func (s *service) DeletedProjects(ctx context.Context, ws WorkspaceID) ([]DeletedProject, error) {
	if !uuidRe.MatchString(string(ws)) {
		return nil, ErrNotFound
	}
	return s.listDeleted(ctx, `p.workspace_id = $1`, string(ws), 500)
}

func (s *service) ProjectsToPurge(ctx context.Context, now time.Time, limit int) ([]DeletedProject, error) {
	return s.listDeleted(ctx, `p.purge_after <= $1`, now.UTC(), limit)
}

func (s *service) listDeleted(ctx context.Context, where string, arg any, limit int) ([]DeletedProject, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+deletedColumns+`
		  FROM projects p JOIN workspaces w ON w.id = p.workspace_id
		 WHERE p.deleted_at IS NOT NULL AND `+where+`
		 ORDER BY p.deleted_at DESC LIMIT `+fmt.Sprint(limit), arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeletedProject{}
	for rows.Next() {
		d, err := scanDeleted(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *service) PurgeProject(ctx context.Context, id ProjectID) error {
	if !uuidRe.MatchString(string(id)) {
		return ErrNotFound
	}
	var deleted bool
	// Deleted on its own, or under a deleted organization (its purge).
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT p.deleted_at IS NOT NULL OR NOT `+liveOrg+` FROM projects p WHERE p.id = $1`, string(id)).Scan(&deleted)
	if err != nil {
		return mapErr(err)
	}
	if !deleted {
		return fmt.Errorf("%w: project %s is not deleted", ErrConflict, id)
	}
	if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM environments WHERE project_id = $1`, string(id)); err != nil {
		return err
	}
	_, err = s.db.Q(ctx).Exec(ctx, `DELETE FROM projects WHERE id = $1`, string(id))
	return err
}

// ---- organization soft delete (§11.3.2 "Billing, xóa org") -------------------

const deletedOrgColumns = `o.id, o.name, o.slug, o.plan_id, o.created_at, o.deleted_at, o.deleted_by, o.purge_after,
	coalesce((SELECT array_agg(w.id::text ORDER BY w.id) FROM workspaces w WHERE w.org_id = o.id), '{}')`

func scanDeletedOrg(row interface{ Scan(...any) error }) (DeletedOrganization, error) {
	var d DeletedOrganization
	var ws []string
	err := row.Scan(&d.ID, &d.Name, &d.Slug, &d.PlanID, &d.CreatedAt, &d.DeletedAt, &d.DeletedBy, &d.PurgeAfter, &ws)
	d.DeletedAt, d.PurgeAfter = d.DeletedAt.UTC(), d.PurgeAfter.UTC()
	for _, w := range ws {
		d.WorkspaceIDs = append(d.WorkspaceIDs, WorkspaceID(w))
	}
	return d, err
}

func (s *service) DeleteOrganization(ctx context.Context, id OrganizationID, actor string, keep time.Duration) (DeletedOrganization, error) {
	if !uuidRe.MatchString(string(id)) {
		return DeletedOrganization{}, ErrNotFound
	}
	if strings.TrimSpace(actor) == "" || keep < time.Hour {
		return DeletedOrganization{}, fmt.Errorf("%w: actor and a restore window of at least an hour", ErrInvalid)
	}
	now := time.Now().UTC()
	d, err := scanDeletedOrg(s.db.Q(ctx).QueryRow(ctx, `
		WITH o AS (
			UPDATE organizations SET deleted_at = $2, deleted_by = $3, purge_after = $4
			 WHERE id = $1 AND deleted_at IS NULL
			RETURNING *)
		SELECT `+deletedOrgColumns+` FROM o`,
		string(id), now, strings.TrimSpace(actor), now.Add(keep)))
	return d, mapErr(err)
}

func (s *service) RestoreOrganization(ctx context.Context, id OrganizationID) (Organization, error) {
	if !uuidRe.MatchString(string(id)) {
		return Organization{}, ErrNotFound
	}
	var o Organization
	err := s.db.Q(ctx).QueryRow(ctx, `
		UPDATE organizations SET deleted_at = NULL, deleted_by = NULL, purge_after = NULL
		 WHERE id = $1 AND deleted_at IS NOT NULL
		RETURNING id, name, slug, plan_id, created_at`, string(id),
	).Scan(&o.ID, &o.Name, &o.Slug, &o.PlanID, &o.CreatedAt)
	return o, mapErr(err)
}

func (s *service) DeletedOrganizations(ctx context.Context, ids []OrganizationID) ([]DeletedOrganization, error) {
	list := make([]string, 0, len(ids))
	for _, id := range ids {
		if uuidRe.MatchString(string(id)) {
			list = append(list, string(id))
		}
	}
	return s.listDeletedOrgs(ctx, `o.id::text = ANY($1)`, list, 500)
}

func (s *service) OrganizationsToPurge(ctx context.Context, now time.Time, limit int) ([]DeletedOrganization, error) {
	return s.listDeletedOrgs(ctx, `o.purge_after <= $1`, now.UTC(), limit)
}

func (s *service) listDeletedOrgs(ctx context.Context, where string, arg any, limit int) ([]DeletedOrganization, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+deletedOrgColumns+`
		  FROM organizations o
		 WHERE o.deleted_at IS NOT NULL AND `+where+`
		 ORDER BY o.deleted_at DESC LIMIT `+fmt.Sprint(limit), arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeletedOrganization{}
	for rows.Next() {
		d, err := scanDeletedOrg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *service) OrganizationProjects(ctx context.Context, id OrganizationID) ([]DeletedProject, error) {
	if !uuidRe.MatchString(string(id)) {
		return nil, ErrNotFound
	}
	// A project deleted on its own keeps its own deletion record; the
	// others carry the organization's.
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT p.id, p.workspace_id, w.name, p.name, p.default_branch, p.created_at, w.org_id,
		       coalesce(p.deleted_at, o.deleted_at), coalesce(p.deleted_by, o.deleted_by), coalesce(p.purge_after, o.purge_after)
		  FROM projects p JOIN workspaces w ON w.id = p.workspace_id JOIN organizations o ON o.id = w.org_id
		 WHERE o.id = $1 AND o.deleted_at IS NOT NULL
		 ORDER BY p.id`, string(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeletedProject{}
	for rows.Next() {
		d, err := scanDeleted(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *service) PurgeOrganization(ctx context.Context, id OrganizationID) error {
	if !uuidRe.MatchString(string(id)) {
		return ErrNotFound
	}
	var deleted bool
	var projects int
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT o.deleted_at IS NOT NULL,
		       (SELECT count(*) FROM projects p JOIN workspaces w ON w.id = p.workspace_id WHERE w.org_id = o.id)
		  FROM organizations o WHERE o.id = $1`, string(id)).Scan(&deleted, &projects)
	if err != nil {
		return mapErr(err)
	}
	if !deleted {
		return fmt.Errorf("%w: organization %s is not deleted", ErrConflict, id)
	}
	if projects > 0 {
		return fmt.Errorf("%w: organization %s still has %d projects", ErrConflict, id, projects)
	}
	if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM workspaces WHERE org_id = $1`, string(id)); err != nil {
		return err
	}
	_, err = s.db.Q(ctx).Exec(ctx, `DELETE FROM organizations WHERE id = $1`, string(id))
	return err
}
