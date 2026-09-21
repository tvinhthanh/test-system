// Package authz owns who-may-do-what: API tokens (§8.8, §11.4), memberships
// with the three P1-min roles (§4.4, §11.3), and the single authorize()
// function every endpoint goes through (§11.3.3: "no endpoint writes its own
// if-check — that is the only way no endpoint can forget").
//
// This file is the module's whole contract (§6.9.4). authz imports only
// platform/* (§6.9.3), so it does not know tenancy's types: the caller
// resolves a project to its ancestry with tenancy.ProjectScope and hands the
// ids over as a Resource. That is the one place the two modules touch.
package authz

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// Level is where a token or membership is attached. Run is not a level:
// it is data, not a permission boundary (§8.8).
type Level string

const (
	LevelOrganization Level = "organization"
	LevelWorkspace    Level = "workspace"
	LevelProject      Level = "project"
	LevelEnvironment  Level = "environment" // tokens only; memberships stop at project
)

// Role is the P1-min set (§4.4): six roles of §11.3.1 collapsed to three.
// admin absorbs owner/admin/ws_admin/maintainer; member and viewer are as
// in the matrix §11.3.2.
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
	RoleViewer Role = "viewer"
)

// Permission is what an API token carries (§8.8). Tokens never get roles.
type Permission string

const (
	PermProjectIngest Permission = "project:ingest" // write-only, the default for ingest tokens
	PermProjectRead   Permission = "project:read"
)

// Action is a row of the matrix §11.3.2, P1 columns only.
type Action string

const (
	ActionView             Action = "view"              // runs, tests, artifacts, dashboard
	ActionTriage           Action = "triage"            // accept/reject, mark flaky, assign
	ActionIdentityMerge    Action = "identity.merge"    // merge/split test identity
	ActionTokenManage      Action = "token.manage"      // create/revoke ingest token
	ActionProjectConfigure Action = "project.configure" // retention, settings
	ActionProjectManage    Action = "project.manage"    // create/delete project
	ActionMemberManage     Action = "member.manage"
	ActionOrgManage        Action = "org.manage" // billing, delete org
	ActionDataDelete       Action = "data.delete"
	ActionIngest           Action = "ingest" // tokens only: POST /v1/runs and friends
	// P2 (§6.8, QĐ-12): manual test management is daily work like triage;
	// the release decision is a named override and sits with maintainers.
	ActionTestmgmtWrite Action = "testmgmt.write" // cases, requirements, plans, executions, imports
	ActionReleaseDecide Action = "release.decide" // approve/block/ship a release

)

// Resource is the ancestry of the thing being acted on, as ids. Environment
// is optional; the others are required. Build it from tenancy.ProjectScope.
type Resource struct {
	OrgID         string
	WorkspaceID   string
	ProjectID     string
	EnvironmentID string
}

// ScopeRef names one node of the tree.
type ScopeRef struct {
	Level Level
	ID    string
}

// PrincipalKind distinguishes a signed-in human from a token on a CI box.
type PrincipalKind string

const (
	PrincipalUser  PrincipalKind = "user"
	PrincipalToken PrincipalKind = "token"
)

// Principal is who is asking. Exactly one of UserID / TokenID is set.
type Principal struct {
	Kind   PrincipalKind
	UserID string
	// Token fields, populated by Authenticate.
	TokenID     string
	TokenScope  ScopeRef
	Permissions []Permission
}

// User is a dashboard account (§11.2). P1-min creates users by script and
// marks them verified there; self-service signup with a verification mail
// comes with the client.
type User struct {
	ID              string
	Email           string
	EmailVerifiedAt time.Time // zero until verified; login refuses before
	HasPassword     bool
	CreatedAt       time.Time
}

// Session is one browser login. The secret is the cookie value, stored
// only as an HMAC (§11.4 style).
type Session struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	RevokedAt  time.Time
	IP         string
	UserAgent  string
}

// SessionMeta is what Login records about the client and rate-limits on.
type SessionMeta struct {
	IP        string
	UserAgent string
}

// Membership grants a role at a node; it inherits down the tree.
type Membership struct {
	ID        string
	UserID    string
	Scope     ScopeRef
	Role      Role
	CreatedAt time.Time
}

// Token is the stored half of an API token. The secret is never stored and
// never returned after issue (§11.4).
type Token struct {
	ID          string
	Prefix      string
	Scope       ScopeRef
	Permissions []Permission
	Name        string
	CreatedBy   string // user id, or "" for a token issued by script
	CreatedAt   time.Time
	LastUsedAt  time.Time // zero until first use
	ExpiresAt   time.Time
	RevokedAt   time.Time // zero while live
}

// TokenSpec is what IssueToken needs. Zero ExpiresAt means one year (§11.4).
type TokenSpec struct {
	Scope       ScopeRef
	Permissions []Permission
	Name        string
	CreatedBy   string
	ExpiresAt   time.Time
}

