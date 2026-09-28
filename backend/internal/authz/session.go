package authz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// Session lifetimes [ASSUMPTION]: a login lasts 30 days, and last_seen_at
// is touched at most every 5 minutes like API tokens.
const (
	sessionTTL      = 30 * 24 * time.Hour
	sessionPrefix   = "atp_sess_"
	sessionHexLen   = 64
	sessionTouch    = 5 * time.Minute
	loginWindow     = 15 * time.Minute
	loginMaxFailure = 5
	loginBackoff    = 15 * time.Minute // first lock; doubles per further window of failures
	loginMaxBackoff = 24 * time.Hour
)

// ---- Users ----------------------------------------------------------------

const userColumns = `id, email, email_verified_at, password_hash IS NOT NULL, created_at`

func scanUser(row interface{ Scan(dest ...any) error }) (User, error) {
	var u User
	var verified *time.Time
	if err := row.Scan(&u.ID, &u.Email, &verified, &u.HasPassword, &u.CreatedAt); err != nil {
		return User{}, err
	}
	if verified != nil {
		u.EmailVerifiedAt = verified.UTC()
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, nil
}

func (s *service) GetUser(ctx context.Context, id string) (User, error) {
	if !isUUID(id) {
		return User{}, ErrNotFound
	}
	u, err := scanUser(s.db.Q(ctx).QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	return u, mapErr(err)
}

func (s *service) VerifyEmail(ctx context.Context, userID string) error {
	if !isUUID(userID) {
		return ErrNotFound
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `UPDATE users SET email_verified_at = coalesce(email_verified_at, $2) WHERE id = $1`, userID, s.now().UTC())
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *service) SetPassword(ctx context.Context, userID, password, keepSession string) error {
	if !isUUID(userID) {
		return ErrNotFound
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		tag, err := q.Exec(ctx, `UPDATE users SET password_hash = $2, password_updated_at = $3 WHERE id = $1`, userID, hash, now)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		var keep *string
		if keepSession != "" && isUUID(keepSession) {
			keep = &keepSession
		}
		_, err = q.Exec(ctx, `UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL AND ($3::uuid IS NULL OR id <> $3)`, userID, now, keep)
		return mapErr(err)
	})
}

// ---- Login rate limit (§11.2) ---------------------------------------------

// checkLoginLimit reports whether attempts under key are currently locked.
func (s *service) checkLoginLimit(ctx context.Context, key string) (bool, error) {
	var locked *time.Time
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT locked_to FROM login_failures WHERE key = $1`, key).Scan(&locked)
	if errors.Is(err, pg.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return locked != nil && locked.After(s.now()), nil
}

// recordLoginFailure counts one failure in the 15-minute window and locks
// the key with doubling backoff once the window holds 5.
func (s *service) recordLoginFailure(ctx context.Context, key string) error {
	now := s.now().UTC()
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO login_failures (key, failures, window_at) VALUES ($1, 1, $2)
		ON CONFLICT (key) DO UPDATE SET
			failures  = CASE WHEN login_failures.window_at < $2 - $3::interval THEN 1 ELSE login_failures.failures + 1 END,
			window_at = CASE WHEN login_failures.window_at < $2 - $3::interval THEN $2 ELSE login_failures.window_at END,
			locked_to = CASE
				WHEN (CASE WHEN login_failures.window_at < $2 - $3::interval THEN 1 ELSE login_failures.failures + 1 END) >= $4
				THEN $2 + least($5::interval * power(2, greatest(0, (CASE WHEN login_failures.window_at < $2 - $3::interval THEN 1 ELSE login_failures.failures + 1 END) - $4)), $6::interval)
				ELSE login_failures.locked_to END`,
		key, now, fmt.Sprintf("%d seconds", int(loginWindow.Seconds())), loginMaxFailure,
		fmt.Sprintf("%d seconds", int(loginBackoff.Seconds())), fmt.Sprintf("%d seconds", int(loginMaxBackoff.Seconds())))
	return err
}

