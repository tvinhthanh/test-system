package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// ---- Self-serve sign-up with role approval (ADR-001, variant A "join_org") -
//
// A visitor signs up and REQUESTS a role on an existing node. No membership
// is granted until an admin of that node approves, so until then the RBAC
// union is empty and the account reaches nothing. One open request per
// (user, node). Approving grants the role through the normal path; denying
// closes it. Both are audited.
//
// The address is proved before anything trusts it (0044): a verified
// address is what Members, invitations and the first OAuth sign-in treat as
// its owner. The form answers the same for any address, like forgot
// password; the account and its request exist only once the person opens
// the single-use link mailed there and chooses a password. SIGNUP=on needs
// MAIL=smtp for that mail.

const (
	accessRequestNoteMax = 1000
	signupTTL            = 24 * time.Hour
	signupPrefix         = "atp_sgn_"
)

// SignupRequest is the sign-up form: who asks to join which node as what.
type SignupRequest struct {
	Email string
	Scope ScopeRef
	Role  Role
	Note  string
}

// normalized checks the form without a round trip: the address, the note,
// and a role that lives at the node's level (the placeholder user id is the
// trick Invite uses).
func (r SignupRequest) normalized() (SignupRequest, error) {
	e, ok := normalizeEmail(r.Email)
	if !ok {
		return r, fmt.Errorf("%w: email", ErrInvalid)
	}
	r.Email = e
	r.Note = strings.TrimSpace(r.Note)
	if len(r.Note) > accessRequestNoteMax {
		return r, fmt.Errorf("%w: note is too long", ErrInvalid)
	}
	if err := checkGrant("00000000-0000-4000-8000-000000000000", r.Scope, r.Role); err != nil {
		return r, err
	}
	return r, nil
}

// AccessRequest is one pending/decided request to join a node with a role.
type AccessRequest struct {
	ID            string
	UserID        string
	Email         string // joined from users for display
	Scope         ScopeRef
	RequestedRole Role
	Note          string
	Status        string // pending | approved | denied
	DecidedBy     string
	DecidedAt     time.Time
	CreatedAt     time.Time
}

// CountSignupRequest is the part of a sign-up the request waits for: the
// form's shape and the limit. Every request counts toward five per 15
// minutes per IP and per address, whether or not the address has an
// account — otherwise the limit itself would tell the two apart, and an
// address could be flooded with mail.
func (s *service) CountSignupRequest(ctx context.Context, req SignupRequest, ip string) error {
	r, err := req.normalized()
	if err != nil {
		return err
	}
	keys := []string{"signup-ip:" + ip, "signup-email:" + r.Email}
	for _, k := range keys {
		if locked, err := s.checkLoginLimit(ctx, k); err != nil {
			return err
		} else if locked {
			return ErrRateLimited
		}
	}
	for _, k := range keys {
		if err := s.recordLoginFailure(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// IssueSignup files the request and returns (address, token) for the
// caller to mail, or ("", "") when the address has an account — that person
// signs in instead. It differs in work between the two, so callers run it
// after the answer has gone (as IssuePasswordReset). Asking again replaces
// the open request, so only the newest link works.
func (s *service) IssueSignup(ctx context.Context, req SignupRequest) (string, string, error) {
	r, err := req.normalized()
	if err != nil {
		return "", "", err
	}
	var exists bool
	if err := s.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, r.Email).Scan(&exists); err != nil {
		return "", "", err
	}
	if exists {
		return "", "", nil
	}
	token := signupPrefix + randomToken()
	now := s.now().UTC()
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		// Expired requests go on the way past rather than in a job: the
		// limit above bounds how many there can be.
		if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM signup_requests WHERE expires_at <= $1`, now); err != nil {
			return err
		}
		_, err := s.db.Q(ctx).Exec(ctx, `
			INSERT INTO signup_requests (email, scope_level, scope_id, requested_role, note, token_hash, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (email) DO UPDATE SET scope_level = EXCLUDED.scope_level, scope_id = EXCLUDED.scope_id,
			    requested_role = EXCLUDED.requested_role, note = EXCLUDED.note, token_hash = EXCLUDED.token_hash,
			    created_at = EXCLUDED.created_at, expires_at = EXCLUDED.expires_at`,
			r.Email, string(r.Scope.Level), r.Scope.ID, string(r.Role), r.Note, hashSecret(s.pepper, token), now, now.Add(signupTTL))
		return mapErr(err)
	})
	if err != nil {
		return "", "", err
	}
	return r.Email, token, nil
}

// CompleteSignup spends a sign-up link: the account is created verified
// (the mail proved the address) with the password chosen now, the access
// request goes to the node's admins, and a session opens — one that reaches
// nothing until the request is approved. ErrNotFound for an unknown, used,
// replaced or expired link (counted toward the per-IP limit);
// ErrAccountExists when the address got an account since (sign in with it).
func (s *service) CompleteSignup(ctx context.Context, token, password string, meta SessionMeta) (Session, string, error) {
	var userID string
	err := s.guardInvitation(ctx, meta.IP, func() error {
		if len(token) > 100 || !strings.HasPrefix(token, signupPrefix) {
			return ErrNotFound
		}
		// argon2 only after the rate limit (see AcceptInvitation).
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			now := s.now().UTC()
			var r SignupRequest
			err := s.db.Q(ctx).QueryRow(ctx, `
				DELETE FROM signup_requests WHERE token_hash = $1 AND expires_at > $2
				RETURNING email, scope_level, scope_id, requested_role, note`,
				hashSecret(s.pepper, token), now).Scan(&r.Email, &r.Scope.Level, &r.Scope.ID, &r.Role, &r.Note)
			if errors.Is(err, pg.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			// A link never takes an account over; rolling back keeps it, so
			// trying again says the same.
			var exists bool
			if err := s.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, r.Email).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrAccountExists
			}
			if err := s.db.Q(ctx).QueryRow(ctx, `
				INSERT INTO users (email, password_hash, password_updated_at, email_verified_at)
				VALUES ($1, $2, $3, $3) RETURNING id`, r.Email, hash, now).Scan(&userID); err != nil {
				return mapErr(err)
			}
			var reqID string
			if err := s.db.Q(ctx).QueryRow(ctx, `
				INSERT INTO access_requests (user_id, scope_level, scope_id, requested_role, note)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				userID, string(r.Scope.Level), r.Scope.ID, string(r.Role), r.Note).Scan(&reqID); err != nil {
				return mapErr(err)
			}
			return s.audit(ctx, "access_request.create", "access_request", reqID, string(r.Scope.Level), r.Scope.ID, r.Email,
				map[string]any{"user_id": userID, "requested_role": r.Role})
		})
	})
	if err != nil {
		return Session{}, "", err
	}
	return s.openSession(ctx, userID, meta)
}

