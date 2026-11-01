package main

import (
	"context"
	"io"
	"log/slog"
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

// Auto test (QĐ-16) through the api: test accounts keep their passwords to
// themselves; a runnable case is a testmgmt case and its spec made
// together (a bad spec leaves no case behind) and saving a spec rewrites
// the case's words; a run makes its plan and needs the checker; CI starts
// and reads runs with a project:autorun token only; cases export as a
// Playwright file; a site scan needs an account whose sign-in works.
func TestAutoTest(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	pepper := "test-pepper-test-pepper-test-pepper!"
	newServer := func(on bool) *httptest.Server {
		cfg := config.Config{Env: "dev", TokenPepper: pepper, DashboardOrigins: []string{origin}, URLCheck: on, PublicURL: "https://atp.example"}
		handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		return srv
	}
	on, off := newServer(true), newServer(false)

	ten, az := tenancy.New(db), authz.New(db, []byte(pepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Chat")
	project, _ := ten.CreateProject(ctx, ws.ID, "chat", "")
	staging, _ := ten.CreateEnvironment(ctx, project.ID, "staging", "https://chat.example.com")
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
	base := "/v1/projects/" + string(project.ID)
	env := string(staging.ID)

	// Test accounts.
	accounts := base + "/environments/" + env + "/accounts"
	if r := viewer.post(accounts, map[string]any{"key": "default", "username": "u", "password": "p"}); r.code != 403 {
		t.Errorf("viewer adds an account: %d %s", r.code, r.raw)
	}
	r := tester.post(accounts, map[string]any{"key": "default", "username": "tester@chat.example.com", "password": "s3cret!", "loginUrl": "/login"})
	if r.code != 200 || r.body["loginStatus"] != "unknown" || r.body["loginSteps"] != nil || strings.Contains(r.raw, "s3cret") {
		t.Fatalf("save account: %d %s", r.code, r.raw)
	}
	if r := viewer.get(accounts); r.code != 200 || len(r.items()) != 1 || strings.Contains(r.raw, "s3cret") || strings.Contains(r.raw, "password") {
		t.Errorf("list accounts: %d %s", r.code, r.raw)
	}
	if r := tester.post(accounts, map[string]any{"key": "bad key", "username": "u", "password": "p"}); r.code != 400 {
		t.Errorf("bad key: %d %s", r.code, r.raw)
	}
	var audited int
	if err := db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM authz_audit WHERE action = 'project.configure' AND details::text LIKE '%testAccount%' AND details::text NOT LIKE '%s3cret%'`).Scan(&audited); err != nil || audited != 1 {
		t.Errorf("account change audited without the password: %d %v", audited, err)
	}
	if r := offTester.post(accounts+"/default/find-login", nil); r.code != 409 || r.errCode() != "AUTO_TEST_OFF" {
		t.Errorf("find sign-in with the checker off: %d %s", r.code, r.raw)
	}
	if r := tester.post(accounts+"/nope/find-login", nil); r.code != 404 {
		t.Errorf("find sign-in of an unknown account: %d %s", r.code, r.raw)
	}
	if r := tester.post(accounts+"/default/find-login", nil); r.code != 409 || r.errCode() != "AI_NOT_CONFIGURED" {
		t.Errorf("find sign-in without an AI key: %d %s", r.code, r.raw)
	}

	// A runnable case: the testmgmt case and its spec together.
	specs := base + "/auto-specs"
	steps := []map[string]any{
		{"do": "fill", "by": "label", "name": "Tin nhắn", "value": "{{noi_dung}}", "say": "Nhập tin nhắn"},
		{"do": "click", "by": "role", "role": "button", "name": "Gửi"},
		{"do": "expect_text", "value": "{{noi_dung}}", "expected": "Tin nhắn hiện ra"},
	}
	before := len(tester.get(base + "/test-cases").items())
	bad := tester.post(specs, map[string]any{"title": "Broken", "target": "/message", "steps": []map[string]any{{"do": "fill", "by": "label", "name": "x", "value": "{{missing}}"}}})
	if bad.code != 400 || !strings.Contains(bad.raw, "{{missing}}") {
		t.Errorf("spec with an unknown variable: %d %s", bad.code, bad.raw)
	}
	if after := len(tester.get(base + "/test-cases").items()); after != before {
		t.Errorf("a bad spec left a case behind: %d → %d", before, after)
	}
	created := tester.post(specs, map[string]any{"title": "Gửi tin nhắn", "priority": "p0", "testType": "functional", "target": "/message?to={{nguoi_nhan}}",
		"account": "default", "inputs": map[string]string{"nguoi_nhan": "bob", "noi_dung": "Xin chào"}, "steps": steps, "source": "ai"})
	if created.code != 201 || created.body["source"] != "ai" || created.body["title"] != "Gửi tin nhắn" {
		t.Fatalf("create runnable case: %d %s", created.code, created.raw)
	}
	caseID := created.body["caseId"].(string)
	tc := tester.get(base + "/test-cases/" + caseID)
	if tc.code != 200 || !strings.Contains(tc.raw, "Nhập tin nhắn") || !strings.Contains(tc.raw, "Click button “Gửi”") || !strings.Contains(tc.raw, "noi_dung = Xin chào") ||
		!strings.Contains(tc.raw, `"automated"`) || !strings.Contains(tc.raw, `"functional"`) {
		t.Errorf("the case's words: %s", tc.raw)
	}
	// Saving the spec rewrites the case's steps.
	steps[1]["name"] = "Send"
	if r := tester.post(base+"/test-cases/"+caseID+"/auto-spec", map[string]any{"target": "/message?to={{nguoi_nhan}}", "account": "default",
		"inputs": map[string]string{"nguoi_nhan": "bob", "noi_dung": "Xin chào"}, "steps": steps}); r.code != 200 {
		t.Fatalf("save spec: %d %s", r.code, r.raw)
	}
	if tc := tester.get(base + "/test-cases/" + caseID); !strings.Contains(tc.raw, "Click button “Send”") {
		t.Errorf("case not rewritten: %s", tc.raw)
	}
	list := viewer.get(specs)
	if list.code != 200 || len(list.items()) != 1 || list.body["enabled"] != true {
		t.Fatalf("list specs: %d %s", list.code, list.raw)
	}

	// Export.
	exp := viewer.get(specs + "/export?environmentId=" + env)
	content, _ := exp.body["content"].(string)
	if exp.code != 200 || !strings.Contains(content, `test("Gửi tin nhắn"`) || !strings.Contains(content, `const BASE_URL = process.env.ATP_BASE_URL ?? "https://chat.example.com"`) ||
		strings.Contains(content, "s3cret") {
		t.Errorf("export: %d %s", exp.code, content)
	}

	// Drafts need the checker and an AI key.
	drafts := base + "/auto-drafts"
	if r := offTester.post(drafts, map[string]any{"environmentId": env, "target": "/"}); r.code != 409 || r.errCode() != "AUTO_TEST_OFF" {
		t.Errorf("draft with the checker off: %d %s", r.code, r.raw)
	}
	if r := tester.post(drafts, map[string]any{"environmentId": env, "target": "/"}); r.code != 409 || r.errCode() != "AI_NOT_CONFIGURED" {
		t.Errorf("draft without a key: %d %s", r.code, r.raw)
	}

	// Runs: a plan with the cases, the checker required.
	runs := base + "/auto-runs"
	if r := offTester.post(runs, map[string]any{"environmentId": env}); r.code != 409 || r.errCode() != "AUTO_TEST_OFF" {
		t.Errorf("run with the checker off: %d %s", r.code, r.raw)
	}
	if r := viewer.post(runs, map[string]any{"environmentId": env}); r.code != 403 {
		t.Errorf("viewer runs: %d %s", r.code, r.raw)
	}
	run := tester.post(runs, map[string]any{"environmentId": env})
	if run.code != 202 || run.body["status"] != "queued" || run.body["total"] != float64(1) || run.body["planId"] == "" {
		t.Fatalf("start run: %d %s", run.code, run.raw)
	}
	plan := tester.get(base + "/test-plans/" + run.body["planId"].(string))
	if plan.code != 200 || !strings.Contains(plan.raw, "Auto test · staging") || !strings.Contains(plan.raw, caseID) {
		t.Errorf("the run's plan: %d %s", plan.code, plan.raw)
	}
	got := viewer.get(runs + "/" + run.body["id"].(string))
	cases, _ := got.body["cases"].([]any)
	if got.code != 200 || len(cases) != 1 || cases[0].(map[string]any)["status"] != "pending" {
		t.Errorf("get run: %d %s", got.code, got.raw)
	}

	// CI: a project:autorun token starts and reads runs; an ingest token
	// does not.
	issue := func(perms ...authz.Permission) string {
		is, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}, Permissions: perms, Name: "ci"})
		if err != nil {
			t.Fatal(err)
		}
		return is.Plaintext
	}
	auto, ingestOnly := issue(authz.PermProjectAutorun), issue(authz.PermProjectIngest)
	ci := newClient(t, on)
	bearer := func(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }
	if r := ci.do("POST", "/v1/auto-runs", map[string]any{"environment": "staging"}, bearer(ingestOnly)); r.code != 403 {
		t.Errorf("ingest token starts a run: %d %s", r.code, r.raw)
	}
	if r := ci.do("POST", "/v1/auto-runs", map[string]any{"environment": "nope"}, bearer(auto)); r.code != 400 {
		t.Errorf("unknown environment: %d %s", r.code, r.raw)
	}
	ciRun := ci.do("POST", "/v1/auto-runs", map[string]any{"environment": "staging", "heal": false}, bearer(auto))
	if ciRun.code != 202 || !strings.HasPrefix(ciRun.body["url"].(string), "https://atp.example/p/"+string(project.ID)+"/auto/runs/") {
		t.Fatalf("CI run: %d %s", ciRun.code, ciRun.raw)
	}
	if r := ci.do("GET", "/v1/auto-runs/"+ciRun.body["id"].(string), nil, bearer(auto)); r.code != 200 || r.body["total"] != float64(1) {
		t.Errorf("CI reads its run: %d %s", r.code, r.raw)
	}
	if r := ci.do("POST", "/v1/auto-runs", map[string]any{"environment": "staging"}, bearer(auto)); r.code != 429 || r.errCode() != "TOO_MANY_AUTO_RUNS" {
		t.Errorf("third active run: %d %s", r.code, r.raw)
	}
	// A dashboard token issue accepts the new permission.
	maint := signIn(on, "mo@acme.example", authz.RoleMaintainer)
	if r := maint.post(base+"/tokens", map[string]any{"name": "deploy", "permissions": []string{"project:autorun"}}); r.code != 201 {
		t.Errorf("issue an autorun token: %d %s", r.code, r.raw)
	}

	// Site scans: an account must already sign in.
	scans := base + "/site-scans"
	if r := tester.post(scans, map[string]any{"environmentId": env, "account": "default"}); r.code != 400 || !strings.Contains(r.raw, "sign-in steps") {
		t.Errorf("scan as an account without steps: %d %s", r.code, r.raw)
	}
	if r := offTester.post(scans, map[string]any{"environmentId": env}); r.code != 409 {
		t.Errorf("scan with checks off: %d %s", r.code, r.raw)
	}
	scan := tester.post(scans, map[string]any{"environmentId": env, "maxPages": 5})
	if scan.code != 202 || scan.body["startUrl"] != "https://chat.example.com/" || scan.body["pages"] != float64(1) {
		t.Fatalf("scan: %d %s", scan.code, scan.raw)
	}
	if r := tester.post(scans, map[string]any{"url": "https://chat.example.com"}); r.code != 429 || r.errCode() != "SCAN_RUNNING" {
		t.Errorf("second scan: %d %s", r.code, r.raw)
	}
	if r := viewer.get(scans + "/" + scan.body["id"].(string)); r.code != 200 || len(r.body["checks"].([]any)) != 1 {
		t.Errorf("get scan: %d %s", r.code, r.raw)
	}
	// The scan's page is not in the URL check list.
	if r := viewer.get(base + "/url-checks"); len(r.items()) != 0 {
		t.Errorf("scan pages listed as checks: %s", r.raw)
	}

	if r := tester.do("DELETE", accounts+"/default", nil, nil); r.code != 204 {
		t.Errorf("delete account: %d %s", r.code, r.raw)
	}
	if r := tester.do("DELETE", base+"/test-cases/"+caseID+"/auto-spec", nil, nil); r.code != 204 {
		t.Errorf("delete spec: %d %s", r.code, r.raw)
	}
}
