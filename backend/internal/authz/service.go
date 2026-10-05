package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// lastUsedThrottle bounds writes from Authenticate (§11.4: "throttle 5 min").
const lastUsedThrottle = 5 * time.Minute

// defaultTokenTTL is §11.4's one-year default expiry.
const defaultTokenTTL = 365 * 24 * time.Hour

type service struct {
	db     *pg.DB
	pepper []byte
	now    func() time.Time // swapped in tests
}

// New returns the Service. pepper is the HMAC key for token hashes
// (config.TokenPepper); rotating it invalidates every token, which is the
// point of keeping it out of the database.
func New(db *pg.DB, pepper []byte) Service {
	if len(pepper) < 16 {
		panic("authz: token pepper must be at least 16 bytes")
	}
	return &service{db: db, pepper: pepper, now: time.Now}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

// nullable maps "" to SQL NULL for optional uuid parameters.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---- Authorize -----------------------------------------------------------

func (s *service) Authorize(ctx context.Context, p Principal, r Resource, a Action) error {
	if r.OrgID == "" {
		return fmt.Errorf("%w: resource has no organization", ErrInvalid)
	}
	switch p.Kind {
	case PrincipalToken:
		return authorizeToken(p, r, a)
	case PrincipalUser:
		return s.authorizeUser(ctx, p, r, a)
	}
	return fmt.Errorf("%w: principal kind %q", ErrInvalid, p.Kind)
}

// authorizeToken needs no database: Authenticate already loaded everything.
// A token whose node is not on the resource's chain gets ErrNoAccess (404 —
// §7.2 "right token, wrong project"); one that is on the chain but lacks
// the permission gets ErrForbidden.
func authorizeToken(p Principal, r Resource, a Action) error {
	if p.TokenScope.ID == "" || r.idAt(p.TokenScope.Level) != p.TokenScope.ID {
		return ErrNoAccess
	}
	if !permissionsAllow(p.Permissions, a) {
		return ErrForbidden
	}
	return nil
}

// authorizeUser walks up the tree in one query (§11.3.3). The tenant
// condition is in the WHERE clause, not applied afterwards. Several
// memberships on the chain act as their union (§11.3.2 is not a ladder).
func (s *service) authorizeUser(ctx context.Context, p Principal, r Resource, a Action) error {
	roles, err := s.rolesOnChain(ctx, p, r)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return ErrNoAccess
	}
	if !anyAllows(roles, a) {
		return ErrForbidden
	}
	return nil
}

func (s *service) Roles(ctx context.Context, p Principal, r Resource) ([]Role, error) {
	if p.Kind != PrincipalUser {
		return nil, fmt.Errorf("%w: roles are for users", ErrInvalid)
	}
	if r.OrgID == "" {
		return nil, fmt.Errorf("%w: resource has no organization", ErrInvalid)
	}
	return s.rolesOnChain(ctx, p, r)
}

