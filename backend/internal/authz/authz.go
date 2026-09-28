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

// Role is §11.3.1: six flat roles, each attached at exactly one level of
// the tree (RoleLevel) and inherited downwards. P1-min folded them to
// admin/member/viewer; migration 0015 unfolded the stored rows (§4.4 "RBAC
// 6 role — P2 mở rộng"). Custom roles are P6.
type Role string

const (
	RoleOrgOwner       Role = "org_owner"       // organization: billing, delete org, manage owners
	RoleOrgAdmin       Role = "org_admin"       // organization: workspaces, members, tokens org-wide
	RoleWorkspaceAdmin Role = "workspace_admin" // workspace: projects + members in it
	RoleMaintainer     Role = "maintainer"      // project: tokens, retention, identity merge, data delete
	RoleMember         Role = "member"          // project: triage, test management, everything readable
	RoleViewer         Role = "viewer"          // project: read only
)

// Roles lists every role, strongest first — for CLIs and docs.
var Roles = []Role{RoleOrgOwner, RoleOrgAdmin, RoleWorkspaceAdmin, RoleMaintainer, RoleMember, RoleViewer}

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
	// MFAEnrollmentRequired: a session of an org owner who has not turned
	// on MFA (§11.2). SessionMiddleware lets it reach only the session and
	// MFA routes.
	MFAEnrollmentRequired bool
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

// MFAStatus is what the account page shows. Required: the user is an org
// owner somewhere (§11.2), so MFA cannot be turned off.
type MFAStatus struct {
	Enabled           bool
	Required          bool
	RecoveryCodesLeft int
}

// MFASetup is shown once, between "set up" and the first valid code.
type MFASetup struct {
	Secret string // base32, for typing into an authenticator app
	URI    string // otpauth://totp/…, for a QR code or a tap on a phone
}

// MFARequired is Login's answer for an account with MFA on: the password
// was right, no session exists yet, and Challenge (shown once, valid five
// minutes) goes back with a code to CompleteMFALogin. Use errors.As.
type MFARequired struct {
	Challenge string
	ExpiresAt time.Time
}

func (e *MFARequired) Error() string { return "authz: second factor required" }

// ExternalIdentity is who an OAuth provider says signed in (§11.2).
type ExternalIdentity struct {
	Provider      string // google | github
	Subject       string // the provider's stable user id — never the email
	Email         string
	EmailVerified bool
}

