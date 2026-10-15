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

func TestSpecializedOverHTTP(t *testing.T) {
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
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Testing")
	project, _ := ten.CreateProject(ctx, ws.ID, "Reports", "")
	foreign, _ := ten.CreateProject(ctx, ws.ID, "Foreign", "")
	signIn := func(email string, role authz.Role) *client {
		u, err := az.CreateUser(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		if err := az.SetPassword(ctx, u.ID, email+" passphrase", ""); err != nil {
			t.Fatal(err)
		}
		if err := az.VerifyEmail(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, role); err != nil {
			t.Fatal(err)
		}
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"}); r.code != 200 {
			t.Fatalf("login: %d %s", r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	member, viewer := signIn("member@acme.example", authz.RoleMember), signIn("viewer@acme.example", authz.RoleViewer)
	base := "/v1/projects/" + string(project.ID) + "/specialized-results"
	input := map[string]any{"schemaVersion": 1, "idempotencyKey": "http-fixture", "kind": "security", "title": "Synthetic report", "state": "completed", "environment": "fixture", "applicationVersion": "1", "source": "fixture", "toolVersion": "1", "adapterVersion": "atp-v1", "startedAt": "2026-10-13T00:00:00Z", "finishedAt": "2026-10-13T00:01:00Z", "originalReport": "<script>fixture only</script>", "details": map[string]any{}}
	if r := viewer.post(base, input); r.code != 403 {
		t.Fatalf("viewer wrote: %d %s", r.code, r.raw)
	}
	token := member.csrf
	member.csrf = ""
	if r := member.post(base, input); r.code != 403 {
		t.Fatalf("missing CSRF: %d", r.code)
	}
	member.csrf = token
	created := member.post(base, input)
	if created.code != 201 || created.body["outcome"] != "inconclusive" || created.body["createdBy"] != "member@acme.example" {
		t.Fatalf("create: %d %s", created.code, created.raw)
	}
	id := created.body["id"].(string)
	if r := member.post(base, input); r.code != 201 || r.body["id"] != id {
		t.Fatalf("replay: %d %s", r.code, r.raw)
	}
	input["title"] = "Changed"
	if r := member.post(base, input); r.code != 409 {
		t.Fatalf("conflict: %d %s", r.code, r.raw)
	}
	if r := viewer.get(base + "/" + id); r.code != 200 {
		t.Fatalf("viewer read: %d", r.code)
	}
	if r := viewer.get(base + "?kind=security&outcome=inconclusive"); r.code != 200 || len(r.items()) != 1 {
		t.Fatalf("filtered list: %d %s", r.code, r.raw)
	}
	if r := viewer.get(base + "?kind=unknown"); r.code != 400 {
		t.Fatalf("invalid filter: %d", r.code)
	}
	for _, suffix := range []string{"", "/" + id, "/" + id + "/original"} {
		if r := member.get("/v1/projects/" + string(foreign.ID) + "/specialized-results" + suffix); r.code != 403 && r.code != 404 {
			t.Fatalf("foreign read: %d", r.code)
		}
	}
	response, err := viewer.http.Get(srv.URL + base + "/" + id + "/original")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(body) != input["originalReport"] || response.Header.Get("Content-Type") != "text/plain; charset=utf-8" || response.Header.Get("X-Content-Type-Options") != "nosniff" || response.Header.Get("Content-Disposition") != `attachment; filename="original-report.txt"` || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe original: %d %v %s", response.StatusCode, response.Header, body)
	}
	input["idempotencyKey"] = "bad-mapping"
	input["details"] = map[string]any{"unknownField": true}
	if r := member.post(base, input); r.code != 400 {
		t.Fatalf("unknown mapping: %d %s", r.code, r.raw)
	}
}
