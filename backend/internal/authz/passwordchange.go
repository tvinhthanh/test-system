package authz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// ---- Change password (§11.2) ----------------------------------------------
//
// "Đổi password → revoke mọi session khác": the signed-in person proves the
// current password, the same way a login does and under the same limit —
// a stolen session must not be able to take the account over by guessing —
// then every other session of the account ends. MFA stays as it was.

func (s *service) ChangePassword(ctx context.Context, userID, keepSession, current, next, ip string) error {
	if !isUUID(userID) {
		return ErrNotFound
	}
	// Per account and per address, like the login (the account key is the
	// user id, not the email, so a changed email cannot reset the count).
	keys := []string{"pwchange:" + userID, "ip:" + ip}
	for _, k := range keys {
		if locked, err := s.checkLoginLimit(ctx, k); err != nil {
			return err
		} else if locked {
			return ErrRateLimited
		}
	}
	var hash *string
	var email string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT password_hash, email FROM users WHERE id = $1`, userID).Scan(&hash, &email)
	if errors.Is(err, pg.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if hash == nil {
		return ErrNoPassword
	}
	if !verifyPassword(*hash, current) {
		for _, k := range keys {
			if err := s.recordLoginFailure(ctx, k); err != nil {
				return err
			}
		}
		return ErrUnauthenticated
	}
	if next == current {
		return ErrSamePassword
	}
	newHash, err := hashPassword(next)
	if err != nil {
		return err // ErrInvalid: too short
	}
	now := s.now().UTC()
	var keep *string
	if isUUID(keepSession) {
		keep = &keepSession
	}
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		if _, err := q.Exec(ctx, `UPDATE users SET password_hash = $2, password_updated_at = $3 WHERE id = $1`, userID, newHash, now); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `UPDATE sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL AND ($3::uuid IS NULL OR id <> $3)`, userID, now, keep)
		if err != nil {
			return err
		}
		if err := s.clearLoginFailures(ctx, keys[0], "email:"+email); err != nil {
			return err
		}
		return s.audit(ctx, "password.change", "user", userID, "", "", email, map[string]any{"sessions_revoked": tag.RowsAffected()})
	})
	if err != nil {
		return fmt.Errorf("authz: change password: %w", err)
	}
	return nil
}

// POST /v1/session/password {current, password} → 204; the session that
// asked stays signed in, every other one is signed out.
func (h *webHandlers) passwordChange(w http.ResponseWriter, r *http.Request) {
	p, ok := sessionUserFor(w, r)
	if !ok {
		return
	}
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "want {current, password}", nil)
		return
	}
	switch err := h.svc.ChangePassword(r.Context(), p.UserID, p.SessionID, req.Current, req.Password, clientIP(r)); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, ErrUnauthenticated):
		// Not 401: the session is fine, the password typed is not.
		httpx.WriteError(w, http.StatusBadRequest, "WRONG_PASSWORD", "the current password is not right", map[string]any{"field": "current"})
	case errors.Is(err, ErrSamePassword):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PASSWORD", "choose a password different from the current one", map[string]any{"field": "password"})
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PASSWORD", "choose a password of at least 12 characters", map[string]any{"field": "password"})
	case errors.Is(err, ErrNoPassword):
		httpx.WriteError(w, http.StatusConflict, "NO_PASSWORD", "this account signs in with Google or GitHub; set a first password with \"Forgot password\" on the sign-in page", nil)
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many attempts; try again later", nil)
	default:
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
	}
}
