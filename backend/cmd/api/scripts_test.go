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

	// Dangerous archives are 400s that say why — checked before the
	// organization's limit, so even now (one run queued) they are refused
	// as what they are.
	evil := func(h tar.Header, body string) []byte {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "playwright.config.ts", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x"))
		_ = tw.WriteHeader(&h)
		_, _ = tw.Write([]byte(body))
		tw.Close()
		gz.Close()
		return buf.Bytes()
	}
	for name, c := range map[string]struct {
		archive []byte
		want    string
	}{
		"path traversal": {evil(tar.Header{Name: "../../etc/cron.d/x", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, "x"), "inside the archive"},
		"absolute path":  {evil(tar.Header{Name: "/etc/passwd", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}, "x"), "inside the archive"},
		"symlink":        {evil(tar.Header{Name: "secret", Typeflag: tar.TypeSymlink, Linkname: "/etc/shadow"}, ""), "no links"},
		"hard link":      {evil(tar.Header{Name: "h", Typeflag: tar.TypeLink, Linkname: "playwright.config.ts"}, ""), "no links"},
	} {
		r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env}, c.archive)
		if r.code != 400 || !strings.Contains(r.raw, c.want) {
			t.Errorf("%s: %d %s", name, r.code, r.raw)
		}
	}
	// A body over the multipart cap is cut off by MaxBytesReader: still 413.
	if r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env}, bytes.Repeat([]byte{1}, 12<<20)); r.code != 413 || !strings.Contains(r.raw, "PAYLOAD_TOO_LARGE") {
		t.Errorf("12 MB body: %d %s", r.code, r.raw[:min(len(r.raw), 200)])
	}
	// No CSRF token: refused before anything else.
	csrf := tester.csrf
	tester.csrf = ""
	if r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env}, pw); r.code != 403 {
		t.Errorf("no csrf: %d %s", r.code, r.raw)
	}
	tester.csrf = csrf
	// Another project's environment is a 400 on the field, not a run.
	other, _ := ten.CreateProject(ctx, ws.ID, "billing", "")
	otherEnv, _ := ten.CreateEnvironment(ctx, other.ID, "staging", "https://billing.example.com")
	if r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": string(otherEnv.ID)}, pw); r.code != 400 || !strings.Contains(r.raw, "environmentId") {
		t.Errorf("another project's environment: %d %s", r.code, r.raw)
	}
	// One at a time per organization, across its projects.
	tia, _ := az.GetUserByEmail(ctx, "tia@acme.example")
	if _, err := az.GrantRole(ctx, tia.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(other.ID)}, authz.RoleMember); err != nil {
		t.Fatal(err)
	}
	otherPath := "/v1/projects/" + string(other.ID) + "/script-runs"
	if r := formUpload(tester, otherPath, map[string]string{"kind": "playwright", "environmentId": string(otherEnv.ID)}, pw); r.code != 429 || !strings.Contains(r.raw, "TOO_MANY_SCRIPT_RUNS") {
		t.Errorf("a second run in the organization's other project: %d %s", r.code, r.raw)
	}
	finish := func(id string) {
		t.Helper()
		if _, err := db.Q(ctx).Exec(ctx, `UPDATE script_runs SET status = 'done', archive = NULL, finished_at = now() WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}
	finish(id)
	// k6 once the host is proved: queued with its vus and seconds.
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO site_ownership (org_id, host, token, method, status, verified_at, created_by)
		VALUES ($1, 'chat.example.com', 'abcdefghijklmnopqrstuvwxyz012345', 'file', 'verified', now(), 'tia@acme.example')`, string(org.ID)); err != nil {
		t.Fatal(err)
	}
	k6 := tgz(t, map[string]string{"load.js": "export default function () {}"})
	r = formUpload(tester, path, map[string]string{"kind": "k6", "environmentId": env, "entry": "load.js", "vus": "3", "seconds": "10", "timeoutSeconds": "60"}, k6)
	if r.code != 202 || r.body["kind"] != "k6" || r.body["vus"] != float64(3) || r.body["seconds"] != float64(10) || r.body["testRunId"] != nil || r.body["exitCode"] != nil {
		t.Fatalf("k6 proved: %d %s", r.code, r.raw)
	}
	finish(r.body["id"].(string))
	// 30 a day per project: 2 so far, 28 more, then the 31st is refused.
	if _, err := db.Q(ctx).Exec(ctx, `INSERT INTO script_runs (project_id, org_id, environment_id, kind, base, entry, timeout_seconds, archive_bytes, status, finished_at, requested_by)
		SELECT $1, $2, $3, 'playwright', 'https://chat.example.com', 'playwright.config.ts', 60, 2, 'done', now(), 'tia@acme.example' FROM generate_series(1, 28)`,
		string(project.ID), string(org.ID), env); err != nil {
		t.Fatal(err)
	}
	if r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env}, pw); r.code != 429 || !strings.Contains(r.raw, `"perDay":30`) {
		t.Errorf("the 31st today: %d %s", r.code, r.raw)
	}
	// Yesterday's runs no longer count.
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE script_runs SET created_at = now() - interval '25 hours' WHERE project_id = $1`, string(project.ID)); err != nil {
		t.Fatal(err)
	}
	if r := formUpload(tester, path, map[string]string{"kind": "playwright", "environmentId": env}, pw); r.code != 202 {
		t.Errorf("the next day: %d %s", r.code, r.raw)
	}
}
