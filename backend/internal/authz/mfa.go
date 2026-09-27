package authz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// ---- MFA (§11.2 "MFA TOTP cho org owner") ---------------------------------
//
// Enrolment: BeginMFASetup stores a sealed pending secret and hands the
// user the key; EnableMFA promotes it once a valid code proves the app has
// it, issues ten recovery codes and revokes the user's other sessions (an
// attacker's session opened before the second factor existed dies with it).
// Login: a correct password on an enrolled account opens no session but a
// five-minute challenge; CompleteMFALogin trades challenge + code for the
// session. Codes are rate limited per account and per IP with the login
// counters. An org owner cannot turn MFA off, and until they turn it on
// their session reaches nothing but the session and MFA routes.

const (
	challengePrefix = "atp_mfa_"
	challengeTTL    = 5 * time.Minute
)

func mfaKey(userID string) string { return "mfa:" + userID }

// guard checks the MFA counters, runs verify, and counts a failure when it
// reports one. verify returns ErrInvalidCode for a wrong code.
func (s *service) guard(ctx context.Context, keys []string, verify func() error) error {
	for _, k := range keys {
		locked, err := s.checkLoginLimit(ctx, k)
		if err != nil {
			return err
		}
		if locked {
			return ErrRateLimited
		}
	}
	err := verify()
	if errors.Is(err, ErrInvalidCode) {
		for _, k := range keys {
			if err := s.recordLoginFailure(ctx, k); err != nil {
				return err
			}
		}
		return ErrInvalidCode
	}
	if err != nil {
		return err
	}
	return s.clearLoginFailures(ctx, keys...)
}

func (s *service) MFAStatus(ctx context.Context, userID string) (MFAStatus, error) {
	if !isUUID(userID) {
		return MFAStatus{}, ErrNotFound
	}
	var st MFAStatus
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT u.totp_enabled_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM memberships m WHERE m.user_id = u.id AND m.role = 'org_owner'),
		       (SELECT count(*) FROM mfa_recovery_codes r WHERE r.user_id = u.id AND r.used_at IS NULL)
		  FROM users u WHERE u.id = $1`, userID).Scan(&st.Enabled, &st.Required, &st.RecoveryCodesLeft)
	return st, mapErr(err)
}

func (s *service) BeginMFASetup(ctx context.Context, userID string) (MFASetup, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return MFASetup{}, err
	}
	secret := newTOTPSecret()
	tag, err := s.db.Q(ctx).Exec(ctx,
		`UPDATE users SET totp_pending_secret = $2 WHERE id = $1 AND totp_enabled_at IS NULL`,
		userID, seal(s.pepper, userID, secret))
	if err != nil {
		return MFASetup{}, err
	}
	if tag.RowsAffected() == 0 {
		return MFASetup{}, fmt.Errorf("%w: MFA is already on; turn it off first", ErrConflict)
	}
	return MFASetup{Secret: b32.EncodeToString(secret), URI: otpauthURI(u.Email, secret)}, nil
}

func (s *service) EnableMFA(ctx context.Context, userID, code, keepSession string) ([]string, error) {
	if !isUUID(userID) {
		return nil, ErrNotFound
	}
	var codes []string
	err := s.guard(ctx, []string{mfaKey(userID)}, func() error {
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			var pending []byte
			var enabled *time.Time
			err := s.db.Q(ctx).QueryRow(ctx,
				`SELECT totp_pending_secret, totp_enabled_at FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&pending, &enabled)
			if err != nil {
				return mapErr(err)
			}
			if enabled != nil {
				return fmt.Errorf("%w: MFA is already on", ErrConflict)
			}
			if pending == nil {
				return fmt.Errorf("%w: start the setup first", ErrConflict)
			}
			secret, err := unseal(s.pepper, userID, pending)
			if err != nil {
				return err
			}
			step := totpMatch(secret, code, s.now(), 0)
			if step == 0 {
				return ErrInvalidCode
			}
			now := s.now().UTC()
			if _, err := s.db.Q(ctx).Exec(ctx, `
				UPDATE users SET totp_secret = totp_pending_secret, totp_pending_secret = NULL,
				       totp_enabled_at = $2, totp_last_step = $3 WHERE id = $1`, userID, now, step); err != nil {
				return err
			}
			if codes, err = s.replaceRecoveryCodes(ctx, userID); err != nil {
				return err
			}
			if _, err := s.db.Q(ctx).Exec(ctx,
				`UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL AND id::text <> $3`,
				userID, now, keepSession); err != nil {
				return err
			}
			return s.audit(ctx, "mfa.enable", "user", userID, "", "", userID, map[string]any{})
		})
	})
	return codes, err
}

