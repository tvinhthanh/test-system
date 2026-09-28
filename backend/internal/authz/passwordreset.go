package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// ---- Forgot password (§11.2) ----------------------------------------------
//
// "forgot-password luôn trả 'nếu email tồn tại, đã gửi'" and "rate limit
// theo IP và theo account": RequestPasswordReset answers the same for a known
// and an unknown email (the caller mails only when a token comes back), and
// every request — known or not — counts toward five per 15 minutes per email
// and per IP. A token is single use, thirty minutes, stored as an HMAC;
// using it sets the password, marks the address verified (the mail proved
// it) and revokes every session. It does not sign in: MFA, if on, is still
// asked at the next sign-in.

const (
	resetTTL    = 30 * time.Minute
	resetPrefix = "atp_rst_"
)

// CountPasswordResetRequest is the part of "forgot password" a request waits
// for: the limit, identical work for any email. Every request counts,
// whether or not an account exists — otherwise the limit itself would tell
// the two apart.
func (s *service) CountPasswordResetRequest(ctx context.Context, email, ip string) error {
	keys := []string{"reset-ip:" + ip}
	if e, ok := normalizeEmail(email); ok {
		keys = append(keys, "reset-email:"+e)
	}
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

// IssuePasswordReset is the part that differs between a known and an
// unknown email (a lookup, a write), so it runs after the answer has gone:
// the time a request takes says nothing (28-09 review, finding 5).
func (s *service) IssuePasswordReset(ctx context.Context, email string) (string, string, error) {
	e, ok := normalizeEmail(email)
	if !ok {
		return "", "", nil
	}
	var userID string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, e).Scan(&userID)
	if errors.Is(err, pg.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	token := resetPrefix + randomToken()
	now := s.now().UTC()
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := s.db.Q(ctx).Exec(ctx,
			`UPDATE password_resets SET revoked_at = $2 WHERE user_id = $1 AND used_at IS NULL AND revoked_at IS NULL`, userID, now); err != nil {
			return err
		}
		_, err := s.db.Q(ctx).Exec(ctx,
			`INSERT INTO password_resets (user_id, token_hash, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
			userID, hashSecret(s.pepper, token), now, now.Add(resetTTL))
		return err
	})
	if err != nil {
		return "", "", err
	}
	return e, token, nil
}

func (s *service) ResetPassword(ctx context.Context, token, password, ip string) error {
	var email string
	err := s.guardInvitation(ctx, ip, func() error {
		if len(token) > 100 || len(token) <= len(resetPrefix) || token[:len(resetPrefix)] != resetPrefix {
			return ErrNotFound
		}
		// argon2 only after the rate limit (see AcceptInvitation).
		hash, err := hashPassword(password)
		if err != nil {
			return err
		}
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			now := s.now().UTC()
			var id, userID string
			err := s.db.Q(ctx).QueryRow(ctx, `
				SELECT r.id, r.user_id, u.email FROM password_resets r JOIN users u ON u.id = r.user_id
				 WHERE r.token_hash = $1 AND r.used_at IS NULL AND r.revoked_at IS NULL AND r.expires_at > $2
				   FOR UPDATE OF r`, hashSecret(s.pepper, token), now).Scan(&id, &userID, &email)
			if errors.Is(err, pg.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			for _, q := range []struct {
				sql  string
				args []any
			}{
				{`UPDATE users SET password_hash = $2, password_updated_at = $3, email_verified_at = coalesce(email_verified_at, $3) WHERE id = $1`, []any{userID, hash, now}},
				{`UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`, []any{userID, now}},
				{`UPDATE password_resets SET used_at = $2 WHERE id = $1`, []any{id, now}},
			} {
				if _, err := s.db.Q(ctx).Exec(ctx, q.sql, q.args...); err != nil {
					return err
				}
			}
			// A locked-out account is unlocked by proving the mailbox.
			if err := s.clearLoginFailures(ctx, "email:"+email); err != nil {
				return err
			}
			return s.audit(ctx, "password.reset", "user", userID, "", "", email, map[string]any{"via": "emailed link"})
		})
	})
	if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrRateLimited) {
		return fmt.Errorf("authz: reset password: %w", err)
	}
	return err
}
