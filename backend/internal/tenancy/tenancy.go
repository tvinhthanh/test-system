// Package tenancy owns the Organization → Workspace → Project → Environment
// hierarchy (§6.3, §8.2) and the four tables behind it (§6.9.5). Everything
// else in the system hangs off a ProjectID: ingest tokens, test identity,
// runs. Run itself is not a level here — it is data, not a boundary (§8.8).
//
// This file is the module's whole contract (§6.9.4). The interface lives on
// the owner side because more than one module needs it — authz walks the
// scope chain, ingest resolves environmentKey, query reads names.
//
// tenancy imports only platform/* (§6.9.3); the architecture test enforces
// that, and the compiler enforces that nothing below this file is reachable
// from outside.
package tenancy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// IDs are the canonical text form of a uuid. Distinct types so a
// WorkspaceID cannot be passed where a ProjectID is expected.
type (
	OrganizationID string
	WorkspaceID    string
	ProjectID      string
	EnvironmentID  string
)

// Organization is the billing unit (§8.2).
type Organization struct {
	ID        OrganizationID
	Name      string
	Slug      string
	PlanID    string
	CreatedAt time.Time
}

// Workspace groups projects by department or product.
type Workspace struct {
	ID        WorkspaceID
	OrgID     OrganizationID
	Name      string
	CreatedAt time.Time
}

// Project is the unit of ingest tokens and test identity.
type Project struct {
	ID            ProjectID
	WorkspaceID   WorkspaceID
	WorkspaceName string // filled by ListProjects; "" from Create/Get
	Name          string
	DefaultBranch string
	CreatedAt     time.Time
}

// DeletedProject is a project in its restore window.
type DeletedProject struct {
	Project
	OrgID      OrganizationID
	DeletedAt  time.Time
	DeletedBy  string
	PurgeAfter time.Time
}

// DeletedOrganization is an organization in its restore window, with the
// workspaces under it (what the purge removes with it).
type DeletedOrganization struct {
	Organization
	DeletedAt    time.Time
	DeletedBy    string
	PurgeAfter   time.Time
	WorkspaceIDs []WorkspaceID
}