// rolesOnChain is the one membership query behind Authorize and Roles.
func (s *service) rolesOnChain(ctx context.Context, p Principal, r Resource) ([]Role, error) {
	if !isUUID(p.UserID) {
		return nil, nil
	}
	if !isUUID(r.OrgID) || (r.WorkspaceID != "" && !isUUID(r.WorkspaceID)) || (r.ProjectID != "" && !isUUID(r.ProjectID)) {
		return nil, fmt.Errorf("%w: resource ids", ErrInvalid)
	}
	// Absent levels are passed as NULL: `scope_id = NULL` is never true, so
	// an org-level action simply does not match workspace or project rows.
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT role FROM memberships
		 WHERE user_id = $1
		   AND ((scope_level = 'organization' AND scope_id = $2)
		     OR (scope_level = 'workspace'    AND scope_id = $3)
		     OR (scope_level = 'project'      AND scope_id = $4))`,
		p.UserID, r.OrgID, nullable(r.WorkspaceID), nullable(r.ProjectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []Role
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, Role(role))
	}
	return roles, rows.Err()
}

// ---- Tokens --------------------------------------------------------------

func (s *service) Authenticate(ctx context.Context, bearer string) (Principal, error) {
	prefix, secret, ok := splitBearer(strings.TrimSpace(bearer))
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	want := hashSecret(s.pepper, secret)

	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, token_hash, scope_level, scope_id, permissions, last_used_at, expires_at, revoked_at
		  FROM api_tokens WHERE prefix = $1`, prefix)
	if err != nil {
		return Principal{}, err
	}
	defer rows.Close()

	type candidate struct {
		id, level, scopeID string
		hash               []byte
		perms              []string
		lastUsed, revoked  *time.Time
		expires            time.Time
	}
	var match *candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.hash, &c.level, &c.scopeID, &c.perms, &c.lastUsed, &c.expires, &c.revoked); err != nil {
			return Principal{}, err
		}
		// Compare every candidate; do not break early. Prefix collisions
		// are rare and the loop is short, so there is no timing to leak.
		if hashEqual(c.hash, want) && match == nil {
			cc := c
			match = &cc
		}
	}
	if err := rows.Err(); err != nil {
		return Principal{}, err
	}
	rows.Close()

	now := s.now()
	if match == nil || match.revoked != nil || !now.Before(match.expires) {
		return Principal{}, ErrUnauthenticated
	}

	if match.lastUsed == nil || now.Sub(*match.lastUsed) >= lastUsedThrottle {
		// Best effort: a lost touch costs nothing but a stale "last used"
		// column, and the next request past the throttle retries it.
		_, _ = s.db.Q(ctx).Exec(ctx, `UPDATE api_tokens SET last_used_at = $2 WHERE id = $1`, match.id, now)
	}

	perms := make([]Permission, len(match.perms))
	for i, p := range match.perms {
		perms[i] = Permission(p)
	}
	return Principal{
		Kind:        PrincipalToken,
		TokenID:     match.id,
		TokenScope:  ScopeRef{Level: Level(match.level), ID: match.scopeID},
		Permissions: perms,
	}, nil
}

func (s *service) IssueToken(ctx context.Context, spec TokenSpec) (Issued, error) {
	if !tokenLevels[spec.Scope.Level] || !isUUID(spec.Scope.ID) {
		return Issued{}, fmt.Errorf("%w: token scope", ErrInvalid)
	}
	if len(spec.Permissions) == 0 {
		return Issued{}, fmt.Errorf("%w: token needs at least one permission", ErrInvalid)
	}
	perms := make([]string, 0, len(spec.Permissions))
	for _, p := range spec.Permissions {
		if !validPermissions[p] {
			return Issued{}, fmt.Errorf("%w: permission %q", ErrInvalid, p)
		}
		perms = append(perms, string(p))
	}
	if l := len(spec.Name); l < 1 || l > 200 {
		return Issued{}, fmt.Errorf("%w: token name", ErrInvalid)
	}
	var createdBy *string
	if spec.CreatedBy != "" {
		if !isUUID(spec.CreatedBy) {
			return Issued{}, ErrNotFound
		}
		createdBy = &spec.CreatedBy
	}
	now := s.now()
	expires := spec.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(defaultTokenTTL)
	}
	if !expires.After(now) {
		return Issued{}, fmt.Errorf("%w: expiry is in the past", ErrInvalid)
	}

	secret := newSecret()
	var t Token
	var cb *string
	err := s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO api_tokens (prefix, token_hash, scope_level, scope_id, permissions, name, created_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, prefix, scope_level, scope_id, name, created_by, created_at, expires_at`,
		secret[:prefixLen], hashSecret(s.pepper, secret), string(spec.Scope.Level), spec.Scope.ID, perms, spec.Name, createdBy, expires,
	).Scan(&t.ID, &t.Prefix, &t.Scope.Level, &t.Scope.ID, &t.Name, &cb, &t.CreatedAt, &t.ExpiresAt)
	if err != nil {
		return Issued{}, mapErr(err)
	}
	if cb != nil {
		t.CreatedBy = *cb
	}
	t.Permissions = append([]Permission(nil), spec.Permissions...)
	return Issued{Token: t, Plaintext: TokenPrefix + secret}, nil
}

func (s *service) RevokeToken(ctx context.Context, id, actor string) error {
	if !isUUID(id) {
		return ErrNotFound
	}
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("%w: actor is required to revoke a token", ErrInvalid)
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var level, scopeID, name string
		err := q.QueryRow(ctx, `
			UPDATE api_tokens SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL
			RETURNING scope_level, scope_id, name`, id, s.now()).Scan(&level, &scopeID, &name)
		if errors.Is(err, pg.ErrNoRows) {
			// Zero rows: either unknown or already revoked. Revoking twice is
			// fine and leaves no second audit row — nothing changed.
			var exists bool
			if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_tokens WHERE id = $1)`, id).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return ErrNotFound
			}
			return nil
		}
		if err != nil {
			return err
		}
		return s.audit(ctx, "token.revoke", "api_token", id, level, scopeID, actor, map[string]any{"name": name})
	})
}