func (s *service) replaceRecoveryCodes(ctx context.Context, userID string) ([]string, error) {
	if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, recoveryCodes)
	for i := range codes {
		codes[i] = newRecoveryCode()
		if _, err := s.db.Q(ctx).Exec(ctx, `INSERT INTO mfa_recovery_codes (user_id, code_hash) VALUES ($1, $2)`,
			userID, hashSecret(s.pepper, normalizeRecoveryCode(codes[i]))); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// checkSecondFactor accepts a TOTP code (advancing the replay mark) or an
// unused recovery code (spending it). It must run inside a transaction.
func (s *service) checkSecondFactor(ctx context.Context, userID, code string) error {
	var sealed []byte
	var last int64
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT totp_secret, totp_last_step FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&sealed, &last)
	if err != nil {
		return mapErr(err)
	}
	if sealed == nil {
		return fmt.Errorf("%w: MFA is off", ErrConflict)
	}
	secret, err := unseal(s.pepper, userID, sealed)
	if err != nil {
		return err
	}
	if step := totpMatch(secret, code, s.now(), last); step != 0 {
		_, err := s.db.Q(ctx).Exec(ctx, `UPDATE users SET totp_last_step = $2 WHERE id = $1`, userID, step)
		return err
	}
	var id string
	err = s.db.Q(ctx).QueryRow(ctx, `
		UPDATE mfa_recovery_codes SET used_at = $3
		 WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
		RETURNING id`, userID, hashSecret(s.pepper, normalizeRecoveryCode(code)), s.now().UTC()).Scan(&id)
	if errors.Is(err, pg.ErrNoRows) {
		return ErrInvalidCode
	}
	if err != nil {
		return err
	}
	return s.audit(ctx, "mfa.recovery_used", "user", userID, "", "", userID, map[string]any{"code_id": id})
}

func (s *service) DisableMFA(ctx context.Context, userID, code string) error {
	st, err := s.MFAStatus(ctx, userID)
	if err != nil {
		return err
	}
	if st.Required {
		return ErrMFAMandatory
	}
	return s.guard(ctx, []string{mfaKey(userID)}, func() error {
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			if err := s.checkSecondFactor(ctx, userID, code); err != nil {
				return err
			}
			if _, err := s.db.Q(ctx).Exec(ctx, `
				UPDATE users SET totp_secret = NULL, totp_pending_secret = NULL, totp_enabled_at = NULL, totp_last_step = 0
				 WHERE id = $1`, userID); err != nil {
				return err
			}
			if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id = $1`, userID); err != nil {
				return err
			}
			return s.audit(ctx, "mfa.disable", "user", userID, "", "", userID, map[string]any{})
		})
	})
}

// openChallenge is Login's second half for an enrolled account.
func (s *service) openChallenge(ctx context.Context, userID string, meta SessionMeta) (*MFARequired, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("authz: crypto/rand: " + err.Error())
	}
	secret := challengePrefix + hex.EncodeToString(b[:])
	now := s.now().UTC()
	// Expired challenges of anyone go on the way; the table stays small.
	if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM mfa_challenges WHERE expires_at < $1`, now); err != nil {
		return nil, err
	}
	exp := now.Add(challengeTTL)
	if _, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO mfa_challenges (user_id, token_hash, expires_at, ip, user_agent) VALUES ($1, $2, $3, $4, $5)`,
		userID, hashSecret(s.pepper, secret), exp, nullable(meta.IP), nullable(truncate(meta.UserAgent, 300))); err != nil {
		return nil, err
	}
	return &MFARequired{Challenge: secret, ExpiresAt: exp}, nil
}

func (s *service) CompleteMFALogin(ctx context.Context, challenge, code string, meta SessionMeta) (Session, string, error) {
	if len(challenge) != len(challengePrefix)+64 || challenge[:len(challengePrefix)] != challengePrefix {
		return Session{}, "", ErrUnauthenticated
	}
	var userID string
	err := s.db.Q(ctx).QueryRow(ctx,
		`SELECT user_id FROM mfa_challenges WHERE token_hash = $1 AND expires_at > $2`,
		hashSecret(s.pepper, challenge), s.now().UTC()).Scan(&userID)
	if errors.Is(err, pg.ErrNoRows) {
		return Session{}, "", ErrUnauthenticated
	}
	if err != nil {
		return Session{}, "", err
	}
	var sess Session
	var secret string
	err = s.guard(ctx, []string{mfaKey(userID), "ip:" + meta.IP}, func() error {
		return s.db.WithTx(ctx, func(ctx context.Context) error {
			if err := s.checkSecondFactor(ctx, userID, code); err != nil {
				return err
			}
			// One challenge, one session.
			tag, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM mfa_challenges WHERE token_hash = $1`, hashSecret(s.pepper, challenge))
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrUnauthenticated
			}
			sess, secret, err = s.openSession(ctx, userID, meta)
			return err
		})
	})
	return sess, secret, err
}
