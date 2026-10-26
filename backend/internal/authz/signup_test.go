package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Self-serve sign-up with role approval (ADR-001, variant A "join_org").

func TestSignupAndApprovalAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	project := "11111111-1111-4111-8111-111111111111"
	scope := authz.ScopeRef{Level: authz.LevelProject, ID: project}
	meta := authz.SessionMeta{IP: "203.0.113.5", UserAgent: "test"}

	// Sign up requesting member on the project.
	_, secret, err := svc.Signup(ctx, "joiner@example.com", "longenoughpw12", scope, authz.RoleMember, "please let me in", meta)
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if secret == "" {
		t.Fatalf("no session opened on signup")
	}
	u, err := svc.GetUserByEmail(ctx, "joiner@example.com")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	// No role yet: the account reaches nothing until approved.
	if ms, _ := svc.ListMemberships(ctx, u.ID); len(ms) != 0 {
		t.Fatalf("expected zero memberships before approval, got %v", ms)
	}

	// The request is pending on the node.
	reqs, err := svc.AccessRequests(ctx, scope)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(reqs) != 1 || reqs[0].Email != "joiner@example.com" || reqs[0].RequestedRole != authz.RoleMember || reqs[0].Note != "please let me in" {
		t.Fatalf("unexpected requests: %+v", reqs)
	}

	// Signing up again with the same email is a conflict (no second account).
	if _, _, err := svc.Signup(ctx, "joiner@example.com", "longenoughpw12", scope, authz.RoleMember, "", meta); !errors.Is(err, authz.ErrAccountExists) {
		t.Errorf("duplicate signup: want ErrAccountExists, got %v", err)
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

	// Deciding the same request again finds no pending row.
	if err := svc.DecideAccessRequest(ctx, scope, reqs[0].ID, "approved", "", "admin@example.com"); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("re-decide: want ErrNotFound, got %v", err)
	}
}

func TestSignupDenyAndValidationAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := authz.New(db, pepper)
	ctx := context.Background()
	project := "22222222-2222-4222-8222-222222222222"
	scope := authz.ScopeRef{Level: authz.LevelProject, ID: project}
	meta := authz.SessionMeta{IP: "203.0.113.9"}

	// A role that does not live at this level is rejected before any account
	// is created.
	if _, _, err := svc.Signup(ctx, "x@example.com", "longenoughpw12", scope, authz.RoleOrgOwner, "", meta); !errors.Is(err, authz.ErrInvalid) {
		t.Errorf("role/level mismatch: want ErrInvalid, got %v", err)
	}
	if _, err := svc.GetUserByEmail(ctx, "x@example.com"); !errors.Is(err, authz.ErrNotFound) {
		t.Errorf("account created despite invalid role: %v", err)
	}

	// Deny: the account stays, gets no role, and the queue clears.
	if _, _, err := svc.Signup(ctx, "denyme@example.com", "longenoughpw12", scope, authz.RoleViewer, "", meta); err != nil {
		t.Fatalf("signup: %v", err)
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
