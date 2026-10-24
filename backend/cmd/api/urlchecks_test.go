package main

import (
	"context"
	"encoding/json"
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
	"github.com/vinh/automation-testing-platform/backend/internal/urlcheck"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

type stubChecker struct{ urls []string }

func (s *stubChecker) Check(_ context.Context, url string) (urlcheck.Result, error) {
	s.urls = append(s.urls, url)
	return urlcheck.Result{
		Report:  json.RawMessage(`{"url":"` + url + `","finalUrl":"` + url + `","title":"<b>Shop</b>","verdict":"failed","checks":[{"key":"load","title":"Page loads","status":"passed","summary":"200","details":[]},{"key":"links","title":"Links work","status":"failed","summary":"1 broken link","details":["404 https://shop.example/x"]}]}`),
		Verdict: "failed",
		Desktop: []byte{0xFF, 0xD8, 0xFF, 0xE0, 9},
	}, nil
}

// URL checks (QĐ-14) through the api: members ask, viewers read, an
// environment's address is used when no url is given, the server's switch
// and the per-project queue limit hold, and a finished check is read back
// with its report and its screenshot served as an inert JPEG.
func TestURLChecks(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	newServer := func(on bool) *httptest.Server {
		cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, URLCheck: on}
		handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		return srv
	}
	on, off := newServer(true), newServer(false)

	ten, az := tenancy.New(db), authz.New(db, []byte("test-pepper-test-pepper-test-pepper!"))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	project, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	other, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	staging, _ := ten.CreateEnvironment(ctx, project.ID, "staging", "https://staging.shop.example")
	bare, _ := ten.EnsureEnvironment(ctx, project.ID, "ci")
	foreign, _ := ten.CreateEnvironment(ctx, other.ID, "staging", "https://search.example")
	signIn := func(srv *httptest.Server, email string, role authz.Role) *client {
		if _, err := az.GetUserByEmail(ctx, email); err != nil {
			u, _ := az.CreateUser(ctx, email)
			_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
			_ = az.VerifyEmail(ctx, u.ID)
			if _, err := az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, role); err != nil {
				t.Fatal(err)
			}
		}
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": email + " passphrase"}); r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return c
	}
	tester := signIn(on, "tia@acme.example", authz.RoleMember)
	viewer := signIn(on, "vic@acme.example", authz.RoleViewer)
	offTester := signIn(off, "tia@acme.example", authz.RoleMember)
	path := "/v1/projects/" + string(project.ID) + "/url-checks"

	if r := offTester.post(path, map[string]any{"url": "https://shop.example"}); r.code != 409 || r.errCode() != "URL_CHECKS_OFF" {
		t.Errorf("checks off: %d %s", r.code, r.raw)
	}
	if r := offTester.get(path); r.code != 200 || r.body["enabled"] != false {
		t.Errorf("list with checks off: %d %s", r.code, r.raw)
	}
	if r := viewer.post(path, map[string]any{"url": "https://shop.example"}); r.code != 403 {
		t.Errorf("viewer asks: %d %s", r.code, r.raw)
	}
	for _, bad := range []map[string]any{
		{"url": "javascript:alert(1)"},
		{"url": "https://admin:pw@shop.example"},
		{},
		{"environmentId": string(bare.ID)},
		{"environmentId": string(foreign.ID)},
	} {
		if r := tester.post(path, bad); r.code != 400 {
			t.Errorf("bad check %v: %d %s", bad, r.code, r.raw)
		}
	}
	typed := tester.post(path, map[string]any{"url": "https://Shop.Example/cart"})
	if typed.code != 202 || typed.body["status"] != "queued" || typed.body["url"] != "https://shop.example/cart" {
		t.Fatalf("tester asks: %d %s", typed.code, typed.raw)
	}
	fromEnv := tester.post(path, map[string]any{"environmentId": string(staging.ID)})
	if fromEnv.code != 202 || fromEnv.body["url"] != "https://staging.shop.example" || fromEnv.body["environmentId"] != string(staging.ID) {
		t.Fatalf("check of an environment: %d %s", fromEnv.code, fromEnv.raw)
	}

	stub := &stubChecker{}
	w := urlcheck.New(db, stub).Worker(1)
	for i := 0; i < 2; i++ {
		if did, err := w.RunOnce(ctx); !did || err != nil {
			t.Fatalf("run %d: %v %v", i, did, err)
		}
	}
	id := typed.body["id"].(string)
	got := viewer.get(path + "/" + id)
	report, _ := got.body["report"].(map[string]any)
	if got.code != 200 || got.body["status"] != "done" || got.body["verdict"] != "failed" || got.body["title"] != "<b>Shop</b>" || report == nil {
		t.Fatalf("viewer reads the check: %d %s", got.code, got.raw)
	}
	list := viewer.get(path)
	first := list.items()[0].(map[string]any)
	if list.body["enabled"] != true || len(list.items()) != 2 || first["report"] != nil || len(first["checkStatuses"].([]any)) != 2 {
		t.Fatalf("list: %s", list.raw)
	}
	shot := viewer.do("GET", path+"/"+id+"/screenshots/desktop", nil, nil)
	if shot.code != 200 || shot.header.Get("Content-Type") != "image/jpeg" || shot.header.Get("Content-Security-Policy") == "" {
		t.Fatalf("screenshot: %d %v", shot.code, shot.header)
	}
	if r := viewer.get(path + "/" + id + "/screenshots/mobile"); r.code != 404 {
		t.Errorf("missing phone screenshot: %d", r.code)
	}

	// Five waiting is the most a project gets.
	for i := 0; i < urlcheck.MaxQueued; i++ {
		if r := tester.post(path, map[string]any{"url": "https://shop.example/p"}); r.code != 202 {
			t.Fatalf("queue %d: %d %s", i, r.code, r.raw)
		}
	}
	if r := tester.post(path, map[string]any{"url": "https://shop.example/p"}); r.code != 429 || r.errCode() != "TOO_MANY_URL_CHECKS" {
		t.Errorf("sixth waiting check: %d %s", r.code, r.raw)
	}
}