// Invitation is an open invitation to a role on a node, mailed to an email
// with no account yet (§11.2). The token is never stored or returned again.
type Invitation struct {
	ID        string
	Email     string
	Scope     ScopeRef
	Role      Role
	InvitedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// LinkedIdentity is a Google or GitHub sign-in linked to an account.
type LinkedIdentity struct {
	ID          string
	Provider    string
	Email       string // as the provider reported it when linked
	CreatedAt   time.Time
	LastLoginAt time.Time // zero until used after linking
}

// Membership grants a role at a node; it inherits down the tree.
type Membership struct {
	ID        string
	UserID    string
	Scope     ScopeRef
	Role      Role
	CreatedAt time.Time
}

// Member is one membership that reaches a project, with the user's email:
// the project's own rows and those inherited from its workspace and org —
// everyone Authorize would let in, and through which node.
type Member struct {
	UserID    string
	Email     string
	Role      Role
	Scope     ScopeRef
	CreatedAt time.Time
}

// MemberKey is where a page of Members resumes: the last row's email and
// level (one person may hold a role at several nodes of the chain).
type MemberKey struct {
	Email string
	Level Level
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
// role revoked, or a role granted by a person — a grant can demote), who did
// it and on what (§11.7). Append-only; written in the same transaction as
// the change.
type AuditEntry struct {
	ID          int64
	Action      string // token.revoke | role.revoke | role.grant
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
	// ErrInvalidCode: a TOTP or recovery code that does not match (counted
	// by the login rate limit).
	ErrInvalidCode = errors.New("authz: invalid code")
	// ErrMFAMandatory: an org owner may not turn MFA off (§11.2).
	ErrMFAMandatory = errors.New("authz: MFA is mandatory for org owners")
	// ErrNoAccount: an OAuth identity that is not linked and whose verified
	// email matches no verified account (no self-signup, §11.2).
	ErrNoAccount = errors.New("authz: no account for that identity")
	// ErrLastSignIn: removing this would leave the account no way to sign
	// in (no password, no other linked provider).
	ErrLastSignIn = errors.New("authz: the account's last way to sign in")
	// ErrAccountExists: an invitation was accepted for an email that has an
	// account by now; the role is granted and that account signs in with its
	// own password — a link never takes an account over.
	ErrAccountExists = errors.New("authz: that email has an account; sign in")
	// ErrLastOwner: the change would leave an organization with no owner.
	ErrLastOwner = errors.New("authz: an organization keeps at least one owner")
)

// Service is the contract. Writes join the caller's transaction (§6.9.6).
type Service interface {
	// Authorize is THE check (§11.3.3). It returns nil, ErrForbidden or
	// ErrNoAccess. The caller must have validated that the Resource ids are
	// real and related; authz only asks "may this principal act here".
	Authorize(ctx context.Context, p Principal, r Resource, a Action) error
	// Roles returns the roles a user holds on the chain of r (one query,
	// the same walk-up as Authorize) — for the projects list, which turns
	// them into the actions the UI may show (§11.3.2 "ẩn nút").
	Roles(ctx context.Context, p Principal, r Resource) ([]Role, error)

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
	// exceeded. On an account with MFA on it opens no session and returns
	// *MFARequired instead. The returned secret is the cookie value, shown once.
	Login(ctx context.Context, email, password string, meta SessionMeta) (session Session, secret string, err error)
	// AuthenticateSession turns a cookie value into a user Principal.
	AuthenticateSession(ctx context.Context, secret string) (Principal, Session, error)
	// LoginWithIdentity signs in whoever an OAuth provider vouched for: by
	// the linked (provider, subject), or on first use by the provider's
	// verified email matching a verified account, which links them (audited
	// identity.link). ErrNoAccount otherwise; ErrConflict when the account
	// already has a different identity at that provider. Like Login it
	// returns *MFARequired when the account has MFA on.
	LoginWithIdentity(ctx context.Context, id ExternalIdentity, meta SessionMeta) (session Session, secret string, err error)
	// Identities lists the provider sign-ins linked to a user, oldest first.
	Identities(ctx context.Context, userID string) ([]LinkedIdentity, error)
	// UnlinkIdentity removes one of the user's own links (ErrNotFound for
	// anyone else's), audited identity.unlink; ErrLastSignIn when it is the
	// account's only way in.
	UnlinkIdentity(ctx context.Context, userID, identityID string) error
	// Invite makes an invitation for an email with no account (ErrConflict
	// if it has one), replacing an open one for the same node; the token is
	// returned once, for the mail. Audited invitation.create.
	Invite(ctx context.Context, email string, scope ScopeRef, role Role, actor string) (Invitation, string, error)
	// Invitations lists a node's open invitations, newest first.
	Invitations(ctx context.Context, scope ScopeRef) ([]Invitation, error)
	// RevokeInvitation ends an open invitation on scope (audited).
	RevokeInvitation(ctx context.Context, scope ScopeRef, id, actor string) error
	// LookupInvitation reads an open invitation by token for the accept page;
	// unknown tokens count toward the per-IP limit (ErrRateLimited).
	LookupInvitation(ctx context.Context, token, ip string) (Invitation, error)
	// AcceptInvitation creates the verified account with password, grants
	// the role and opens a session; ErrAccountExists when the email has an
	// account by now (role granted, no session).
	AcceptInvitation(ctx context.Context, token, password string, meta SessionMeta) (session Session, secret string, err error)
	// CompleteMFALogin trades the challenge from Login plus a TOTP or
	// recovery code for a session. Wrong codes count toward the rate limit
	// (ErrInvalidCode, then ErrRateLimited); an unknown or expired challenge
	// is ErrUnauthenticated.
	CompleteMFALogin(ctx context.Context, challenge, code string, meta SessionMeta) (session Session, secret string, err error)
	MFAStatus(ctx context.Context, userID string) (MFAStatus, error)
	// BeginMFASetup draws a new secret (replacing an unconfirmed one);
	// ErrConflict when MFA is already on.
	BeginMFASetup(ctx context.Context, userID string) (MFASetup, error)
	// EnableMFA turns MFA on once code matches the pending secret, returns
	// ten recovery codes (shown once) and revokes every other session of the
	// user except keepSession.
	EnableMFA(ctx context.Context, userID, code, keepSession string) ([]string, error)
	// DisableMFA needs a current code or a recovery code; ErrMFAMandatory
	// for an org owner.
	DisableMFA(ctx context.Context, userID, code string) error
	// ResetMFA is for the operator (cmd/admin): MFA off without a code,
	// every session revoked, audited with actor and reason.
	ResetMFA(ctx context.Context, userID, actor, reason string) error
	Logout(ctx context.Context, sessionID string) error
	ListSessions(ctx context.Context, userID string) ([]Session, error)
	GrantRole(ctx context.Context, userID string, scope ScopeRef, role Role) (Membership, error)
	// SetRole is GrantRole done by a person: it grants role at scope, or
	// replaces the role already held on that node, and records actor and the
	// replaced role in authz_audit in the same transaction (§11.7).
	SetRole(ctx context.Context, userID string, scope ScopeRef, role Role, actor string) (Membership, error)
	// GrantRole, SetRole and RevokeRole refuse with ErrLastOwner a change
	// that would leave an organization without an org_owner.
	// RevokeRole removes a membership and records who did it (§11.7).
	RevokeRole(ctx context.Context, userID string, scope ScopeRef, actor string) error
	ListMemberships(ctx context.Context, userID string) ([]Membership, error)
	// Members lists the memberships on r's chain org → workspace → project,
	// (r may stop at the workspace or the organization),
	// ordered by email then level (org first), resuming after the given key
	// (zero key = first page); limit ≤ 0 or > 100 → 100.
	Members(ctx context.Context, r Resource, after MemberKey, limit int) ([]Member, error)
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