// AuditEntry is one row of authz_audit — a destructive change (token or
// role revoked), who did it and on what (§11.7). Append-only; written in
// the same transaction as the change.
type AuditEntry struct {
	ID          int64
	Action      string // token.revoke | role.revoke
	SubjectKind string // api_token | membership
	SubjectID   string
	Scope       ScopeRef // the subject's scope, for filtering by tenant
	Actor       string
	Details     map[string]any
	At          time.Time
}

// Issued pairs the stored token with its plaintext — shown once, then gone.
type Issued struct {
	Token     Token
	Plaintext string
}

var (
	// ErrUnauthenticated: no usable credential (→ 401). Deliberately one
	// error for missing, malformed, unknown, expired and revoked: the client
	// learns nothing beyond "this token does not work" (§11.2 spirit).
	ErrUnauthenticated = errors.New("authz: unauthenticated")
	// ErrForbidden: the principal can see the resource but not do this
	// (→ 403, §11.3.2).
	ErrForbidden = errors.New("authz: forbidden")
	// ErrNoAccess: the principal has no relationship with the resource's
	// tenant at all (→ 404, so existence is not revealed; §7.2, §11.3.2).
	ErrNoAccess = errors.New("authz: no access")
	// ErrNotFound: a user, membership or token id does not exist.
	ErrNotFound = errors.New("authz: not found")
	// ErrConflict: email or (user, scope) already taken.
	ErrConflict = errors.New("authz: already exists")
	// ErrRateLimited: too many login failures for this IP or account; the
	// client must wait (§11.2: 5 per 15 min, then backoff).
	ErrRateLimited = errors.New("authz: too many attempts")
	// ErrInvalid: input failed shape validation.
	ErrInvalid = errors.New("authz: invalid input")
)

// Service is the contract. Writes join the caller's transaction (§6.9.6).
type Service interface {
	// Authorize is THE check (§11.3.3). It returns nil, ErrForbidden or
	// ErrNoAccess. The caller must have validated that the Resource ids are
	// real and related; authz only asks "may this principal act here".
	Authorize(ctx context.Context, p Principal, r Resource, a Action) error

	// Authenticate turns a bearer token into a Principal, or
	// ErrUnauthenticated. It updates last_used_at at most every 5 minutes.
	Authenticate(ctx context.Context, bearer string) (Principal, error)

	IssueToken(ctx context.Context, spec TokenSpec) (Issued, error)
	// RevokeToken makes the token unusable at once and records who did it
	// (§11.7 audit of destructive actions); actor is required.
	RevokeToken(ctx context.Context, id, actor string) error
	ListTokens(ctx context.Context, scope ScopeRef) ([]Token, error)
	// ExpiringTokens counts live (unrevoked, unexpired) tokens that expire
	// within d — §11.4 "nhắc trước 30 ngày", read by the api's health gauges.
	ExpiringTokens(ctx context.Context, within time.Duration) (int, error)

	CreateUser(ctx context.Context, email string) (User, error)
	GetUserByEmail(ctx context.Context, email string) (User, error)
	GetUser(ctx context.Context, id string) (User, error)
	// VerifyEmail marks the address verified (§11.2: required before an
	// account may create or join an org, and before it may log in).
	VerifyEmail(ctx context.Context, userID string) error
	// SetPassword stores an argon2id hash and revokes every session of the
	// user except keepSession ("" revokes all) — §11.2 "đổi password → revoke
	// mọi session khác".
	SetPassword(ctx context.Context, userID, password, keepSession string) error

	// Login checks email + password and opens a session. Every failure is
	// ErrUnauthenticated — unknown email, wrong password, unverified email —
	// so the response cannot enumerate accounts; the rate limit counts
	// failures per IP and per account and answers ErrRateLimited once
	// exceeded. The returned secret is the cookie value, shown once.
	Login(ctx context.Context, email, password string, meta SessionMeta) (session Session, secret string, err error)
	// AuthenticateSession turns a cookie value into a user Principal.
	AuthenticateSession(ctx context.Context, secret string) (Principal, Session, error)
	Logout(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context, userID string) ([]Session, error)
	GrantRole(ctx context.Context, userID string, scope ScopeRef, role Role) (Membership, error)
	// RevokeRole removes a membership and records who did it (§11.7).
	RevokeRole(ctx context.Context, userID string, scope ScopeRef, actor string) error
	ListMemberships(ctx context.Context, userID string) ([]Membership, error)
	// ListAudit reads the newest destructive actions (§11.7), newest first,
	// filtered to one scope id when given.
	ListAudit(ctx context.Context, scopeID string, limit int) ([]AuditEntry, error)
}

// TokenPrefix is what every ingest token starts with. It is registered with
// secret-scanning services (§11.4) — change it and the scanners go blind.
const TokenPrefix = "atp_ing_"

// PrincipalFrom returns the Principal placed in ctx by Middleware.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// WithPrincipal returns ctx carrying p — for tests and for the session
// middleware that will follow the login use case.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// Middleware authenticates `Authorization: Bearer <token>` with svc and puts
// the Principal in ctx, or answers 401 in the §7.1 error envelope. It only
// authenticates; each handler still calls Authorize for its resource.
func Middleware(svc Service) func(http.Handler) http.Handler {
	return bearerMiddleware(svc)
}
