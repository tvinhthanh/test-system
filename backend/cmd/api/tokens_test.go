package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// Ingest tokens from the dashboard (§11.3.2, §11.4): a maintainer issues
// one, the secret works for ingest and appears once, the list shows only a
// prefix, a revoke stops it and is audited; a member may not; another
// project's token is not reachable through this project.
func TestProjectTokens(t *testing.T) {
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
	checkout, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	search, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	signIn := func(email string, project tenancy.ProjectID, role authz.Role) *client {
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project)}, role); err != nil {
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
	maint := signIn("mia@acme.example", checkout.ID, authz.RoleMaintainer)
	member := signIn("max@acme.example", checkout.ID, authz.RoleMember)
	searchMaint := signIn("sam@acme.example", search.ID, authz.RoleMaintainer)
	base := "/v1/projects/" + string(checkout.ID) + "/tokens"

	if r := member.get(base); r.code != 403 {
		t.Errorf("member lists tokens: %d %s", r.code, r.raw)
	}
	if r := member.post(base, map[string]any{"name": "ci"}); r.code != 403 {
		t.Errorf("member issues: %d %s", r.code, r.raw)
	}
	for _, bad := range []map[string]any{{"name": " "}, {"name": "ci", "permissions": []string{"project:admin"}}, {"name": "ci", "expiresInDays": 400}} {
		if r := maint.post(base, bad); r.code != 400 {
			t.Errorf("issue %v: %d %s", bad, r.code, r.raw)
		}
	}
	r := maint.post(base, map[string]any{"name": "github-actions", "expiresInDays": 90})
	secret, _ := r.body["secret"].(string)
	tok, _ := r.body["token"].(map[string]any)
	if r.code != 201 || !strings.HasPrefix(secret, "atp_ing_") || tok["name"] != "github-actions" || tok["revokedAt"] != nil {
		t.Fatalf("issue: %d %s", r.code, r.raw)
	}
	id := tok["id"].(string)

	// The secret works for ingest, as a reporter would use it.
	ingest := func() int {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/runs", strings.NewReader(`{"localRunId":"0198c3e1-7b1a-7c3d-9e4f-0000000000a1","environmentKey":"ci","startedAt":"2026-09-29T08:00:00Z"}`))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Protocol-Version", "1.1")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := ingest(); code != 201 {
		t.Fatalf("ingest with the new token: %d", code)
	}
	// The list never carries the secret.
	list := maint.get(base)
	if len(list.items()) != 1 || strings.Contains(list.raw, secret) || !strings.HasPrefix(secret, list.items()[0].(map[string]any)["prefix"].(string)) {
		t.Errorf("list: %s", list.raw)
	}
	// Another project's maintainer cannot reach it through their project.
	if r := searchMaint.do("DELETE", "/v1/projects/"+string(search.ID)+"/tokens/"+id, nil, nil); r.code != 404 {
		t.Errorf("revoke through another project: %d %s", r.code, r.raw)
	}
	if r := member.do("DELETE", base+"/"+id, nil, nil); r.code != 403 {
		t.Errorf("member revokes: %d %s", r.code, r.raw)
	}
	if r := maint.do("DELETE", base+"/"+id, nil, nil); r.code != 204 {
		t.Fatalf("revoke: %d %s", r.code, r.raw)
	}
	if code := ingest(); code != 401 {
		t.Errorf("revoked token still ingests: %d", code)
	}
	if l := maint.get(base); l.items()[0].(map[string]any)["revokedAt"] == nil {
		t.Errorf("revoked token listed as live: %s", l.raw)
	}
	var audited int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE action = 'token.revoke' AND subject_id = $1 AND actor = 'mia@acme.example'`, id).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("audit: %d (%v)", audited, err)
	}
}
