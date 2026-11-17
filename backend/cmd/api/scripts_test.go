package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
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

// formUpload posts fields and an archive as multipart/form-data.
func formUpload(c *client, path string, fields map[string]string, archive []byte) resp {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	if archive != nil {
		part, _ := mw.CreateFormFile("archive", "tests.tar.gz")
		_, _ = part.Write(archive)
	}
	_ = mw.Close()
	return c.do("POST", path, buf.Bytes(), map[string]string{"Content-Type": mw.FormDataContentType()})
}

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// Script runs (QĐ-19 tier 4) through the api: off without SANDBOX, the
// archive is checked before anything is queued, a k6 script needs the
// ownership proof, a viewer reads but does not run.
func TestScriptRunsAPI(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	pepper := "test-pepper-test-pepper-test-pepper!"
	newServer := func(on bool) *httptest.Server {
		cfg := config.Config{Env: "dev", TokenPepper: pepper, DashboardOrigins: []string{origin}, Sandbox: on, ScriptMaxSeconds: 600,
			LoadMaxVUs: 20, LoadMaxSeconds: 60, PublicURL: "https://atp.example"}
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
	byIP, _ := ten.CreateEnvironment(ctx, project.ID, "lan", "http://203.0.113.10:8080")
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
	path := "/v1/projects/" + string(project.ID) + "/script-runs"
	pw := tgz(t, map[string]string{"playwright.config.ts": "export default {}", "tests/a.spec.ts": "test()"})
	env := string(staging.ID)

	if r := formUpload(offTester, path, map[string]string{"kind": "playwright", "environmentId": env}, pw); r.code != 409 || !strings.Contains(r.raw, "SCRIPTS_OFF") {
		t.Errorf("off: %d %s", r.code, r.raw)
	}
	if r := offTester.get(path); r.code != 200 || r.body["enabled"] != false {
		t.Errorf("list when off: %d %s", r.code, r.raw)
	}
	if r := formUpload(viewer, path, map[string]string{"kind": "playwright", "environmentId": env}, pw); r.code != 403 {
		t.Errorf("viewer runs a script: %d %s", r.code, r.raw)
	}
	for name, c := range map[string]struct {
		fields  map[string]string
		archive []byte
		code    int
		want    string
	}{
		"no archive":       {map[string]string{"kind": "playwright", "environmentId": env}, nil, 400, "archive"},
		"not a tar.gz":     {map[string]string{"kind": "playwright", "environmentId": env}, []byte("hello"), 400, "not a .tar.gz"},
		"no config":        {map[string]string{"kind": "playwright", "environmentId": env}, tgz(t, map[string]string{"a.js": "x"}), 400, "playwright.config"},
		"vus not a number": {map[string]string{"kind": "k6", "environmentId": env, "vus": "many"}, pw, 400, "vus"},
		"no environment":   {map[string]string{"kind": "playwright"}, pw, 400, "environmentId"},
		"by IP":            {map[string]string{"kind": "playwright", "environmentId": string(byIP.ID)}, pw, 400, "by name"},
		"k6 unproved":      {map[string]string{"kind": "k6", "environmentId": env, "entry": "tests/a.spec.ts", "vus": "2", "seconds": "10"}, pw, 409, "OWNERSHIP_NOT_PROVED"},
		"too large":        {map[string]string{"kind": "playwright", "environmentId": env}, bytes.Repeat([]byte{1}, 10<<20+10), 413, "PAYLOAD_TOO_LARGE"},
	} {
		r := formUpload(tester, path, c.fields, c.archive)
		if r.code != c.code || !strings.Contains(r.raw, c.want) {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}
	r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env, "grep": "inbox"}, pw)
	if r.code != 202 || r.body["status"] != "queued" || r.body["entry"] != "playwright.config.ts" || r.body["vus"] != nil || r.body["timeoutSeconds"] != float64(600) {
		t.Fatalf("create: %d %s", r.code, r.raw)
	}
	id, _ := r.body["id"].(string)
	if r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env}, pw); r.code != 429 || !strings.Contains(r.raw, "TOO_MANY_SCRIPT_RUNS") {
		t.Errorf("second at once: %d %s", r.code, r.raw)
	}
	if r := viewer.get(path); r.code != 200 || len(r.items()) != 1 || r.body["enabled"] != true || strings.Contains(r.raw, `"log"`) {
		t.Errorf("list: %d %s", r.code, r.raw)
	}
	if r := viewer.get(path + "/" + id); r.code != 200 || r.body["grep"] != "inbox" || r.body["log"] != "" {
		t.Errorf("get: %d %s", r.code, r.raw)
	}
	if r := viewer.get(path + "/c0000000-0000-4000-8000-000000000009"); r.code != 404 {
		t.Errorf("unknown: %d", r.code)
	}
}