// audit appends the record of a destructive change in the caller's
// transaction: the change and its record commit together or not at all.
func (s *service) audit(ctx context.Context, action, subjectKind, subjectID, level, scopeID, actor string, details map[string]any) error {
	b, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.db.Q(ctx).Exec(ctx, `
		INSERT INTO authz_audit (action, subject_kind, subject_id, scope_level, scope_id, actor, details, at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		action, subjectKind, subjectID, nullable(level), nullable(scopeID), strings.TrimSpace(actor), b, s.now().UTC())
	return err
}

// ListAudit returns the newest destructive actions, optionally only those
// on one scope (a project, workspace or organization id); limit ≤ 0 → 100.
func (s *service) ListAudit(ctx context.Context, scopeID string, limit int) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	var scope *string
	if scopeID != "" {
		if !isUUID(scopeID) {
			return nil, fmt.Errorf("%w: audit scope", ErrInvalid)
		}
		scope = &scopeID
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, action, subject_kind, subject_id, coalesce(scope_level, ''), scope_id, actor, details, at
		  FROM authz_audit WHERE $1::uuid IS NULL OR scope_id = $1
		 ORDER BY id DESC LIMIT $2`, scope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var level string
		var sid *string
		var details []byte
		if err := rows.Scan(&e.ID, &e.Action, &e.SubjectKind, &e.SubjectID, &level, &sid, &e.Actor, &details, &e.At); err != nil {
			return nil, err
		}
		e.Scope.Level = Level(level)
		if sid != nil {
			e.Scope.ID = *sid
		}
		_ = json.Unmarshal(details, &e.Details)
		e.At = e.At.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *service) ListTokens(ctx context.Context, scope ScopeRef) ([]Token, error) {
	if !tokenLevels[scope.Level] || !isUUID(scope.ID) {
		return nil, fmt.Errorf("%w: token scope", ErrInvalid)
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, prefix, scope_level, scope_id, permissions, name, created_by, created_at, last_used_at, expires_at, revoked_at
		  FROM api_tokens WHERE scope_level = $1 AND scope_id = $2 ORDER BY created_at`, string(scope.Level), scope.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var t Token
		var perms []string
		var cb *string
		var lastUsed, revoked *time.Time
		if err := rows.Scan(&t.ID, &t.Prefix, &t.Scope.Level, &t.Scope.ID, &perms, &t.Name, &cb, &t.CreatedAt, &lastUsed, &t.ExpiresAt, &revoked); err != nil {
			return nil, err
		}
		for _, p := range perms {
			t.Permissions = append(t.Permissions, Permission(p))
		}
		if cb != nil {
			t.CreatedBy = *cb
		}
		if lastUsed != nil {
			t.LastUsedAt = *lastUsed
		}
		if revoked != nil {
			t.RevokedAt = *revoked
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---- Users and memberships ----------------------------------------------

func normalizeEmail(email string) (string, bool) {
	e := strings.ToLower(strings.TrimSpace(email))
	at := strings.IndexByte(e, '@')
	if at < 1 || at == len(e)-1 || len(e) > 254 || strings.ContainsAny(e, " \t\r\n") {
		return "", false
	}
	return e, true
}

func (s *service) CreateUser(ctx context.Context, email string) (User, error) {
	e, ok := normalizeEmail(email)
	if !ok {
		return User{}, fmt.Errorf("%w: email", ErrInvalid)
	}
	u, err := scanUser(s.db.Q(ctx).QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING `+userColumns, e))
	return u, mapErr(err)
}

func (s *service) GetUserByEmail(ctx context.Context, email string) (User, error) {
	e, ok := normalizeEmail(email)
	if !ok {
		return User{}, ErrNotFound
	}
	u, err := scanUser(s.db.Q(ctx).QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, e))
	return u, mapErr(err)
}

func (s *service) GrantRole(ctx context.Context, userID string, scope ScopeRef, role Role) (Membership, error) {
	if err := checkGrant(userID, scope, role); err != nil {
		return Membership{}, err
	}
	var m Membership
	// A transaction, so the last-owner check and the upsert see one state.
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		var err error
		m, _, err = s.grant(ctx, userID, scope, role)
		return err
	})
	return m, err
}