const accessRequestColumns = `ar.id, ar.user_id, u.email, ar.scope_level, ar.scope_id, ar.requested_role, ar.note, ar.status, ar.decided_by, ar.decided_at, ar.created_at`

func scanAccessRequest(row interface{ Scan(dest ...any) error }) (AccessRequest, error) {
	var a AccessRequest
	var decidedAt *time.Time
	err := row.Scan(&a.ID, &a.UserID, &a.Email, &a.Scope.Level, &a.Scope.ID, &a.RequestedRole, &a.Note, &a.Status, &a.DecidedBy, &decidedAt, &a.CreatedAt)
	if decidedAt != nil {
		a.DecidedAt = decidedAt.UTC()
	}
	a.CreatedAt = a.CreatedAt.UTC()
	return a, err
}

// AccessRequests lists the pending requests on a node, newest first.
func (s *service) AccessRequests(ctx context.Context, scope ScopeRef) ([]AccessRequest, error) {
	if !membershipLevels[scope.Level] || !isUUID(scope.ID) {
		return nil, fmt.Errorf("%w: scope", ErrInvalid)
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+accessRequestColumns+` FROM access_requests ar JOIN users u ON u.id = ar.user_id
		 WHERE ar.scope_level = $1 AND ar.scope_id = $2 AND ar.status = 'pending'
		 ORDER BY ar.created_at DESC LIMIT 200`, string(scope.Level), scope.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessRequest{}
	for rows.Next() {
		a, err := scanAccessRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DecideAccessRequest approves (granting a role — the requested one, or the
// override in role) or denies a pending request on the given node. The HTTP
// layer must have authorized member.manage on that scope. decision is
// "approved" or "denied"; role is optional and only used when approving.
func (s *service) DecideAccessRequest(ctx context.Context, scope ScopeRef, id, decision string, role Role, actor string) error {
	if !isUUID(id) || !isUUID(scope.ID) || !membershipLevels[scope.Level] {
		return ErrNotFound
	}
	if decision != "approved" && decision != "denied" {
		return fmt.Errorf("%w: decision must be approved or denied", ErrInvalid)
	}
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("%w: actor is required to decide a request", ErrInvalid)
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		var userID, requested string
		err := s.db.Q(ctx).QueryRow(ctx, `
			SELECT user_id, requested_role FROM access_requests
			 WHERE id = $1 AND scope_level = $2 AND scope_id = $3 AND status = 'pending'
			 FOR UPDATE`, id, string(scope.Level), scope.ID).Scan(&userID, &requested)
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		granted := Role(requested)
		if decision == "approved" {
			if role != "" {
				granted = role
			}
			if _, _, err := s.grant(ctx, userID, scope, granted); err != nil {
				return err
			}
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `
			UPDATE access_requests SET status = $2, decided_by = $3, decided_at = $4 WHERE id = $1`,
			id, decision, strings.TrimSpace(actor), s.now().UTC()); err != nil {
			return err
		}
		action := "access_request.deny"
		details := map[string]any{"user_id": userID, "requested_role": requested}
		if decision == "approved" {
			action = "access_request.approve"
			details["granted_role"] = granted
		}
		return s.audit(ctx, action, "access_request", id, string(scope.Level), scope.ID, actor, details)
	})
}
