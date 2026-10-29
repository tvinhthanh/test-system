package authz_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Self-serve sign-up with role approval (ADR-001, variant A "join_org"),
// proved by mail (QĐ-17, 0044).

func TestSignupAndApprovalAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	project := "11111111-1111-4111-8111-111111111111"
	scope := authz.ScopeRef{Level: authz.LevelProject, ID: project}
	meta := authz.SessionMeta{IP: "203.0.113.5", UserAgent: "test"}
	form := authz.SignupRequest{Email: " Joiner@Example.com ", Scope: scope, Role: authz.RoleMember, Note: "please let me in"}

	if err := svc.CountSignupRequest(ctx, form, meta.IP); err != nil {
		t.Fatalf("count: %v", err)
	}
	to, token, err := svc.IssueSignup(ctx, form)
	if err != nil || to != "joiner@example.com" || token == "" {
		t.Fatalf("issue: %q %q %v", to, token, err)
	}
	// Nothing exists for an address nobody proved: no account, no request.
	if _, err := svc.GetUserByEmail(ctx, "joiner@example.com"); !errors.Is(err, authz.ErrNotFound) {
		t.Fatalf("account before the link was opened: %v", err)
	}
	if reqs, _ := svc.AccessRequests(ctx, scope); len(reqs) != 0 {
		t.Fatalf("request listed before the link was opened: %+v", reqs)
	}

	// Asking again replaces the link: only the newest one works.
	_, newer, err := svc.IssueSignup(ctx, form)
	if err != nil || newer == token {
		t.Fatalf("second issue: %q %v", newer, err)
	}
	if _, _, err := svc.CompleteSignup(ctx, token, "longenoughpw12", meta); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("replaced link: want ErrNotFound, got %v", err)
	}
	// A weak password spends nothing.
	if _, _, err := svc.CompleteSignup(ctx, newer, "short", meta); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("weak password: want ErrInvalid, got %v", err)
	}
	_, secret, err := svc.CompleteSignup(ctx, newer, "longenoughpw12", meta)
	if err != nil || secret == "" {
		t.Fatalf("complete: %q %v", secret, err)
	}
	u, err := svc.GetUserByEmail(ctx, "joiner@example.com")
	if err != nil || u.EmailVerifiedAt.IsZero() || !u.HasPassword {
		t.Fatalf("account after the link: %+v %v", u, err)
	}
	if _, _, err := svc.CompleteSignup(ctx, newer, "longenoughpw12", meta); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("link used twice: want ErrNotFound, got %v", err)
	}

	// No role yet: the account reaches nothing until approved.
	if ms, _ := svc.ListMemberships(ctx, u.ID); len(ms) != 0 {
		t.Fatalf("expected zero memberships before approval, got %v", ms)
	}
	reqs, err := svc.AccessRequests(ctx, scope)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reqs) != 1 || reqs[0].Email != "joiner@example.com" || reqs[0].RequestedRole != authz.RoleMember || reqs[0].Note != "please let me in" {
		t.Fatalf("unexpected requests: %+v", reqs)
	}

	// An address with an account gets no link (the form said the same).
	if to, token, err := svc.IssueSignup(ctx, form); err != nil || to != "" || token != "" {
		t.Errorf("issue for an existing account: %q %q %v", to, token, err)
	}

	// Approve: the requested role is granted and the queue clears.
	if err := svc.DecideAccessRequest(ctx, scope, reqs[0].ID, "approved", "", "admin@example.com"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ms, _ := svc.ListMemberships(ctx, u.ID)
	if len(ms) != 1 || ms[0].Role != authz.RoleMember || ms[0].Scope != scope {
		t.Fatalf("role not granted after approval: %+v", ms)
	}
	if left, _ := svc.AccessRequests(ctx, scope); len(left) != 0 {
		t.Errorf("queue not cleared after approval: %+v", left)
	}
	if err := svc.DecideAccessRequest(ctx, scope, reqs[0].ID, "approved", "", "admin@example.com"); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("re-decide: want ErrNotFound, got %v", err)
	}
	actions := map[string]bool{}
	audit, _ := svc.ListAudit(ctx, project, 20)
	for _, a := range audit {
		actions[a.Action] = true
	}
	if !actions["access_request.create"] || !actions["access_request.approve"] {
		t.Errorf("audit: %+v", audit)
	}
}

func TestSignupDenyExpiryAndValidationAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	scope := authz.ScopeRef{Level: authz.LevelProject, ID: "22222222-2222-4222-8222-222222222222"}
	meta := authz.SessionMeta{IP: "203.0.113.9"}

	// A role that does not live at this level, a bad address or a long note
	// is refused before anything is counted or written.
	for name, form := range map[string]authz.SignupRequest{
		"role/level": {Email: "x@example.com", Scope: scope, Role: authz.RoleOrgOwner},
		"email":      {Email: "not-an-email", Scope: scope, Role: authz.RoleViewer},
		"note":       {Email: "x@example.com", Scope: scope, Role: authz.RoleViewer, Note: string(make([]byte, 1001))},
	} {
		if err := svc.CountSignupRequest(ctx, form, meta.IP); !errors.Is(err, authz.ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}

	// Five per 15 minutes per address, whether or not it has an account.
	flood := authz.SignupRequest{Email: "ghost@example.com", Scope: scope, Role: authz.RoleViewer}
	var err error
	for i := 0; i < 6; i++ {
		err = svc.CountSignupRequest(ctx, flood, "198.51.100."+string(rune('1'+i)))
	}
	if !errors.Is(err, authz.ErrRateLimited) {
		t.Errorf("sixth request for one address: want ErrRateLimited, got %v", err)
	}

	// An expired link creates nothing.
	_, stale, err := svc.IssueSignup(ctx, authz.SignupRequest{Email: "late@example.com", Scope: scope, Role: authz.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE signup_requests SET expires_at = now() - interval '1 minute'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompleteSignup(ctx, stale, "longenoughpw12", meta); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("expired link: want ErrNotFound, got %v", err)
	}

	// An account made for the address in the meantime is not taken over.
	_, token, err := svc.IssueSignup(ctx, authz.SignupRequest{Email: "taken@example.com", Scope: scope, Role: authz.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateUser(ctx, "taken@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompleteSignup(ctx, token, "longenoughpw12", meta); !errors.Is(err, authz.ErrAccountExists) {
		t.Errorf("account made meanwhile: want ErrAccountExists, got %v", err)
	}
	if reqs, _ := svc.AccessRequests(ctx, scope); len(reqs) != 0 {
		t.Errorf("request filed for someone else's account: %+v", reqs)
	}

	// Deny: the account stays, gets no role, and the queue clears.
	_, token, err = svc.IssueSignup(ctx, authz.SignupRequest{Email: "denyme@example.com", Scope: scope, Role: authz.RoleViewer})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CompleteSignup(ctx, token, "longenoughpw12", meta); err != nil {
		t.Fatalf("complete: %v", err)
	}
	reqs, _ := svc.AccessRequests(ctx, scope)
	if len(reqs) != 1 {
		t.Fatalf("want 1 pending, got %d", len(reqs))
	}
	if err := svc.DecideAccessRequest(ctx, scope, reqs[0].ID, "denied", "", "admin@example.com"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	u, _ := svc.GetUserByEmail(ctx, "denyme@example.com")
	if ms, _ := svc.ListMemberships(ctx, u.ID); len(ms) != 0 {
		t.Errorf("denied user has a role: %+v", ms)
	}
	if left, _ := svc.AccessRequests(ctx, scope); len(left) != 0 {
		t.Errorf("queue not cleared after deny")
	}
}

// 0044 on a database the first cut (0035) wrote to: accounts signed up
// without proof lose "verified" and their sessions, unless a password reset
// proved the mailbox since; other accounts are untouched.
func TestSignupVerificationMigrationUnverifiesUnprovenAccounts(t *testing.T) {
	db := pgtest.New(t, nil)
	ctx := context.Background()
	before := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() < "0044" {
			b, _ := fs.ReadFile(migrations.FS, e.Name())
			before[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	if _, err := pg.Migrate(ctx, db, before); err != nil {
		t.Fatal(err)
	}
	svc := authz.New(db, pepper)
	meta := authz.SessionMeta{IP: "203.0.113.20"}
	project := "33333333-3333-4333-8333-333333333333"
	// What 0035's Signup wrote: verified on the spot, with a request.
	oldSignup := func(email string) (string, string) {
		t.Helper()
		var id string
		if err := db.Q(ctx).QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if err := svc.SetPassword(ctx, id, "longenoughpw12", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `UPDATE users SET email_verified_at = password_updated_at WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO access_requests (user_id, scope_level, scope_id, requested_role) VALUES ($1, 'project', $2, 'viewer')`, id, project); err != nil {
			t.Fatal(err)
		}
		_, secret, err := svc.Login(ctx, email, "longenoughpw12", meta)
		if err != nil {
			t.Fatal(err)
		}
		return id, secret
	}
	_, squatterSession := oldSignup("ceo@victim.example")
	provedID, provedSession := oldSignup("proved@example.com")
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO authz_audit (action, subject_kind, subject_id, actor) VALUES ('password.reset', 'user', $1, 'proved@example.com')`, provedID); err != nil {
		t.Fatal(err)
	}
	seeded, _ := svc.CreateUser(ctx, "seeded@example.com")
	_ = svc.SetPassword(ctx, seeded.ID, "longenoughpw12", "")
	_ = svc.VerifyEmail(ctx, seeded.ID)
	_, seededSession, err := svc.Login(ctx, "seeded@example.com", "longenoughpw12", meta)
	if err != nil {
		t.Fatal(err)
	}

	ran, err := pg.Migrate(ctx, db, migrations.FS)
	if err != nil || len(ran) != 1 || ran[0] != "0044_signup_verification" {
		t.Fatalf("0044: %v %v", ran, err)
	}
	for _, c := range []struct {
		email, session string
		verified       bool
	}{
		{"ceo@victim.example", squatterSession, false},
		{"proved@example.com", provedSession, true},
		{"seeded@example.com", seededSession, true},
	} {
		u, err := svc.GetUserByEmail(ctx, c.email)
		if err != nil {
			t.Fatal(err)
		}
		if got := !u.EmailVerifiedAt.IsZero(); got != c.verified {
			t.Errorf("%s verified = %v, want %v", c.email, got, c.verified)
		}
		_, _, err = svc.AuthenticateSession(ctx, c.session)
		if signedIn := err == nil; signedIn != c.verified {
			t.Errorf("%s session alive = %v (%v), want %v", c.email, signedIn, err, c.verified)
		}
	}
	if _, _, err := svc.Login(ctx, "ceo@victim.example", "longenoughpw12", meta); !errors.Is(err, authz.ErrUnauthenticated) {
		t.Errorf("unproven account signs in: %v", err)
	}
}
