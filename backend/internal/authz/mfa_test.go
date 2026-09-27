package authz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// RFC 6238 Appendix B, SHA-1 rows, truncated to the six digits every
// authenticator app shows.
func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := authz.B32ForTest("12345678901234567890")
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		if got := authz.TOTPCodeForTest(secret, time.Unix(unix, 0)); got != want {
			t.Errorf("T=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestMFALifecycle(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	clock := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	svc := authz.NewForTest(db, pepper, func() time.Time { return clock })
	ctx := context.Background()
	meta := authz.SessionMeta{IP: "203.0.113.7", UserAgent: "test"}
	const pw = "correct horse battery staple"

	u, _ := svc.CreateUser(ctx, "alice@example.com")
	_ = svc.SetPassword(ctx, u.ID, pw, "")
	_ = svc.VerifyEmail(ctx, u.ID)
	keep, _, err := svc.Login(ctx, "alice@example.com", pw, meta)
	if err != nil {
		t.Fatal(err)
	}
	other, otherSecret, _ := svc.Login(ctx, "alice@example.com", pw, meta)

	if _, err := svc.EnableMFA(ctx, u.ID, "123456", keep.ID); !errors.Is(err, authz.ErrConflict) {
		t.Errorf("enable before setup: %v", err)
	}
	setup, err := svc.BeginMFASetup(ctx, u.ID)
	if err != nil || len(setup.Secret) != 32 || setup.URI == "" {
		t.Fatalf("setup: %+v %v", setup, err)
	}
	if _, err := svc.EnableMFA(ctx, u.ID, "000000", keep.ID); !errors.Is(err, authz.ErrInvalidCode) {
		t.Errorf("enable with a wrong code: %v", err)
	}
	if st, _ := svc.MFAStatus(ctx, u.ID); st.Enabled {
		t.Error("MFA on before a valid code")
	}
	codes, err := svc.EnableMFA(ctx, u.ID, authz.TOTPCodeForTest(setup.Secret, clock), keep.ID)
	if err != nil || len(codes) != 10 {
		t.Fatalf("enable: %v %v", codes, err)
	}
	if _, err := svc.BeginMFASetup(ctx, u.ID); !errors.Is(err, authz.ErrConflict) {
		t.Errorf("setup while on: %v", err)
	}
	// Turning MFA on revokes every other session.
	if _, _, err := svc.AuthenticateSession(ctx, otherSecret); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("session %s survived enabling MFA: %v", other.ID, err)
	}
	if st, _ := svc.MFAStatus(ctx, u.ID); !st.Enabled || st.Required || st.RecoveryCodesLeft != 10 {
		t.Errorf("status: %+v", st)
	}

	// The password alone now opens a challenge, not a session.
	login := func() *authz.MFARequired {
		t.Helper()
		_, _, err := svc.Login(ctx, "alice@example.com", pw, meta)
		var ch *authz.MFARequired
		if !errors.As(err, &ch) || ch.Challenge == "" || !ch.ExpiresAt.Equal(clock.Add(5*time.Minute)) {
			t.Fatalf("login with MFA on: %v", err)
		}
		return ch
	}
	ch := login()
	// The code used to enable is spent: same 30-second step, refused.
	if _, _, err := svc.CompleteMFALogin(ctx, ch.Challenge, authz.TOTPCodeForTest(setup.Secret, clock), meta); !errors.Is(err, authz.ErrInvalidCode) {
		t.Errorf("replayed code: %v", err)
	}
	clock = clock.Add(30 * time.Second)
	sess, secret, err := svc.CompleteMFALogin(ctx, ch.Challenge, authz.TOTPCodeForTest(setup.Secret, clock), meta)
	if err != nil || secret == "" || sess.UserID != u.ID {
		t.Fatalf("complete: %v", err)
	}
	if _, _, err := svc.CompleteMFALogin(ctx, ch.Challenge, authz.TOTPCodeForTest(setup.Secret, clock.Add(30*time.Second)), meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("challenge used twice: %v", err)
	}
	// One step of drift either way is accepted.
	clock = clock.Add(90 * time.Second)
	if _, _, err := svc.CompleteMFALogin(ctx, login().Challenge, authz.TOTPCodeForTest(setup.Secret, clock.Add(-30*time.Second)), meta); err != nil {
		t.Errorf("code one step old: %v", err)
	}
	// A recovery code works once, typed any way.
	ch = login()
	if _, _, err := svc.CompleteMFALogin(ctx, ch.Challenge, " "+codes[0][:5]+codes[0][6:]+" ", meta); err != nil {
		t.Errorf("recovery code: %v", err)
	}
	if _, _, err := svc.CompleteMFALogin(ctx, login().Challenge, codes[0], meta); !errors.Is(err, authz.ErrInvalidCode) {
		t.Errorf("recovery code twice: %v", err)
	}
	if st, _ := svc.MFAStatus(ctx, u.ID); st.RecoveryCodesLeft != 9 {
		t.Errorf("recovery codes left: %d", st.RecoveryCodesLeft)
	}
	// A challenge dies after five minutes.
	ch = login()
	clock = clock.Add(6 * time.Minute)
	if _, _, err := svc.CompleteMFALogin(ctx, ch.Challenge, authz.TOTPCodeForTest(setup.Secret, clock), meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("expired challenge: %v", err)
	}
	if _, _, err := svc.CompleteMFALogin(ctx, "atp_mfa_nonsense", "123456", meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("malformed challenge: %v", err)
	}
	// Guessing codes locks the account like guessing passwords (§11.2).
	// (A fresh 15-minute window: the wrong recovery code above counted.)
	clock = clock.Add(16 * time.Minute)
	ch = login()
	for i := 0; i < 5; i++ {
		if _, _, err := svc.CompleteMFALogin(ctx, ch.Challenge, "000000", meta); !errors.Is(err, authz.ErrInvalidCode) {
			t.Fatalf("guess %d: %v", i, err)
		}
	}
	if _, _, err := svc.CompleteMFALogin(ctx, ch.Challenge, authz.TOTPCodeForTest(setup.Secret, clock), meta); !errors.Is(err, authz.ErrRateLimited) {
		t.Errorf("after five wrong codes: %v", err)
	}
	clock = clock.Add(16 * time.Minute)

	// An org owner may not turn MFA off, and one without it is flagged.
	org := "11111111-1111-4111-8111-111111111111"
	if _, err := svc.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: org}, authz.RoleOrgOwner); err != nil {
		t.Fatal(err)
	}
	if err := svc.DisableMFA(ctx, u.ID, authz.TOTPCodeForTest(setup.Secret, clock)); !errors.Is(err, authz.ErrMFAMandatory) {
		t.Errorf("owner disabling: %v", err)
	}
	if p, _, _ := svc.AuthenticateSession(ctx, secret); p.MFAEnrollmentRequired {
		t.Error("owner with MFA flagged for enrolment")
	}
	// Leaving the owner role takes another owner (ErrLastOwner otherwise).
	coOwner, _ := svc.CreateUser(ctx, "co-owner@example.com")
	if _, err := svc.GrantRole(ctx, coOwner.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: org}, authz.RoleOrgOwner); err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: org}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DisableMFA(ctx, u.ID, "000000"); !errors.Is(err, authz.ErrInvalidCode) {
		t.Errorf("disable with a wrong code: %v", err)
	}
	if err := svc.DisableMFA(ctx, u.ID, authz.TOTPCodeForTest(setup.Secret, clock)); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if st, _ := svc.MFAStatus(ctx, u.ID); st.Enabled || st.RecoveryCodesLeft != 0 {
		t.Errorf("status after disable: %+v", st)
	}
	if _, _, err := svc.Login(ctx, "alice@example.com", pw, meta); err != nil {
		t.Errorf("login after disable: %v", err)
	}
	if _, err := svc.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelOrganization, ID: org}, authz.RoleOrgOwner); err != nil {
		t.Fatal(err)
	}
	if p, _, _ := svc.AuthenticateSession(ctx, secret); !p.MFAEnrollmentRequired {
		t.Error("owner without MFA not flagged for enrolment")
	}

	// Every change is on the record, newest first.
	audit, _ := svc.ListAudit(ctx, "", 20)
	var actions []string
	for _, a := range audit {
		actions = append(actions, a.Action)
	}
	if got := join(actions); got != "mfa.disable role.revoke mfa.recovery_used mfa.enable" {
		t.Errorf("audit: %s", got)
	}
}