func (s *service) clearLoginFailures(ctx context.Context, keys ...string) error {
	_, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM login_failures WHERE key = ANY($1)`, keys)
	return err
}

// ---- Login / sessions -----------------------------------------------------

func newSessionSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("authz: crypto/rand: " + err.Error())
	}
	return sessionPrefix + hex.EncodeToString(b[:])
}

func (s *service) Login(ctx context.Context, email, password string, meta SessionMeta) (Session, string, error) {
	e, ok := normalizeEmail(email)
	if !ok {
		return Session{}, "", ErrUnauthenticated
	}
	ipKey, emailKey := "ip:"+meta.IP, "email:"+e
	for _, k := range []string{ipKey, emailKey} {
		locked, err := s.checkLoginLimit(ctx, k)
		if err != nil {
			return Session{}, "", err
		}
		if locked {
			return Session{}, "", ErrRateLimited
		}
	}

	var userID string
	var hash *string
	var verified *time.Time
	var mfa bool
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT id, password_hash, email_verified_at, totp_enabled_at IS NOT NULL FROM users WHERE email = $1`, e).Scan(&userID, &hash, &verified, &mfa)
	if err != nil && !errors.Is(err, pg.ErrNoRows) {
		return Session{}, "", err
	}
	stored := dummyHash // unknown account: still burn the argon2 time
	if hash != nil {
		stored = *hash
	}
	ok = verifyPassword(stored, password) && err == nil && hash != nil && verified != nil
	if !ok {
		for _, k := range []string{ipKey, emailKey} {
			if err := s.recordLoginFailure(ctx, k); err != nil {
				return Session{}, "", err
			}
		}
		return Session{}, "", ErrUnauthenticated
	}
	if err := s.clearLoginFailures(ctx, ipKey, emailKey); err != nil {
		return Session{}, "", err
	}
	// The password alone opens no session on an account with a second factor.
	if mfa {
		ch, err := s.openChallenge(ctx, userID, meta)
		if err != nil {
			return Session{}, "", err
		}
		return Session{}, "", ch
	}
	return s.openSession(ctx, userID, meta)
}

func (s *service) openSession(ctx context.Context, userID string, meta SessionMeta) (Session, string, error) {
	secret := newSessionSecret()
	now := s.now().UTC()
	var sess Session
	err := scanSession(s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO sessions (user_id, token_hash, created_at, last_seen_at, expires_at, ip, user_agent)
		VALUES ($1, $2, $3, $3, $4, $5, $6)
		RETURNING `+sessionColumns,
		userID, hashSecret(s.pepper, secret), now, now.Add(sessionTTL), nullable(meta.IP), nullable(truncate(meta.UserAgent, 300))), &sess)
	if err != nil {
		return Session{}, "", mapErr(err)
	}
	return sess, secret, nil
}

const sessionColumns = `id, user_id, created_at, last_seen_at, expires_at, revoked_at, coalesce(ip, ''), coalesce(user_agent, '')`

// scanSession reads sessionColumns, then any extra columns into extra.
func scanSession(row interface{ Scan(dest ...any) error }, out *Session, extra ...any) error {
	var revoked *time.Time
	if err := row.Scan(append([]any{&out.ID, &out.UserID, &out.CreatedAt, &out.LastSeenAt, &out.ExpiresAt, &revoked, &out.IP, &out.UserAgent}, extra...)...); err != nil {
		return err
	}
	out.CreatedAt, out.LastSeenAt, out.ExpiresAt = out.CreatedAt.UTC(), out.LastSeenAt.UTC(), out.ExpiresAt.UTC()
	if revoked != nil {
		out.RevokedAt = revoked.UTC()
	}
	return nil
}

func (s *service) AuthenticateSession(ctx context.Context, secret string) (Principal, Session, error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, sessionPrefix) || len(secret) != len(sessionPrefix)+sessionHexLen {
		return Principal{}, Session{}, ErrUnauthenticated
	}
	var sess Session
	var enrol bool
	// One query: the session, and whether its user is an org owner without
	// MFA (§11.2) — such a session reaches only the session and MFA routes.
	err := scanSession(s.db.Q(ctx).QueryRow(ctx, `
		SELECT `+sessionColumns+`,
		       NOT EXISTS (SELECT 1 FROM users u WHERE u.id = sessions.user_id AND u.totp_enabled_at IS NOT NULL)
		       AND EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = sessions.user_id AND m.role = 'org_owner')
		  FROM sessions WHERE token_hash = $1 AND revoked_at IS NULL`, hashSecret(s.pepper, secret)), &sess, &enrol)
	if err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return Principal{}, Session{}, ErrUnauthenticated
		}
		return Principal{}, Session{}, err
	}
	now := s.now().UTC()
	if !now.Before(sess.ExpiresAt) {
		return Principal{}, Session{}, ErrUnauthenticated
	}
	if now.Sub(sess.LastSeenAt) >= sessionTouch {
		_, _ = s.db.Q(ctx).Exec(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE id = $1`, sess.ID, now)
	}
	return Principal{Kind: PrincipalUser, UserID: sess.UserID, MFAEnrollmentRequired: enrol}, sess, nil
}