// keepAnOwner refuses a change that removes userID's org_owner role at org
// when it is the last one (§11.3.1: the owner holds billing and deleting the
// org — an org without one cannot be recovered from the UI). The owner rows
// are locked, so two concurrent demotions cannot both pass.
func (s *service) keepAnOwner(ctx context.Context, org, userID string) error {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT user_id FROM memberships
		 WHERE scope_level = 'organization' AND scope_id = $1 AND role = 'org_owner'
		   FOR UPDATE`, org)
	if err != nil {
		return err
	}
	defer rows.Close()
	var owners []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		owners = append(owners, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(owners) == 1 && owners[0] == userID {
		return ErrLastOwner
	}
	return nil
}

func (s *service) SetRole(ctx context.Context, userID string, scope ScopeRef, role Role, actor string) (Membership, error) {
	if strings.TrimSpace(actor) == "" {
		return Membership{}, fmt.Errorf("%w: actor is required to grant a role", ErrInvalid)
	}
	var m Membership
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		var previous string
		var err error
		if m, previous, err = s.grant(ctx, userID, scope, role); err != nil {
			return err
		}
		return s.audit(ctx, "role.grant", "membership", m.ID, string(scope.Level), scope.ID, actor, map[string]any{"user_id": userID, "role": role, "previous": previous})
	})
	return m, err
}

// grant upserts the one row per (user, node) and returns the role it
// replaced ("" for a new row).
// checkGrant is the input validation of a grant, before any round trip.
func checkGrant(userID string, scope ScopeRef, role Role) error {
	if !membershipLevels[scope.Level] || !isUUID(scope.ID) {
		return fmt.Errorf("%w: membership scope", ErrInvalid)
	}
	level, ok := RoleLevel[role]
	if !ok {
		return fmt.Errorf("%w: role %q", ErrInvalid, role)
	}
	// §11.3.1 "Gán ở tầng": a role lives at one level and inherits down.
	if level != scope.Level {
		return fmt.Errorf("%w: role %s is granted at the %s level, not %s", ErrInvalid, role, level, scope.Level)
	}
	if !isUUID(userID) {
		return ErrNotFound
	}
	return nil
}

func (s *service) grant(ctx context.Context, userID string, scope ScopeRef, role Role) (Membership, string, error) {
	if err := checkGrant(userID, scope, role); err != nil {
		return Membership{}, "", err
	}
	if scope.Level == LevelOrganization && role != RoleOrgOwner {
		if err := s.keepAnOwner(ctx, scope.ID, userID); err != nil {
			return Membership{}, "", err
		}
	}
	var m Membership
	var previous *string
	// Granting again changes the role: one row per (user, node). The CTE
	// reads the row as it was before this statement.
	err := s.db.Q(ctx).QueryRow(ctx, `
		WITH prev AS (
			SELECT role FROM memberships WHERE user_id = $1 AND scope_level = $2 AND scope_id = $3
		)
		INSERT INTO memberships (user_id, scope_level, scope_id, role) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, scope_level, scope_id) DO UPDATE SET role = EXCLUDED.role
		RETURNING id, user_id, scope_level, scope_id, role, created_at, (SELECT role FROM prev)`,
		userID, string(scope.Level), scope.ID, string(role),
	).Scan(&m.ID, &m.UserID, &m.Scope.Level, &m.Scope.ID, &m.Role, &m.CreatedAt, &previous)
	if previous == nil {
		return m, "", mapErr(err)
	}
	return m, *previous, mapErr(err)
}

func (s *service) Members(ctx context.Context, r Resource, after MemberKey, limit int) ([]Member, error) {
	// An organization, a workspace or a project: the chain stops at the
	// deepest id given (scope_id = NULL matches nothing).
	if !isUUID(r.OrgID) || (r.WorkspaceID != "" && !isUUID(r.WorkspaceID)) || (r.ProjectID != "" && !isUUID(r.ProjectID)) {
		return nil, fmt.Errorf("%w: resource", ErrInvalid)
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rank := map[Level]int{LevelOrganization: 0, LevelWorkspace: 1, LevelProject: 2}
	afterRank, ok := rank[after.Level]
	if after.Email != "" && !ok {
		return nil, fmt.Errorf("%w: member cursor", ErrInvalid)
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT user_id, email, role, scope_level, scope_id, created_at FROM (
			SELECT m.user_id, u.email, m.role, m.scope_level, m.scope_id, m.created_at,
			       CASE m.scope_level WHEN 'organization' THEN 0 WHEN 'workspace' THEN 1 ELSE 2 END AS rank
			  FROM memberships m JOIN users u ON u.id = m.user_id
			 WHERE (m.scope_level = 'organization' AND m.scope_id = $1)
			    OR (m.scope_level = 'workspace'    AND m.scope_id = $2)
			    OR (m.scope_level = 'project'      AND m.scope_id = $3)
		) chain
		 WHERE $4 = '' OR (email, rank) > ($4, $5)
		 ORDER BY email, rank
		 LIMIT $6`,
		r.OrgID, nullable(r.WorkspaceID), nullable(r.ProjectID), after.Email, afterRank, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.Role, &m.Scope.Level, &m.Scope.ID, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *service) RevokeRole(ctx context.Context, userID string, scope ScopeRef, actor string) error {
	if !membershipLevels[scope.Level] || !isUUID(scope.ID) {
		return fmt.Errorf("%w: membership scope", ErrInvalid)
	}
	if !isUUID(userID) {
		return ErrNotFound
	}
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("%w: actor is required to revoke a role", ErrInvalid)
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		if scope.Level == LevelOrganization {
			if err := s.keepAnOwner(ctx, scope.ID, userID); err != nil {
				return err
			}
		}
		var id, role string
		err := s.db.Q(ctx).QueryRow(ctx,
			`DELETE FROM memberships WHERE user_id = $1 AND scope_level = $2 AND scope_id = $3 RETURNING id, role`,
			userID, string(scope.Level), scope.ID).Scan(&id, &role)
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return s.audit(ctx, "role.revoke", "membership", id, string(scope.Level), scope.ID, actor, map[string]any{"user_id": userID, "role": role})
	})
}