func join(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += " "
		}
		out += v
	}
	return out
}

// The operator's way back in (cmd/admin user mfa-reset): no code needed,
// every session dies, and the record says who and why.
func TestResetMFA(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	clock := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	svc := authz.NewForTest(db, pepper, func() time.Time { return clock })
	ctx := context.Background()
	meta := authz.SessionMeta{IP: "203.0.113.8"}
	const pw = "lost my phone and my codes"
	u, _ := svc.CreateUser(ctx, "bob@example.com")
	_ = svc.SetPassword(ctx, u.ID, pw, "")
	_ = svc.VerifyEmail(ctx, u.ID)
	sess, secret, _ := svc.Login(ctx, "bob@example.com", pw, meta)
	setup, _ := svc.BeginMFASetup(ctx, u.ID)
	if _, err := svc.EnableMFA(ctx, u.ID, authz.TOTPCodeForTest(setup.Secret, clock), sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ResetMFA(ctx, u.ID, "", "ticket 12"); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("reset without an actor: %v", err)
	}
	if err := svc.ResetMFA(ctx, u.ID, "ops@example.com", " "); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("reset without a reason: %v", err)
	}
	if err := svc.ResetMFA(ctx, u.ID, "ops@example.com", "ticket 12: identity checked by video call"); err != nil {
		t.Fatal(err)
	}
	if st, _ := svc.MFAStatus(ctx, u.ID); st.Enabled || st.RecoveryCodesLeft != 0 {
		t.Errorf("status after reset: %+v", st)
	}
	if _, _, err := svc.AuthenticateSession(ctx, secret); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("session survived the reset: %v", err)
	}
	if _, _, err := svc.Login(ctx, "bob@example.com", pw, meta); err != nil {
		t.Errorf("password login after reset: %v", err)
	}
	if err := svc.ResetMFA(ctx, u.ID, "ops@example.com", "again"); !errors.Is(err, authz.ErrConflict) {
		t.Errorf("reset with MFA already off: %v", err)
	}
	audit, _ := svc.ListAudit(ctx, "", 1)
	if len(audit) != 1 || audit[0].Action != "mfa.disable" || audit[0].Actor != "ops@example.com" || audit[0].Details["via"] != "operator reset" || audit[0].Details["reason"] != "ticket 12: identity checked by video call" {
		t.Errorf("audit: %+v", audit)
	}
}
