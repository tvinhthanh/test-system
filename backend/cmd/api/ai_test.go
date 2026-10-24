package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/aisuggest"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// A stand-in for Anthropic's Models API: "sk-ant-good" may use every model
// but Fable, anything else is rejected — enough for the key check on save.
func fakeAnthropic(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Header.Get("X-Api-Key") != "sk-ant-good":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
		case strings.HasSuffix(r.URL.Path, "/claude-fable-5-1"):
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"not_found_error","message":"model"}}`))
		default:
			_, _ = w.Write([]byte(`{"type":"model","id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5","created_at":"2026-09-01T00:00:00Z"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

type draftingLLM struct{ user string }

func (d *draftingLLM) JSON(_ context.Context, req llm.Request) (llm.Result, error) {
	d.user = req.User
	return llm.Result{JSON: json.RawMessage(`{"cases":[{"title":"Đăng nhập đúng","priority":"p0","preconditions":"","why":"Chặn release.","steps":[{"action":"Mở trang đăng nhập","expected":"Thấy form"}]}]}`)}, nil
}
func (d *draftingLLM) CheckKey(context.Context, string, string) error { return nil }

// QĐ-15 through the api: organization admins keep the key (checked with the
// provider, never shown back, audited), testers ask for drafts with the
// organization's key or get told there is none, plans carry their test type.
func TestAISettingsAndSuggestions(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin},
		LLMBaseURL: fakeAnthropic(t)}
	handler, _ := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	project, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	staging, _ := ten.CreateEnvironment(ctx, project.ID, "staging", "https://staging.shop.example")
	signIn := func(email string, scope authz.ScopeRef, role authz.Role) *client {
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, email+" passphrase", "")
		_ = az.VerifyEmail(ctx, u.ID)
		if _, err := az.GrantRole(ctx, u.ID, scope, role); err != nil {
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
	onProject := authz.ScopeRef{Level: authz.LevelProject, ID: string(project.ID)}
	admin := signIn("ana@acme.example", authz.ScopeRef{Level: authz.LevelOrganization, ID: string(org.ID)}, authz.RoleOrgAdmin)
	tester := signIn("tia@acme.example", onProject, authz.RoleMember)
	viewer := signIn("vic@acme.example", onProject, authz.RoleViewer)
	settings := "/v1/organizations/" + string(org.ID) + "/ai-settings"
	base := "/v1/projects/" + string(project.ID)

	got := admin.get(settings)
	if got.code != 200 || got.body["organization"] != nil || got.body["effective"].(map[string]any)["available"] != false || len(got.body["models"].([]any)) != 4 {
		t.Fatalf("no key yet: %d %s", got.code, got.raw)
	}
	if r := tester.get(settings); r.code != 403 && r.code != 404 {
		t.Errorf("a tester reads the AI settings: %d", r.code)
	}
	if r := tester.do("POST", settings, map[string]any{"apiKey": "sk-ant-good", "model": "claude-sonnet-5-5"}, nil); r.code != 403 && r.code != 404 {
		t.Errorf("a tester saves a key: %d", r.code)
	}
	if r := tester.post(base+"/case-suggestions", map[string]any{"testType": "smoke"}); r.code != 409 || r.errCode() != "AI_NOT_CONFIGURED" {
		t.Errorf("no key anywhere: %d %s", r.code, r.raw)
	}
	if r := admin.do("POST", settings, map[string]any{"apiKey": "sk-ant-wrong", "model": "claude-sonnet-5-5"}, nil); r.code != 400 || r.errCode() != "AI_KEY_REJECTED" {
		t.Errorf("rejected key: %d %s", r.code, r.raw)
	}
	if r := admin.do("POST", settings, map[string]any{"apiKey": "sk-ant-good", "model": "claude-fable-5-1"}, nil); r.code != 400 || r.errCode() != "AI_MODEL_UNAVAILABLE" {
		t.Errorf("model the key cannot use: %d %s", r.code, r.raw)
	}
	if r := admin.do("POST", settings, map[string]any{"apiKey": "sk-ant-good", "model": "gpt-5"}, nil); r.code != 400 {
		t.Errorf("model off the list: %d %s", r.code, r.raw)
	}
	// This server has no key of its own: choosing it is refused.
	if r := admin.do("POST", settings, map[string]any{"keySource": "server", "model": "claude-opus-5-5"}, nil); r.code != 400 || !strings.Contains(r.raw, `"keySource"`) {
		t.Errorf("server key on a server without one: %d %s", r.code, r.raw)
	}
	if r := admin.do("POST", settings, map[string]any{"keySource": "server", "apiKey": "sk-ant-good", "model": "claude-opus-5-5"}, nil); r.code != 400 {
		t.Errorf("a key sent with keySource server: %d %s", r.code, r.raw)
	}
	saved := admin.do("POST", settings, map[string]any{"keySource": "organization", "apiKey": "sk-ant-good", "model": "claude-sonnet-5-5"}, nil)
	own, _ := saved.body["organization"].(map[string]any)
	if saved.code != 200 || own["keyHint"] != "good" || own["model"] != "claude-sonnet-5-5" || strings.Contains(saved.raw, "sk-ant-good") {
		t.Fatalf("save: %d %s", saved.code, saved.raw)
	}
	var details string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT details::text FROM authz_audit WHERE action = 'organization.ai_settings'`).Scan(&details); err != nil || strings.Contains(details, "sk-ant") || !strings.Contains(details, "good") {
		t.Errorf("audit: %q %v", details, err)
	}
	if r := viewer.get(base + "/ai-status"); r.body["available"] != true || r.body["source"] != "organization" || r.body["model"] != "claude-sonnet-5-5" {
		t.Errorf("status: %s", r.raw)
	}

	// Drafts: the environment's address is used, the drafts come back as text.
	if r := viewer.post(base+"/case-suggestions", map[string]any{"testType": "smoke"}); r.code != 403 {
		t.Errorf("viewer asks: %d", r.code)
	}
	if r := tester.post(base+"/case-suggestions", map[string]any{"testType": "chaos"}); r.code != 400 {
		t.Errorf("unknown type: %d %s", r.code, r.raw)
	}
	asked := tester.post(base+"/case-suggestions", map[string]any{"testType": "smoke", "environmentId": string(staging.ID), "context": "Kiểm tra đăng nhập", "count": 3})
	if asked.code != 202 || asked.body["url"] != "https://staging.shop.example" || asked.body["status"] != "queued" {
		t.Fatalf("ask: %d %s", asked.code, asked.raw)
	}
	fake := &draftingLLM{}
	w := aisuggest.New(aisuggest.Deps{DB: db, Pepper: []byte(cfg.TokenPepper), LLM: fake}).Worker(1)
	if did, err := w.RunOnce(ctx); !did || err != nil {
		t.Fatalf("run: %v %v", did, err)
	}
	if !strings.Contains(fake.user, "Kiểm tra đăng nhập") || !strings.Contains(fake.user, "https://staging.shop.example") {
		t.Errorf("prompt: %s", fake.user)
	}
	done := viewer.get(base + "/case-suggestions/" + asked.body["id"].(string))
	drafts, _ := done.body["drafts"].([]any)
	if done.body["status"] != "done" || len(drafts) != 1 || drafts[0].(map[string]any)["title"] != "Đăng nhập đúng" || done.body["keySource"] != "organization" {
		t.Fatalf("done: %s", done.raw)
	}

	// Plans record their test type.
	if r := tester.post(base+"/test-plans", map[string]any{"name": "Smoke", "testType": "chaos", "caseIds": []string{}}); r.code != 400 {
		t.Errorf("plan with an unknown type: %d %s", r.code, r.raw)
	}
	plan := tester.post(base+"/test-plans", map[string]any{"name": "Smoke 23-10", "testType": "smoke", "environmentId": string(staging.ID), "caseIds": []string{}})
	if plan.code != 201 || plan.body["testType"] != "smoke" {
		t.Fatalf("plan: %d %s", plan.code, plan.raw)
	}

	if r := admin.do("DELETE", settings, nil, nil); r.code != 200 || r.body["organization"] != nil {
		t.Errorf("delete: %d %s", r.code, r.raw)
	}
	if r := viewer.get(base + "/ai-status"); r.body["available"] != false {
		t.Errorf("status after delete: %s", r.raw)
	}
}