func (s *service) ListMemberships(ctx context.Context, userID string) ([]Membership, error) {
	if !isUUID(userID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, user_id, scope_level, scope_id, role, created_at
		  FROM memberships WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.ID, &m.UserID, &m.Scope.Level, &m.Scope.ID, &m.Role, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

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

func (s *service) ExpiringTokens(ctx context.Context, within time.Duration) (int, error) {
	now := s.now().UTC()
	var n int
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT count(*) FROM api_tokens
		 WHERE revoked_at IS NULL AND expires_at > $1 AND expires_at <= $2`, now, now.Add(within)).Scan(&n)
	return n, err
}

func (s *service) PurgeProject(ctx context.Context, projectID string, environmentIDs []string) error {
	if !isUUID(projectID) {
		return ErrNotFound
	}
	scopes := append([]string{projectID}, environmentIDs...)
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, q := range []string{
			`DELETE FROM api_tokens WHERE scope_id::text = ANY($1)`,
			`DELETE FROM memberships WHERE scope_level = 'project' AND scope_id::text = ANY($1)`,
			`DELETE FROM invitations WHERE scope_level = 'project' AND scope_id::text = ANY($1)`,
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, scopes); err != nil {
				return fmt.Errorf("authz: purge project: %w", err)
			}
		}
		return nil
	})
}

func (s *service) AuditProject(ctx context.Context, action, projectID, workspaceID, actor string, details map[string]any) error {
	switch action {
	case "project.delete", "project.restore", "project.purge", "project.configure":
	default:
		return fmt.Errorf("%w: audit action %q", ErrInvalid, action)
	}
	if !isUUID(projectID) || !isUUID(workspaceID) || actor == "" {
		return fmt.Errorf("%w: project, workspace and actor", ErrInvalid)
	}
	return s.audit(ctx, action, "project", projectID, string(LevelWorkspace), workspaceID, actor, details)
}

func (s *service) AuditRun(ctx context.Context, action, runID, projectID, actor string, details map[string]any) error {
	switch action {
	case "run.delete", "run.restore", "run.purge":
	default:
		return fmt.Errorf("%w: audit action %q", ErrInvalid, action)
	}
	if !isUUID(runID) || !isUUID(projectID) || actor == "" {
		return fmt.Errorf("%w: run, project and actor", ErrInvalid)
	}
	return s.audit(ctx, action, "run", runID, string(LevelProject), projectID, actor, details)
}