func (s *service) Logout(ctx context.Context, sessionID string) error {
	if !isUUID(sessionID) {
		return ErrNotFound
	}
	_, err := s.db.Q(ctx).Exec(ctx, `UPDATE sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`, sessionID, s.now().UTC())
	return mapErr(err)
}

func (s *service) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	if !isUUID(userID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := scanSession(rows, &sess); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// ---- OAuth sign-in (§11.2) -------------------------------------------------

func (s *service) LoginWithIdentity(ctx context.Context, id ExternalIdentity, meta SessionMeta) (Session, string, error) {
	if (id.Provider != "google" && id.Provider != "github") || id.Subject == "" || len(id.Subject) > 255 {
		return Session{}, "", fmt.Errorf("%w: identity", ErrInvalid)
	}
	var userID string
	var mfa bool
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		now := s.now().UTC()
		// Known subject: the email the provider reports today does not matter.
		err := s.db.Q(ctx).QueryRow(ctx, `
			UPDATE user_identities i SET last_login_at = $3
			  FROM users u
			 WHERE i.provider = $1 AND i.subject = $2 AND u.id = i.user_id AND u.email_verified_at IS NOT NULL
			RETURNING i.user_id, u.totp_enabled_at IS NOT NULL`, id.Provider, id.Subject, now).Scan(&userID, &mfa)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pg.ErrNoRows) {
			return err
		}
		// First sign-in: link by an email both sides have verified.
		e, ok := normalizeEmail(id.Email)
		if !id.EmailVerified || !ok {
			return ErrNoAccount
		}
		err = s.db.Q(ctx).QueryRow(ctx,
			`SELECT id, totp_enabled_at IS NOT NULL FROM users WHERE email = $1 AND email_verified_at IS NOT NULL`, e).Scan(&userID, &mfa)
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNoAccount
		}
		if err != nil {
			return err
		}
		var linkID string
		err = s.db.Q(ctx).QueryRow(ctx, `
			INSERT INTO user_identities (user_id, provider, subject, email, last_login_at) VALUES ($1, $2, $3, $4, $5)
			RETURNING id`, userID, id.Provider, id.Subject, e, now).Scan(&linkID)
		if err != nil {
			return mapErr(err) // unique (user, provider): another account at that provider
		}
		return s.audit(ctx, "identity.link", "user", userID, "", "", e, map[string]any{"provider": id.Provider, "subject": id.Subject, "identity_id": linkID})
	})
	if err != nil {
		return Session{}, "", err
	}
	if mfa {
		ch, err := s.openChallenge(ctx, userID, meta)
		if err != nil {
			return Session{}, "", err
		}
		return Session{}, "", ch
	}
	return s.openSession(ctx, userID, meta)
}

func (s *service) Identities(ctx context.Context, userID string) ([]LinkedIdentity, error) {
	if !isUUID(userID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, provider, email, created_at, last_login_at FROM user_identities
		 WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkedIdentity{}
	for rows.Next() {
		var li LinkedIdentity
		var last *time.Time
		if err := rows.Scan(&li.ID, &li.Provider, &li.Email, &li.CreatedAt, &last); err != nil {
			return nil, err
		}
		if last != nil {
			li.LastLoginAt = last.UTC()
		}
		li.CreatedAt = li.CreatedAt.UTC()
		out = append(out, li)
	}
	return out, rows.Err()
}

func (s *service) UnlinkIdentity(ctx context.Context, userID, identityID string) error {
	if !isUUID(userID) || !isUUID(identityID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		// Lock the user's row so two unlinks cannot both leave "one other way".
		var email string
		var hasPassword bool
		err := s.db.Q(ctx).QueryRow(ctx,
			`SELECT email, password_hash IS NOT NULL FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&email, &hasPassword)
		if err != nil {
			return mapErr(err)
		}
		var provider string
		err = s.db.Q(ctx).QueryRow(ctx,
			`DELETE FROM user_identities WHERE id = $1 AND user_id = $2 RETURNING provider`, identityID, userID).Scan(&provider)
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !hasPassword {
			var left int
			if err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM user_identities WHERE user_id = $1`, userID).Scan(&left); err != nil {
				return err
			}
			if left == 0 {
				return ErrLastSignIn // the DELETE rolls back with the transaction
			}
		}
		return s.audit(ctx, "identity.unlink", "user", userID, "", "", email, map[string]any{"provider": provider, "identity_id": identityID})
	})
}
