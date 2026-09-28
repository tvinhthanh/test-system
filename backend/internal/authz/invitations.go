package authz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// ---- Invitations (§11.2 "mời member", §11.6 API6) --------------------------
//
// Granting a role to an email with no account, when mail is on, invites it:
// a single-use token mailed to the address (stored as an HMAC), valid seven
// days, one open invitation per (email, node). Accepting sets a password,
// creates the account verified — the link proved the address — grants the
// role and signs in. Wrong tokens count toward the per-IP login limit.

const (
	invitationTTL    = 7 * 24 * time.Hour
	invitationPrefix = "atp_inv_"
)

func (s *service) Invite(ctx context.Context, email string, scope ScopeRef, role Role, actor string) (Invitation, string, error) {
	e, ok := normalizeEmail(email)
	if !ok {
		return Invitation{}, "", fmt.Errorf("%w: email", ErrInvalid)
	}
	// The same role rules as a grant; the user id is a placeholder.
	if err := checkGrant("00000000-0000-4000-8000-000000000000", scope, role); err != nil {
		return Invitation{}, "", err
	}
	if strings.TrimSpace(actor) == "" {
		return Invitation{}, "", fmt.Errorf("%w: actor is required to invite", ErrInvalid)
	}
	token := invitationPrefix + randomToken()
	now := s.now().UTC()
	var inv Invitation
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		// Someone with an account gets the role directly, not a mail.
		var exists bool
		if err := s.db.Q(ctx).QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, e).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return ErrAccountExists
		}
		// Inviting again replaces the open invitation (a new link, a new week).
		if _, err := s.db.Q(ctx).Exec(ctx, `
			UPDATE invitations SET revoked_at = $4
			 WHERE email = $1 AND scope_level = $2 AND scope_id = $3 AND accepted_at IS NULL AND revoked_at IS NULL`,
			e, string(scope.Level), scope.ID, now); err != nil {
			return err
		}
		err := s.db.Q(ctx).QueryRow(ctx, `
			INSERT INTO invitations (email, token_hash, scope_level, scope_id, role, invited_by, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, email, scope_level, scope_id, role, invited_by, created_at, expires_at`,
			e, hashSecret(s.pepper, token), string(scope.Level), scope.ID, string(role), strings.TrimSpace(actor), now, now.Add(invitationTTL),
		).Scan(&inv.ID, &inv.Email, &inv.Scope.Level, &inv.Scope.ID, &inv.Role, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt)
		if err != nil {
			return mapErr(err)
		}
		return s.audit(ctx, "invitation.create", "invitation", inv.ID, string(scope.Level), scope.ID, actor, map[string]any{"email": e, "role": role})
	})
	if err != nil {
		return Invitation{}, "", err
	}
	return inv, token, nil
}

const invitationColumns = `id, email, scope_level, scope_id, role, invited_by, created_at, expires_at`

func scanInvitation(row interface{ Scan(dest ...any) error }) (Invitation, error) {
	var inv Invitation
	err := row.Scan(&inv.ID, &inv.Email, &inv.Scope.Level, &inv.Scope.ID, &inv.Role, &inv.InvitedBy, &inv.CreatedAt, &inv.ExpiresAt)
	inv.CreatedAt, inv.ExpiresAt = inv.CreatedAt.UTC(), inv.ExpiresAt.UTC()
	return inv, err
}

func (s *service) Invitations(ctx context.Context, scope ScopeRef) ([]Invitation, error) {
	if !membershipLevels[scope.Level] || !isUUID(scope.ID) {
		return nil, fmt.Errorf("%w: scope", ErrInvalid)
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+invitationColumns+` FROM invitations
		 WHERE scope_level = $1 AND scope_id = $2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $3
		 ORDER BY created_at DESC LIMIT 200`, string(scope.Level), scope.ID, s.now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *service) RevokeInvitation(ctx context.Context, scope ScopeRef, id, actor string) error {
	if !isUUID(id) || !isUUID(scope.ID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		var email string
		err := s.db.Q(ctx).QueryRow(ctx, `
			UPDATE invitations SET revoked_at = $4
			 WHERE id = $1 AND scope_level = $2 AND scope_id = $3 AND accepted_at IS NULL AND revoked_at IS NULL
			RETURNING email`, id, string(scope.Level), scope.ID, s.now().UTC()).Scan(&email)
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return s.audit(ctx, "invitation.revoke", "invitation", id, string(scope.Level), scope.ID, actor, map[string]any{"email": email})
	})
}

