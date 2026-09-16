package tenancy_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

func TestShapeRules(t *testing.T) {
	for s, ok := range map[string]bool{
		"acme": true, "prod-smoke": true, "a1-b2-c3": true, "x": true,
		"": false, "Acme": false, "prod_smoke": false, "-lead": false, "trail-": false, "a--b": false,
		"ålborg": false, "a b": false,
	} {
		if got := tenancy.ValidSlug(s); got != ok {
			t.Errorf("ValidSlug(%q) = %v, want %v", s, got, ok)
		}
	}
	if tenancy.ValidSlug(string(make([]byte, 65))) {
		t.Error("65-byte slug accepted")
	}
	if tenancy.ValidName("") || !tenancy.ValidName("Team QA") {
		t.Error("ValidName")
	}
}

// TestRejectsBeforeRoundTrip needs no database: every branch returns before
// touching the pool, and the zero-value DB would panic if it did not.
func TestRejectsBeforeRoundTrip(t *testing.T) {
	svc := tenancy.New(nil)
	ctx := context.Background()
	if _, err := svc.CreateOrganization(ctx, "Acme", "Not A Slug"); !errors.Is(err, tenancy.ErrInvalid) {
		t.Errorf("bad slug: %v", err)
	}
	if _, err := svc.CreateOrganization(ctx, "", "acme"); !errors.Is(err, tenancy.ErrInvalid) {
		t.Errorf("empty name: %v", err)
	}
	if _, err := svc.GetProject(ctx, "not-a-uuid"); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("malformed id should be not found: %v", err)
	}
	if _, err := svc.EnsureEnvironment(ctx, "not-a-uuid", "staging"); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("malformed project id: %v", err)
	}
	if _, err := svc.EnsureEnvironment(ctx, "00000000-0000-0000-0000-000000000000", "Prod"); !errors.Is(err, tenancy.ErrInvalid) {
		t.Errorf("bad key: %v", err)
	}
}

func TestHierarchyAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := tenancy.New(db)
	ctx := context.Background()

	org, err := svc.CreateOrganization(ctx, "Acme", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if org.ID == "" || org.PlanID != "free" || org.CreatedAt.IsZero() {
		t.Errorf("org = %+v", org)
	}
	if _, err := svc.CreateOrganization(ctx, "Acme 2", "acme"); !errors.Is(err, tenancy.ErrConflict) {
		t.Errorf("duplicate slug: %v", err)
	}

	ws, err := svc.CreateWorkspace(ctx, org.ID, "Payments")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateWorkspace(ctx, org.ID, "Payments"); !errors.Is(err, tenancy.ErrConflict) {
		t.Errorf("duplicate workspace name: %v", err)
	}
	ghost := tenancy.OrganizationID("00000000-0000-0000-0000-000000000000")
	if _, err := svc.CreateWorkspace(ctx, ghost, "Orphan"); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("unknown org should be not found, got %v", err)
	}

	p, err := svc.CreateProject(ctx, ws.ID, "checkout-e2e", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.DefaultBranch != "main" {
		t.Errorf("default branch = %q", p.DefaultBranch)
	}
	got, err := svc.GetProject(ctx, p.ID)
	if err != nil || got != p {
		t.Errorf("GetProject = %+v, %v; want %+v", got, err, p)
	}
	if _, err := svc.GetProject(ctx, tenancy.ProjectID(ghost)); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}

	sc, err := svc.ProjectScope(ctx, p.ID)
	if err != nil || sc != (tenancy.Scope{OrgID: org.ID, WorkspaceID: ws.ID, ProjectID: p.ID}) {
		t.Errorf("scope = %+v, %v", sc, err)
	}

	// EnsureEnvironment: first sight creates, later sights return the same
	// row, and a burst of concurrent first sights agrees on one id.
	e1, err := svc.EnsureEnvironment(ctx, p.ID, "staging")
	if err != nil {
		t.Fatal(err)
	}
	e2, err := svc.EnsureEnvironment(ctx, p.ID, "staging")
	if err != nil || e2 != e1 {
		t.Errorf("second sight: %+v, %v; want %+v", e2, err, e1)
	}
	var wg sync.WaitGroup
	ids := make(chan tenancy.EnvironmentID, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := svc.EnsureEnvironment(ctx, p.ID, "prod-smoke")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- e.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[tenancy.EnvironmentID]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Errorf("concurrent first sight produced %d distinct ids", len(seen))
	}
	if _, err := svc.EnsureEnvironment(ctx, tenancy.ProjectID(ghost), "staging"); !errors.Is(err, tenancy.ErrNotFound) {
		t.Errorf("unknown project on ensure: %v", err)
	}

	// Writes join the caller's transaction (§6.9.6): a rollback undoes them.
	err = db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := svc.CreateWorkspace(ctx, org.ID, "Doomed"); err != nil {
			return err
		}
		return errors.New("abort")
	})
	if err == nil {
		t.Fatal("expected abort")
	}
	if ws2, err := svc.CreateWorkspace(ctx, org.ID, "Doomed"); err != nil || ws2.Name != "Doomed" {
		t.Errorf("workspace survived a rolled-back transaction: %v", err)
	}
}
