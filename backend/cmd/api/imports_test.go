package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// The dashboard's JUnit upload (§6.8.5, QĐ-13): project.configure — a
// maintainer imports, a member does not; the run lands in the project and
// shows in its run list like any other; the session user is recorded.
func TestJUnitUploadFromTheDashboard(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	project, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	signIn := func(email string, role authz.Role) *client {
		u, err := az.CreateUser(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, role); err != nil {
			t.Fatal(err)
		}
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"}); r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	maint := signIn("mia@acme.example", authz.RoleMaintainer)
	member := signIn("max@acme.example", authz.RoleMember)
	path := "/v1/projects/" + string(project.ID) + "/imports/junit?environment=nightly&branch=main"
	report := []byte(`<testsuite name="com.acme.CartTest" timestamp="2026-03-02T09:15:00">
	  <testcase name="adds" classname="com.acme.CartTest" time="0.1"/>
	  <testcase name="removes" classname="com.acme.CartTest" time="0.2"><failure message="still there"/></testcase>
	</testsuite>`)
	xml := map[string]string{"Content-Type": "application/xml"}

	if r := member.do("POST", path, report, xml); r.code != 403 || r.errCode() != "FORBIDDEN" {
		t.Fatalf("member upload: %d %s", r.code, r.raw)
	}
	r := maint.do("POST", path, report, xml)
	if r.code != 201 || r.body["tests"] != float64(2) || r.body["status"] != "failed" {
		t.Fatalf("maintainer upload: %d %s", r.code, r.raw)
	}
	runID := r.body["runId"].(string)
	if again := maint.do("POST", path, report, xml); again.code != 200 || again.body["runId"] != runID {
		t.Errorf("same file again: %d %s", again.code, again.raw)
	}
	// Without CSRF it is refused like any mutation.
	noCSRF := *maint
	noCSRF.csrf = ""
	if r := noCSRF.do("POST", path, report, xml); r.code != 403 || r.errCode() != "CSRF" {
		t.Errorf("upload without CSRF: %d %s", r.code, r.raw)
	}
	// It is a run of the project, dated from the report, for every reader.
	runs := member.get("/v1/projects/" + string(project.ID) + "/runs")
	if items := runs.items(); len(items) != 1 || items[0].(map[string]any)["id"] != runID || items[0].(map[string]any)["startedAt"] != "2026-03-02T09:15:00Z" {
		t.Errorf("run list: %s", runs.raw)
	}
	var who string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT trigger->>'importedBy' FROM runs WHERE id = $1`, runID).Scan(&who); err != nil || who != "user:mia@acme.example" {
		t.Errorf("importedBy = %q (%v)", who, err)
	}
}