// findInvitation is a usable invitation by its token, or ErrNotFound.
func (s *service) findInvitation(ctx context.Context, token string, lock bool) (Invitation, error) {
	if !strings.HasPrefix(token, invitationPrefix) || len(token) > 100 {
		return Invitation{}, ErrNotFound
	}
	q := `SELECT ` + invitationColumns + ` FROM invitations
	       WHERE token_hash = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > $2`
	if lock {
		q += ` FOR UPDATE`
	}
	inv, err := scanInvitation(s.db.Q(ctx).QueryRow(ctx, q, hashSecret(s.pepper, token), s.now().UTC()))
	if errors.Is(err, pg.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	return inv, err
}

// guardInvitation runs fn under the per-IP login limit and counts an
// unknown token as a failure — outside fn's transaction, so it sticks.
func (s *service) guardInvitation(ctx context.Context, ip string, fn func() error) error {
	key := "ip:" + ip
	if locked, err := s.checkLoginLimit(ctx, key); err != nil {
		return err
	} else if locked {
		return ErrRateLimited
	}
	err := fn()
	if errors.Is(err, ErrNotFound) {
		if rerr := s.recordLoginFailure(ctx, key); rerr != nil {
			return rerr
		}
	}
	return err
}

func (s *service) LookupInvitation(ctx context.Context, token, ip string) (Invitation, error) {
	var inv Invitation
	err := s.guardInvitation(ctx, ip, func() error {
		var err error
		inv, err = s.findInvitation(ctx, token, false)
		return err
	})
	return inv, err
}

func (s *service) AcceptInvitation(ctx context.Context, token, password string, meta SessionMeta) (Session, string, error) {
	var userID string
	err := s.guardInvitation(ctx, meta.IP, func() error {
		// argon2 (19 MiB) only after the rate limit let the request in, so
		// this anonymous route cannot be used to burn memory; and before the
		// token is looked at, so a weak password spends nothing.
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			inv, err := s.findInvitation(ctx, token, true)
			if err != nil {
				return err
			}
			now := s.now().UTC()
			// An account made for this email in the meantime is not taken over
			// by a link: it gets the role and signs in with its own password.
			var existing string
			err = s.db.Q(ctx).QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, inv.Email).Scan(&existing)
			switch {
			case err == nil:
				if _, _, err := s.grant(ctx, existing, inv.Scope, inv.Role); err != nil {
					return err
				}
				userID = ""
			case errors.Is(err, pg.ErrNoRows):
				if err := s.db.Q(ctx).QueryRow(ctx, `
				INSERT INTO users (email, password_hash, password_updated_at, email_verified_at)
				VALUES ($1, $2, $3, $3) RETURNING id`, inv.Email, hash, now).Scan(&userID); err != nil {
					return mapErr(err)
				}
				if _, _, err := s.grant(ctx, userID, inv.Scope, inv.Role); err != nil {
					return err
				}
			default:
				return err
			}
			if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE invitations SET accepted_at = $2 WHERE id = $1`, inv.ID, now); err != nil {
				return err
			}
			return s.audit(ctx, "invitation.accept", "invitation", inv.ID, string(inv.Scope.Level), inv.Scope.ID, inv.Email, map[string]any{"role": inv.Role, "new_account": userID != ""})
		})
	})
	if err != nil {
		return Session{}, "", err
	}
	if userID == "" {
		return Session{}, "", ErrAccountExists
	}
	return s.openSession(ctx, userID, meta)
}