// Environment separates histories of the same test (§8.2). Key is what the
// reporter sends as environmentKey (§7.3). BaseURL is the address of the
// system under test there (QĐ-14), "" when nobody recorded one.
type Environment struct {
	ID        EnvironmentID
	ProjectID ProjectID
	Key       string
	BaseURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Scope is a project's ancestry — the chain authorize() walks up (§8.8).
type Scope struct {
	OrgID       OrganizationID
	WorkspaceID WorkspaceID
	ProjectID   ProjectID
}

var (
	// ErrNotFound: the id does not exist. Callers that must not reveal
	// existence across tenants (§7.2: wrong project → 404) map it themselves.
	ErrNotFound = errors.New("tenancy: not found")
	// ErrConflict: a unique field (slug, name within parent, key within
	// project) is already taken.
	ErrConflict = errors.New("tenancy: already exists")
	// ErrInvalid: input failed shape validation before reaching the database.
	ErrInvalid = errors.New("tenancy: invalid input")
)

// Service is what other modules program against. Writes take the caller's
// transaction from ctx (§6.9.6) and never open their own.
type Service interface {
	CreateOrganization(ctx context.Context, name, slug string) (Organization, error)
	CreateWorkspace(ctx context.Context, org OrganizationID, name string) (Workspace, error)
	CreateProject(ctx context.Context, ws WorkspaceID, name, defaultBranch string) (Project, error)

	// Deleting a project (§11.3.2) is soft: from DeleteProject on, no read
	// of this module returns it — GetProject, ProjectScope and ListProjects
	// skip it, so every route answers 404 — until RestoreProject, or until
	// the worker purges it after keep. PurgeProject removes this module's
	// rows (environments, the project) once the other modules have removed
	// theirs; it refuses a project that is not deleted.
	DeleteProject(ctx context.Context, id ProjectID, actor string, keep time.Duration) (DeletedProject, error)
	RestoreProject(ctx context.Context, id ProjectID) (Project, error)
	DeletedProjects(ctx context.Context, ws WorkspaceID) ([]DeletedProject, error)
	ProjectsToPurge(ctx context.Context, now time.Time, limit int) ([]DeletedProject, error)
	PurgeProject(ctx context.Context, id ProjectID) error

	// UpdateProject renames a live project or changes its default branch;
	// "" keeps a field. A name taken in the workspace is ErrConflict.
	UpdateProject(ctx context.Context, id ProjectID, name, defaultBranch string) (Project, error)

	// Deleting an organization (§11.3.2 "xóa org") is soft the same way:
	// from DeleteOrganization on, the org, its workspaces and its projects
	// are gone from every read above, until RestoreOrganization or the
	// purge after keep. A project deleted on its own before stays deleted
	// after a restore. DeletedOrganizations returns those of ids that are
	// deleted. OrganizationProjects is every project of a deleted org,
	// deleted or not, for the purge; PurgeProject accepts those too.
	// PurgeOrganization removes the workspaces and the org once no
	// project is left; it refuses an org that is not deleted.
	DeleteOrganization(ctx context.Context, id OrganizationID, actor string, keep time.Duration) (DeletedOrganization, error)
	RestoreOrganization(ctx context.Context, id OrganizationID) (Organization, error)
	DeletedOrganizations(ctx context.Context, ids []OrganizationID) ([]DeletedOrganization, error)
	OrganizationsToPurge(ctx context.Context, now time.Time, limit int) ([]DeletedOrganization, error)
	OrganizationProjects(ctx context.Context, id OrganizationID) ([]DeletedProject, error)
	PurgeOrganization(ctx context.Context, id OrganizationID) error

	GetProject(ctx context.Context, id ProjectID) (Project, error)
	GetOrganization(ctx context.Context, id OrganizationID) (Organization, error)
	GetWorkspace(ctx context.Context, id WorkspaceID) (Workspace, error)
	// ListWorkspaces returns an organization's workspaces by name — for the
	// team screens, where an org admin picks the workspace to manage.
	ListWorkspaces(ctx context.Context, org OrganizationID) ([]Workspace, error)
	// ListProjects returns the projects under a node of the tree — all of an
	// organization, one workspace, or just the one project — which is how a
	// membership at that level maps to visible projects (§11.3.1).
	ListProjects(ctx context.Context, level string, id string) ([]Project, error)
	ListEnvironments(ctx context.Context, project ProjectID) ([]Environment, error)
	// ProjectScope returns the ancestry of a project, or ErrNotFound.
	ProjectScope(ctx context.Context, id ProjectID) (Scope, error)

	// EnsureEnvironment returns the environment with the given key, creating
	// it on first sight. Ingest calls this for every POST /v1/runs (§7.3), so
	// it must be idempotent under concurrent first sightings.
	EnsureEnvironment(ctx context.Context, project ProjectID, key string) (Environment, error)
	// CreateEnvironment adds a named environment from the dashboard, with
	// the address testers open there (QĐ-14). A key already in the project
	// is ErrConflict; a bad key or URL is ErrInvalid.
	CreateEnvironment(ctx context.Context, project ProjectID, key, baseURL string) (Environment, error)
	// SetEnvironmentURL replaces an environment's address; "" clears it.
	SetEnvironmentURL(ctx context.Context, project ProjectID, id EnvironmentID, baseURL string) (Environment, error)
}

// NormalizeBaseURL checks an address of a system under test and returns it
// trimmed, without a trailing slash: http or https, a host, no credentials
// (they would be shown to every reader of the project), at most 2048 bytes.
// "" stays "" — no address.
func NormalizeBaseURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	switch {
	case err != nil, len(s) > 2048, u.Host == "", u.Hostname() == "",
		u.Scheme != "http" && u.Scheme != "https":
		return "", fmt.Errorf("%w: baseUrl must be an http(s) address with a host", ErrInvalid)
	case u.User != nil:
		return "", fmt.Errorf("%w: baseUrl must not carry a user or password", ErrInvalid)
	case strings.ContainsAny(s, " \t\r\n"):
		return "", fmt.Errorf("%w: baseUrl must not contain spaces", ErrInvalid)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return strings.TrimSuffix(u.String(), "/"), nil
}

// slugRe is the shape shared by Organization.Slug and Environment.Key: lower
// case, digits, single hyphens between segments. Mirrors the CHECK constraint
// in migrations/0001_tenancy.sql so bad input is refused before a round trip.
var slugRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ValidSlug reports whether s can be an organization slug or environment key.
func ValidSlug(s string) bool { return len(s) <= 64 && slugRe.MatchString(s) }

// ValidName reports whether s can be a display name.
func ValidName(s string) bool { return len(s) >= 1 && len(s) <= 200 }
