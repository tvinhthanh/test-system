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
// Email verification (needs MAIL=smtp) is a follow-up: here the human
// approval is the gate and the account is created verified so the flow works
// without a mailer. Enabling the endpoint is gated by config SIGNUP=on.

const accessRequestNoteMax = 1000

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

// Signup creates an account and a pending access request for the given node,
// then opens a session. The account carries no role until an admin approves,
// so the new session can sign in but reach nothing. ErrAccountExists if the
// email is taken (sign in and request access from there instead).
func (s *service) Signup(ctx context.Context, email, password string, scope ScopeRef, role Role, note string, meta SessionMeta) (Session, string, error) {
	e, ok := normalizeEmail(email)
	if !ok {
		return Session{}, "", fmt.Errorf("%w: email", ErrInvalid)
	}
	if len(note) > accessRequestNoteMax {
		return Session{}, "", fmt.Errorf("%w: note is too long", ErrInvalid)
	}
	// Validate the scope+role combination before touching the database, with
	// a placeholder user id (the same trick Invite uses).
	if err := checkGrant("00000000-0000-4000-8000-000000000000", scope, role); err != nil {
		return Session{}, "", err
	}

	var userID string
	err := s.guardSignup(ctx, meta.IP, func() error {
		// argon2 only after the rate limit let the request in, and before any
		// row is read, so a weak password or a taken email spends little.
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			var exists bool
			if err := s.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, e).Scan(&exists); err != nil {
				return err
			}
			if exists {
				return ErrAccountExists
			}
			now := s.now().UTC()
			if err := s.db.Q(ctx).QueryRow(ctx, `
				INSERT INTO users (email, password_hash, password_updated_at, email_verified_at)
				VALUES ($1, $2, $3, $3) RETURNING id`, e, hash, now).Scan(&userID); err != nil {
				return mapErr(err)
			}
			var reqID string
			err := s.db.Q(ctx).QueryRow(ctx, `
				INSERT INTO access_requests (user_id, scope_level, scope_id, requested_role, note)
				VALUES ($1, $2, $3, $4, $5) RETURNING id`,
				userID, string(scope.Level), scope.ID, string(role), strings.TrimSpace(note)).Scan(&reqID)
			if err != nil {
				return mapErr(err)
			}
			return s.audit(ctx, "access_request.create", "access_request", reqID, string(scope.Level), scope.ID, e,
				map[string]any{"user_id": userID, "requested_role": role})
		})
	})
	if err != nil {
		return Session{}, "", err
	}
	return s.openSession(ctx, userID, meta)
}

// guardSignup applies the per-IP login limit to the anonymous signup route
// and counts any failure as an attempt, so mass account creation throttles.
func (s *service) guardSignup(ctx context.Context, ip string, fn func() error) error {
	key := "ip:" + ip
	if locked, err := s.checkLoginLimit(ctx, key); err != nil {
		return err
	} else if locked {
		return ErrRateLimited
	}
	err := fn()
	if err != nil && !errors.Is(err, ErrRateLimited) {
		if rerr := s.recordLoginFailure(ctx, key); rerr != nil {
			return rerr
		}
	}
	return err
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
