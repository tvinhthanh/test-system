package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

// The HTTP integration test: the exact handler the binary serves, driven
// the way the two clients drive it — the reporter with a bearer token on
// the ingest routes, then a browser with cookies on the dashboard routes of
// frontend/API_CONTRACT.md. Tenant boundaries and CSRF are asserted here
// because §11.3.3 makes the cross-tenant pass a release gate.

const origin = "http://localhost:5173"

type client struct {
	t    *testing.T
	srv  *httptest.Server
	http *http.Client
	csrf string
}

type resp struct {
	code int
	body map[string]any
	raw  string
}

func (c *client) do(method, path string, body any, headers map[string]string) resp {
	c.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, c.srv.URL+path, &buf)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Every state-changing method is a browser mutation: Origin + CSRF, as a
	// fetch() would send them (DELETE has no body and still needs both).
	if method != "GET" {
		req.Header.Set("Origin", origin)
		if c.csrf != "" {
			req.Header.Set(authz.CSRFHeader, c.csrf)
		}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

// upload posts one file as multipart/form-data, the way a browser submits
// a FormData with a File: Origin + CSRF as for any mutation, the part's
// Content-Type set from File.type.
func (c *client) upload(path, field, filename, contentType string, data []byte) resp {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.srv.URL+path, &buf)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", origin)
	if c.csrf != "" {
		req.Header.Set(authz.CSRFHeader, c.csrf)
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (c *client) get(path string) resp { return c.do("GET", path, nil, nil) }

func (c *client) post(path string, body any) resp { return c.do("POST", path, body, nil) }

func (r resp) errCode() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func (r resp) items() []any {
	it, _ := r.body["items"].([]any)
	return it
}

func newClient(t *testing.T, srv *httptest.Server) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, srv: srv, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// bootstrap does GET /v1/session and remembers the CSRF token.
func (c *client) bootstrap() resp {
	r := c.get("/v1/session")
	c.csrf, _ = r.body["csrfToken"].(string)
	return r
}

func TestDashboardContractEndToEnd(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false}
	reg := metric.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, _ := buildHandler(cfg, db, store, reg, logger)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// Seed: org → ws → two projects; alice is a member of checkout only;
	// bob is a viewer of search; an ingest token for checkout.
	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	checkout, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	search, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	alice, _ := az.CreateUser(ctx, "alice@example.com")
	_ = az.SetPassword(ctx, alice.ID, "correct horse battery staple", "")
	_ = az.VerifyEmail(ctx, alice.ID)
	_, _ = az.GrantRole(ctx, alice.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(checkout.ID)}, authz.RoleMember)
	bob, _ := az.CreateUser(ctx, "bob@example.com")
	_ = az.SetPassword(ctx, bob.ID, "another long passphrase!", "")
	_ = az.VerifyEmail(ctx, bob.ID)
	_, _ = az.GrantRole(ctx, bob.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(search.ID)}, authz.RoleViewer)
	tok, _ := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(checkout.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"})

	// A reporter pushes two runs through the real ingest routes; the worker
	// pipeline (tier 2 + flaky) is drained in-process.
	bearer := newClient(t, srv)
	auth := map[string]string{"Authorization": "Bearer " + tok.Plaintext}
	res := results.New(db)
	jobs := ingest.NewJobs(ingest.Deps{DB: db, Queue: queue.New(db), Tenancy: ten, Authz: az, Results: res, Identity: identity.New(db), Metrics: metric.New(), Logger: logger})
	flk := flaky.New(db, res)
	worker := jobs.Worker(1, func(ctx context.Context, projectID string, runID results.RunID) error {
		_, err := flk.RecomputeRun(ctx, projectID, string(runID))
		return err
	})
	ev := func(seq int, client, title, status, outcome string) map[string]any {
		return map[string]any{
			"sequence": seq, "type": "test.finished", "clientTestId": client, "occurrenceIndex": 0, "attempt": 0,
			"identity": map[string]any{"framework": "playwright", "filePath": "tests/checkout.spec.ts", "titlePath": []string{"Checkout", title}, "paramSignature": ""},
			"status":   status, "outcome": outcome, "durationMs": 100,
			"error": map[string]any{"message": "expected 402", "stack": "at checkout.spec.ts:12"},
		}
	}
	var runIDs []string
	for i, payStatus := range []string{"failed", "passed"} {
		local := fmt.Sprintf("0198c3e1-7b1a-7c3d-9e4f-0000000000%02d", i)
		r := bearer.do("POST", "/v1/runs", map[string]any{"localRunId": local, "environmentKey": "staging", "trigger": map[string]any{"ref": "refs/heads/main", "commitSha": fmt.Sprintf("c%d", i)}, "startedAt": time.Now().Add(-time.Duration(2-i) * time.Hour).UTC().Format(time.RFC3339)}, auth)
		if r.code != 201 {
			t.Fatalf("create run: %d %s", r.code, r.raw)
		}
		runIDs = append(runIDs, r.body["runId"].(string))
		outcome := "expected"
		if payStatus == "failed" {
			outcome = "unexpected"
		}
		if r := bearer.do("POST", "/v1/runs/"+local+"/events", map[string]any{"events": []any{ev(1, "ct_pay", "declines expired card", payStatus, outcome), ev(2, "ct_cart", "keeps state", "passed", "expected")}}, auth); r.code != 200 {
			t.Fatalf("events: %d %s", r.code, r.raw)
		}
		if r := bearer.do("POST", "/v1/runs/"+local+"/finish", nil, auth); r.code != 200 {
			t.Fatalf("finish: %d %s", r.code, r.raw)
		}
		if _, err := worker.Drain(ctx); err != nil {
			t.Fatal(err)
		}
	}

	// ---- Session bootstrap and login (contract: GET /v1/session, POST /v1/session) ----
	web := newClient(t, srv)
	boot := web.bootstrap()
	if boot.code != 200 || boot.body["user"] != nil || web.csrf == "" {
		t.Fatalf("anonymous bootstrap: %d %s", boot.code, boot.raw)
	}
	if r := web.get("/v1/projects"); r.code != 401 {
		t.Errorf("projects before login: %d", r.code)
	}
	// Login without the CSRF token from bootstrap is refused; wrong password is a generic 401.
	noCSRF := newClient(t, srv)
	noCSRF.bootstrap()
	noCSRF.csrf = ""
	if r := noCSRF.post("/v1/session", map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"}); r.code != 403 || r.errCode() != "CSRF" {
		t.Errorf("login without CSRF token: %d %s", r.code, r.raw)
	}
	if r := web.post("/v1/session", map[string]string{"email": "alice@example.com", "password": "wrong wrong wrong wrong"}); r.code != 401 || r.errCode() != "INVALID_CREDENTIALS" {
		t.Errorf("wrong password: %d %s", r.code, r.raw)
	}
	login := web.post("/v1/session", map[string]string{"email": "alice@example.com", "password": "correct horse battery staple"})
	user, _ := login.body["user"].(map[string]any)
	if login.code != 200 || user["email"] != "alice@example.com" || user["name"] != "alice" || user["id"] != alice.ID || login.body["csrfToken"] == web.csrf {
		t.Fatalf("login: %d %s", login.code, login.raw)
	}
	web.csrf = login.body["csrfToken"].(string) // rotated
	if r := web.bootstrap(); r.body["user"] == nil || r.body["csrfToken"] != web.csrf {
		t.Errorf("bootstrap after login: %s", r.raw)
	}

	// ---- Projects ----
	pr := web.get("/v1/projects?limit=50")
	if pr.code != 200 || len(pr.items()) != 1 || pr.body["nextCursor"] != nil {
		t.Fatalf("projects: %d %s", pr.code, pr.raw)
	}
	p0 := pr.items()[0].(map[string]any)
	if p0["id"] != string(checkout.ID) || p0["name"] != "checkout" || p0["workspaceName"] != "Payments" {
		t.Errorf("project DTO: %v", p0)
	}
	// §11.3.2 for the UI: a member's actions are exactly view, triage and
	// testmgmt.write — the buttons the screens may show (the server still
	// checks every call below).
	if fmt.Sprint(p0["actions"]) != "[view triage testmgmt.write]" {
		t.Errorf("member actions: %v", p0["actions"])
	}
	base := "/v1/projects/" + string(checkout.ID)

	// ---- Runs: envelope, DTO, sort, cursor, filters ----
	rs := web.get(base + "/runs?limit=1&sort=-startedAt")
	if rs.code != 200 || len(rs.items()) != 1 || rs.body["nextCursor"] == nil {
		t.Fatalf("runs page 1: %d %s", rs.code, rs.raw)
	}
	run := rs.items()[0].(map[string]any)
	if run["id"] != runIDs[1] || run["status"] != "passed" || run["environmentName"] != "staging" || run["branch"] != "main" || run["commitSha"] != "c1" ||
		run["totalCount"] != float64(2) || run["passedCount"] != float64(2) || run["failedCount"] != float64(0) || run["flakyCount"] != float64(0) || run["durationMs"] == nil {
		t.Errorf("run DTO: %v", run)
	}
	rs2 := web.get(base + "/runs?limit=1&sort=-startedAt&cursor=" + url.QueryEscape(rs.body["nextCursor"].(string)))
	if rs2.code != 200 || len(rs2.items()) != 1 || rs2.items()[0].(map[string]any)["id"] != runIDs[0] || rs2.body["nextCursor"] != nil {
		t.Errorf("runs page 2: %d %s", rs2.code, rs2.raw)
	}
	if r := web.get(base + "/runs?status=failed"); len(r.items()) != 1 || r.items()[0].(map[string]any)["failedCount"] != float64(1) {
		t.Errorf("status filter: %s", r.raw)
	}
	if r := web.get(base + "/runs?q=c0"); len(r.items()) != 1 {
		t.Errorf("q filter: %s", r.raw)
	}
	if r := web.get(base + "/runs?environment=prod"); len(r.items()) != 0 {
		t.Errorf("environment filter: %s", r.raw)
	}
	if r := web.get(base + "/runs?sort=startedAt"); r.code != 400 || r.errCode() != "INVALID_SORT" {
		t.Errorf("unsupported sort: %d %s", r.code, r.raw)
	}
	if r := web.get(base + "/runs?cursor=bogus"); r.code != 400 || r.errCode() != "INVALID_CURSOR" {
		t.Errorf("bad cursor: %d %s", r.code, r.raw)
	}
	if r := web.get(base + "/runs/" + runIDs[0]); r.code != 200 || r.body["id"] != runIDs[0] || r.body["status"] != "failed" {
		t.Errorf("run detail: %d %s", r.code, r.raw)
	}

	// ---- Results and result detail ----
	rr := web.get(base + "/runs/" + runIDs[0] + "/results?limit=50")
	if rr.code != 200 || len(rr.items()) != 2 || rr.body["nextCursor"] != nil {
		t.Fatalf("results: %d %s", rr.code, rr.raw)
	}
	failed := rr.items()[0].(map[string]any)
	if failed["displayName"] != "declines expired card" || failed["finalStatus"] != "failed" || failed["filePath"] != "tests/checkout.spec.ts" || failed["attemptCount"] != float64(1) || failed["stableTestId"] == nil {
		t.Errorf("result DTO (failures first): %v", failed)
	}
	resultID := failed["id"].(string)
	det := web.get(base + "/results/" + resultID)
	if det.code != 200 {
		t.Fatalf("result detail: %d %s", det.code, det.raw)
	}
	attempts, _ := det.body["attempts"].([]any)
	events, _ := det.body["events"].([]any)
	if len(attempts) != 1 || attempts[0].(map[string]any)["error"].(map[string]any)["message"] != "expected 402" || len(events) != 0 {
		t.Errorf("detail attempts/events: %s", det.raw)
	}

	// ---- Artifacts on the failed attempt (§7.7 upload, §11.5 serving) ----
	// A screenshot (inline in the browser), a trace (always a download) and
	// one declared but never uploaded: only verified ones may be listed or
	// served. The reporter uses the memory store's presigned PUT like S3's.
	failedLocal := "0198c3e1-7b1a-7c3d-9e4f-000000000000"
	upload := func(name, kind, contentType string, data []byte, complete bool) string {
		sum := sha256.Sum256(data)
		decl := bearer.do("POST", "/v1/artifacts", map[string]any{
			"localRunId": failedLocal, "clientTestId": "ct_pay", "occurrenceIndex": 0, "attempt": 0,
			"name": name, "kind": kind, "contentType": contentType, "sizeBytes": len(data), "sha256": hex.EncodeToString(sum[:]),
		}, auth)
		if decl.code != 201 {
			t.Fatalf("declare %s: %d %s", name, decl.code, decl.raw)
		}
		id := decl.body["artifactId"].(string)
		if !complete {
			return id
		}
		up := decl.body["upload"].(map[string]any)
		req, _ := http.NewRequest("PUT", up["url"].(string), bytes.NewReader(data))
		for k, v := range up["headers"].(map[string]any) {
			req.Header.Set(k, fmt.Sprint(v))
		}
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", contentType)
		}
		put, err := http.DefaultClient.Do(req)
		if err != nil || put.StatusCode != 200 {
			t.Fatalf("PUT %s: %v %v", name, err, put)
		}
		put.Body.Close()
		if r := bearer.do("POST", "/v1/artifacts/"+id+"/complete", map[string]any{"sha256": hex.EncodeToString(sum[:])}, auth); r.code != 200 {
			t.Fatalf("complete %s: %d %s", name, r.code, r.raw)
		}
		return id
	}
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 300)...)
	shotID := upload(`shot "1".png`, "screenshot", "image/png", png, true)
	traceID := upload("trace.zip", "trace", "application/zip", bytes.Repeat([]byte("PK"), 200), true)
	pendingID := upload("never.txt", "log", "text/plain", []byte("nope"), false)
	det = web.get(base + "/results/" + resultID)
	arts, _ := det.body["attempts"].([]any)[0].(map[string]any)["artifacts"].([]any)
	if len(arts) != 2 || arts[0].(map[string]any)["id"] != shotID || arts[0].(map[string]any)["kind"] != "screenshot" || arts[0].(map[string]any)["sizeBytes"] != float64(len(png)) || arts[1].(map[string]any)["id"] != traceID {
		t.Errorf("attempt artifacts (verified only, oldest first): %s", det.raw)
	}
	content := func(id string) (*http.Response, []byte) {
		req, _ := http.NewRequest("GET", srv.URL+base+"/artifacts/"+id+"/content", nil)
		res, err := web.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, b
	}
	if res, body := content(shotID); res.StatusCode != 200 || !bytes.Equal(body, png) ||
		res.Header.Get("Content-Type") != "image/png" || res.Header.Get("X-Content-Type-Options") != "nosniff" ||
		res.Header.Get("Cache-Control") != "private, no-store" ||
		res.Header.Get("Content-Disposition") != `inline; filename="shot _1_.png"; filename*=UTF-8''shot%20%221%22.png` {
		t.Errorf("screenshot content: %d %v (%d bytes)", res.StatusCode, res.Header, len(body))
	}
	if res, _ := content(traceID); res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment; ") || res.Header.Get("Content-Type") != "application/zip" {
		t.Errorf("trace must download, never render: %d %v", res.StatusCode, res.Header)
	}
	if res, _ := content(pendingID); res.StatusCode != 404 {
		t.Errorf("unverified artifact served: %d", res.StatusCode)
	}

	// ---- Triage events: allowed types, dedupe, then visible in the detail ----
	if r := web.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}}); r.code != 201 || r.body["actorName"] != "alice@example.com" {
		t.Errorf("FAILURE_SEEN: %d %s", r.code, r.raw)
	}
	if r := web.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}}); r.code != 200 {
		t.Errorf("FAILURE_SEEN dedupe: %d %s", r.code, r.raw)
	}
	// §11.6 API3: the actor and the time are the session's and the server's;
	// whatever the body claims about them is ignored, not applied.
	if r := web.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "MARKED_FLAKY", "payload": map[string]any{}, "actorId": bob.ID, "actorName": "bob@example.com", "actorType": "SYSTEM", "occurredAt": "2000-01-01T00:00:00Z", "projectId": string(search.ID)}); r.code != 201 || r.body["actorName"] != "alice@example.com" || strings.HasPrefix(r.body["occurredAt"].(string), "2000") {
		t.Errorf("spoofed actor/time was applied: %d %s", r.code, r.raw)
	}
	if r := web.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "HYPOTHESIS_ACCEPTED", "payload": map[string]any{}}); r.code != 400 {
		t.Errorf("type outside the UI set: %d %s", r.code, r.raw)
	}
	det = web.get(base + "/results/" + resultID)
	events, _ = det.body["events"].([]any)
	if len(events) != 2 || events[0].(map[string]any)["type"] != "FAILURE_SEEN" || events[1].(map[string]any)["type"] != "MARKED_FLAKY" {
		t.Errorf("events after triage: %s", det.raw)
	}
	// POST without the CSRF header is refused even with a valid session.
	saved := web.csrf
	web.csrf = ""
	if r := web.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "RESOLVED", "payload": map[string]any{}}); r.code != 403 || r.errCode() != "CSRF" {
		t.Errorf("POST without CSRF: %d %s", r.code, r.raw)
	}
	web.csrf = saved
	if r := web.do("POST", base+"/results/"+resultID+"/triage-events", map[string]any{"type": "RESOLVED", "payload": map[string]any{}}, map[string]string{"Origin": "https://evil.example"}); r.code != 403 {
		t.Errorf("POST from a foreign origin: %d %s", r.code, r.raw)
	}

	// ---- Identities ----
	ids := web.get(base + "/identities?limit=20")
	if ids.code != 200 || len(ids.items()) != 2 {
		t.Fatalf("identities: %d %s", ids.code, ids.raw)
	}
	pay := ids.items()[0].(map[string]any)
	if pay["canonicalTitle"] != "declines expired card" || pay["canonicalFilePath"] != "tests/checkout.spec.ts" || pay["totalRuns"] != float64(2) || pay["flakyScore"] == nil {
		t.Errorf("identity DTO: %v", pay)
	}
	if statuses, _ := pay["recentStatuses"].([]any); len(statuses) != 2 || statuses[0] != "passed" || statuses[1] != "failed" {
		t.Errorf("recentStatuses: %v", pay["recentStatuses"])
	}
	if r := web.get(base + "/identities?view=flaky"); r.code != 200 {
		t.Errorf("view=flaky: %d", r.code)
	}
	payID := pay["id"].(string)
	if r := web.get(base + "/identities/" + payID); r.code != 200 || r.body["id"] != payID {
		t.Errorf("identity detail: %d %s", r.code, r.raw)
	}
	hist := web.get(base + "/identities/" + payID + "/runs?limit=20")
	if hist.code != 200 || len(hist.items()) != 2 || hist.items()[0].(map[string]any)["id"] != runIDs[1] {
		t.Errorf("identity runs: %d %s", hist.code, hist.raw)
	}

	// ---- Cross-tenant: bob (viewer of search) sees nothing of checkout ----
	bobc := newClient(t, srv)
	bobc.bootstrap()
	if r := bobc.post("/v1/session", map[string]string{"email": "bob@example.com", "password": "another long passphrase!"}); r.code != 200 {
		t.Fatalf("bob login: %d %s", r.code, r.raw)
	}
	bobc.csrf, _ = bobc.get("/v1/session").body["csrfToken"].(string)
	for _, path := range []string{base + "/runs", base + "/runs/" + runIDs[0], base + "/runs/" + runIDs[0] + "/results", base + "/results/" + resultID, base + "/identities", base + "/identities/" + payID, base + "/identities/" + payID + "/runs", base + "/artifacts/" + shotID + "/content"} {
		if r := bobc.get(path); r.code != 404 || r.errCode() != "PROJECT_NOT_FOUND" {
			t.Errorf("bob GET %s: %d %s", path, r.code, r.raw)
		}
	}
	if r := bobc.post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}}); r.code != 404 {
		t.Errorf("bob triage on checkout: %d %s", r.code, r.raw)
	}
	if r := bobc.get("/v1/projects"); len(r.items()) != 1 || r.items()[0].(map[string]any)["name"] != "search" || fmt.Sprint(r.items()[0].(map[string]any)["actions"]) != "[view]" {
		t.Errorf("bob's projects (viewer: actions = [view]): %s", r.raw)
	}
	// Viewer role: may see, may mark seen, may not triage (403, not 404).
	searchBase := "/v1/projects/" + string(search.ID)
	if r := bobc.get(searchBase + "/runs"); r.code != 200 || len(r.items()) != 0 {
		t.Errorf("bob's own project: %d %s", r.code, r.raw)
	}
	if r := bobc.post(searchBase+"/results/"+resultID+"/triage-events", map[string]any{"type": "MARKED_FLAKY", "payload": map[string]any{}}); r.code != 403 || r.errCode() != "FORBIDDEN" {
		t.Errorf("viewer triaging: %d %s", r.code, r.raw)
	}

	// ---- Identity operations (§9.4 over HTTP): role, aliases, merge, undo, split, review ----
	cartID := ids.items()[1].(map[string]any)["id"].(string)
	// A member may read the queue but not change identities (403 from the
	// role, not 404 from the tenant).
	if r := web.get(base + "/review"); r.code != 200 || len(r.items()) != 0 {
		t.Errorf("member reads the (empty) review queue: %d %s", r.code, r.raw)
	}
	if r := web.post(base+"/identities/"+cartID+"/merge", map[string]any{"into": payID}); r.code != 403 || r.errCode() != "FORBIDDEN" {
		t.Errorf("member merging: %d %s", r.code, r.raw)
	}
	// carol is a maintainer of checkout (§11.3.1): identity merge, tokens,
	// release decisions — but not project/member management.
	carol, _ := az.CreateUser(ctx, "carol@example.com")
	_ = az.SetPassword(ctx, carol.ID, "carol passphrase long enough", "")
	_ = az.VerifyEmail(ctx, carol.ID)
	_, _ = az.GrantRole(ctx, carol.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(checkout.ID)}, authz.RoleMaintainer)
	carolc := newClient(t, srv)
	carolc.bootstrap()
	if r := carolc.post("/v1/session", map[string]string{"email": "carol@example.com", "password": "carol passphrase long enough"}); r.code != 200 {
		t.Fatalf("carol login: %d %s", r.code, r.raw)
	}
	carolc.csrf, _ = carolc.get("/v1/session").body["csrfToken"].(string)
	if r := carolc.get("/v1/projects"); fmt.Sprint(r.items()[0].(map[string]any)["actions"]) != "[view triage identity.merge token.manage project.configure data.delete testmgmt.write release.decide]" {
		t.Errorf("maintainer actions: %s", r.raw)
	}
	// Aliases: one fingerprint in force per identity.
	al := carolc.get(base + "/identities/" + cartID + "/aliases")
	if al.code != 200 || len(al.items()) != 1 || al.items()[0].(map[string]any)["validTo"] != nil {
		t.Fatalf("aliases: %d %s", al.code, al.raw)
	}
	cartFingerprint := al.items()[0].(map[string]any)["fingerprint"].(string)
	// Merge cart into pay: cart disappears from the active list, pay owns two aliases.
	if r := carolc.post(base+"/identities/"+cartID+"/merge", map[string]any{"into": payID}); r.code != 200 || r.body["mergedInto"] != payID {
		t.Fatalf("merge: %d %s", r.code, r.raw)
	}
	if r := carolc.get(base + "/identities?limit=20"); len(r.items()) != 1 {
		t.Errorf("identities after merge: %s", r.raw)
	}
	if r := carolc.get(base + "/identities/" + payID + "/aliases"); len(r.items()) != 2 {
		t.Errorf("pay aliases after merge: %s", r.raw)
	}
	if r := carolc.post(base+"/identities/"+cartID+"/merge", map[string]any{"into": payID}); r.code != 409 || r.errCode() != "CONFLICT" {
		t.Errorf("merging a merged identity again: %d %s", r.code, r.raw)
	}
	// Undo puts it back exactly (N43: this is not a split).
	if r := carolc.post(base+"/identities/"+cartID+"/undo-merge", map[string]any{}); r.code != 200 {
		t.Fatalf("undo-merge: %d %s", r.code, r.raw)
	}
	if r := carolc.get(base + "/identities?limit=20"); len(r.items()) != 2 {
		t.Errorf("identities after undo: %s", r.raw)
	}
	if r := carolc.get(base + "/identities/" + cartID + "/aliases"); len(r.items()) != 1 {
		t.Errorf("cart aliases after undo: %s", r.raw)
	}
	// Split: the fingerprint must belong to the identity in the path.
	if r := carolc.post(base+"/identities/"+payID+"/split", map[string]any{"fingerprint": cartFingerprint, "at": time.Now().UTC().Format(time.RFC3339Nano)}); r.code != 400 {
		t.Errorf("split with a foreign fingerprint: %d %s", r.code, r.raw)
	}
	sp := carolc.post(base+"/identities/"+cartID+"/split", map[string]any{"fingerprint": cartFingerprint, "at": time.Now().UTC().Format(time.RFC3339Nano)})
	if sp.code != 201 || sp.body["newIdentityId"] == nil {
		t.Fatalf("split: %d %s", sp.code, sp.raw)
	}
	// The old identity keeps its history but no longer owns a fingerprint in
	// force, so it retires from the active list; the new one takes its place.
	newID := sp.body["newIdentityId"].(string)
	after := carolc.get(base + "/identities?limit=20")
	if len(after.items()) != 2 || after.items()[1].(map[string]any)["id"] != newID {
		t.Errorf("identities after split (pay + the new identity): %s", after.raw)
	}
	if r := carolc.get(base + "/identities/" + cartID); r.code != 200 {
		t.Errorf("retired identity still readable by id: %d %s", r.code, r.raw)
	}
	// Review queue: a seeded pending pair, decided "kept" once; a second
	// decision is a conflict; bob (another tenant) cannot see or decide it.
	var itemID string
	if err := db.Q(ctx).QueryRow(ctx, `
		INSERT INTO review_queue_items (project_id, identity_id, candidate_id, fingerprint, score, gap, breakdown, run_id)
		VALUES ($1, $2, $3, 'fp1:test', 0.83, 0.05, '{"title": 0.9}', $4) RETURNING id`, string(checkout.ID), cartID, payID, runIDs[0]).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	rq := carolc.get(base + "/review")
	if rq.code != 200 || len(rq.items()) != 1 {
		t.Fatalf("review queue: %d %s", rq.code, rq.raw)
	}
	item := rq.items()[0].(map[string]any)
	if item["id"] != itemID || item["identity"].(map[string]any)["canonicalFilePath"] != "tests/checkout.spec.ts" || item["candidate"].(map[string]any)["id"] != payID || item["score"] != 0.83 {
		t.Errorf("review item DTO: %v", item)
	}
	if r := web.post(base+"/review/"+itemID, map[string]any{"decision": "kept"}); r.code != 403 {
		t.Errorf("member deciding: %d %s", r.code, r.raw)
	}
	if r := carolc.post(base+"/review/"+itemID, map[string]any{"decision": "maybe"}); r.code != 400 {
		t.Errorf("bad decision: %d %s", r.code, r.raw)
	}
	if r := carolc.post(base+"/review/"+itemID, map[string]any{"decision": "kept"}); r.code != 200 || r.body["status"] != "kept" || r.body["decidedBy"] != "carol@example.com" {
		t.Errorf("decide kept: %d %s", r.code, r.raw)
	}
	if r := carolc.post(base+"/review/"+itemID, map[string]any{"decision": "kept"}); r.code != 409 {
		t.Errorf("deciding twice: %d %s", r.code, r.raw)
	}
	if r := carolc.get(base + "/review"); len(r.items()) != 0 {
		t.Errorf("queue after decision: %s", r.raw)
	}
	var audits int
	_ = db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM identity_audit WHERE project_id = $1`, string(checkout.ID)).Scan(&audits)
	if audits != 4 { // merge, undo_merge, split, review
		t.Errorf("identity_audit rows: %d, want 4", audits)
	}

	// ---- Test Management (P2, §6.8) ----
	// alice (member) writes; carol (admin) decides; bob (viewer of search)
	// reads his own project and is refused writes; checkout is 404 for him.
	reqR := web.post(base+"/requirements", map[string]any{"externalKey": "PAY-1", "title": "Card payments", "priority": "p0"})
	if reqR.code != 201 || reqR.body["coveredBy"] != 0.0 {
		t.Fatalf("create requirement: %d %s", reqR.code, reqR.raw)
	}
	reqID := reqR.body["id"].(string)
	caseR := web.post(base+"/test-cases", map[string]any{"title": "Pay with saved card", "isCritical": true, "steps": []map[string]string{{"action": "open cart", "expected": "cart shown"}}})
	if caseR.code != 201 || caseR.body["automationStatus"] != "manual_only" {
		t.Fatalf("create case: %d %s", caseR.code, caseR.raw)
	}
	caseID := caseR.body["id"].(string)
	if r := web.post(base+"/test-cases", map[string]any{"title": "x", "priority": "urgent"}); r.code != 400 {
		t.Errorf("bad priority: %d %s", r.code, r.raw)
	}
	// Links target TestIdentity.id only (§6.8.1): the retired one is a
	// conflict, an unknown id a 404, the active one links and flips status.
	if r := web.post(base+"/test-cases/"+caseID+"/links", map[string]any{"stableTestId": cartID}); r.code != 409 {
		t.Errorf("link to retired identity: %d %s", r.code, r.raw)
	}
	if r := web.post(base+"/test-cases/"+caseID+"/links", map[string]any{"stableTestId": "0198c3e1-7b1a-7c3d-9e4f-00000000dead"}); r.code != 404 {
		t.Errorf("link to unknown identity: %d %s", r.code, r.raw)
	}
	linkR := web.post(base+"/test-cases/"+caseID+"/links", map[string]any{"stableTestId": payID, "linkType": "exact"})
	if linkR.code != 201 || linkR.body["createdBy"] != "alice@example.com" {
		t.Fatalf("link: %d %s", linkR.code, linkR.raw)
	}
	if r := web.get(base + "/test-cases/" + caseID); r.body["automationStatus"] != "automated" || len(r.body["links"].([]any)) != 1 {
		t.Errorf("case after link: %s", r.raw)
	}
	if r := web.do("POST", base+"/test-cases/"+caseID+"/requirements/"+reqID, map[string]any{}, nil); r.code != 204 {
		t.Errorf("cover: %d %s", r.code, r.raw)
	}
	if r := web.get(base + "/requirements"); len(r.items()) != 1 || r.items()[0].(map[string]any)["coveredBy"] != 1.0 {
		t.Errorf("requirement coverage: %s", r.raw)
	}
	if r := web.do("DELETE", base+"/test-cases/"+caseID+"/requirements/"+reqID, nil, nil); r.code != 204 {
		t.Errorf("uncover: %d %s", r.code, r.raw)
	}
	if r := web.do("DELETE", base+"/test-cases/"+caseID+"/requirements/"+reqID, nil, nil); r.code != 404 {
		t.Errorf("uncover twice: %d %s", r.code, r.raw)
	}
	if r := web.do("POST", base+"/test-cases/"+caseID+"/requirements/"+reqID, map[string]any{}, nil); r.code != 204 {
		t.Errorf("re-cover: %d %s", r.code, r.raw)
	}
	imp := web.do("POST", base+"/test-cases/import", map[string]any{"csv": "key,title,suite,priority\nTC-1,Apply discount,Checkout,p2\nTC-2,Refund,Checkout,p1"}, nil)
	if imp.code != 200 || imp.body["created"] != 2.0 || imp.body["suites"] != 1.0 {
		t.Errorf("import: %d %s", imp.code, imp.raw)
	}
	if r := web.get(base + "/test-cases?q=refund"); len(r.items()) != 1 {
		t.Errorf("case search: %s", r.raw)
	}
	planR := web.post(base+"/test-plans", map[string]any{"name": "Sprint 12"})
	if planR.code != 201 {
		t.Fatalf("plan: %d %s", planR.code, planR.raw)
	}
	planID := planR.body["id"].(string)
	execR := web.post(base+"/test-plans/"+planID+"/executions", map[string]any{"testCaseId": caseID, "status": "passed", "note": "ok on staging"})
	if execR.code != 201 || execR.body["executedBy"] != "alice@example.com" || len(execR.body["evidence"].([]any)) != 0 {
		t.Errorf("execution: %d %s", execR.code, execR.raw)
	}
	execID := execR.body["id"].(string)
	if r := web.get(base + "/test-plans/" + planID); r.body["status"] != "active" || len(r.body["executions"].([]any)) != 1 {
		t.Errorf("plan after execution: %s", r.raw)
	}
	// Evidence (§6.8.2): a member attaches a screenshot to the execution; it
	// is an Artifact, listed on the plan and served by the content route with
	// the §11.5 headers. The wrong bytes, a viewer and a closed plan are refused.
	shot := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 100)...)
	evPath := base + "/test-plans/" + planID + "/executions/" + execID + "/evidence"
	evidence := web.upload(evPath, "file", "checkout.png", "image/png", shot)
	if evidence.code != 201 || evidence.body["kind"] != "manual_evidence" || evidence.body["contentType"] != "image/png" || evidence.body["sizeBytes"] != float64(len(shot)) || evidence.body["name"] != "checkout.png" {
		t.Fatalf("evidence: %d %s", evidence.code, evidence.raw)
	}
	if r := web.upload(evPath, "file", "page.png", "image/png", []byte("<html><script>1</script></html>")); r.code != 400 || r.errCode() != "INVALID_PAYLOAD" {
		t.Errorf("html as png: %d %s", r.code, r.raw)
	}
	if r := web.upload(evPath, "notes", "n.txt", "text/plain", []byte("no file part")); r.code != 400 {
		t.Errorf("missing file part: %d %s", r.code, r.raw)
	}
	if r := web.post(evPath, map[string]any{}); r.code != 400 {
		t.Errorf("json instead of multipart: %d %s", r.code, r.raw)
	}
	if r := bobc.upload(evPath, "file", "b.png", "image/png", shot); r.code != 404 {
		t.Errorf("bob attaching to checkout's plan: %d", r.code)
	}
	if r := web.get(base + "/test-plans/" + planID); r.code != 200 {
		t.Errorf("plan with evidence: %d", r.code)
	} else {
		evs := r.body["executions"].([]any)[0].(map[string]any)["evidence"].([]any)
		if len(evs) != 1 || evs[0].(map[string]any)["id"] != evidence.body["id"] {
			t.Errorf("plan evidence: %s", r.raw)
		}
	}
	{
		req, _ := http.NewRequest("GET", srv.URL+base+"/artifacts/"+evidence.body["id"].(string)+"/content", nil)
		res, err := web.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !bytes.Equal(got, shot) || res.Header.Get("Content-Type") != "image/png" ||
			!strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline;") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("evidence content: %d %v", res.StatusCode, res.Header)
		}
		if r := bobc.get(base + "/artifacts/" + evidence.body["id"].(string) + "/content"); r.code != 404 {
			t.Errorf("bob reading checkout's evidence: %d", r.code)
		}
	}
	if r := web.do("PATCH", base+"/test-plans/"+planID, map[string]any{"status": "closed"}, nil); r.code != 200 {
		t.Fatalf("close plan: %d %s", r.code, r.raw)
	}
	if r := web.upload(evPath, "file", "late.png", "image/png", shot); r.code != 409 {
		t.Errorf("evidence on a closed plan: %d %s", r.code, r.raw)
	}
	if r := web.do("PATCH", base+"/test-plans/"+planID, map[string]any{"status": "active"}, nil); r.code != 200 {
		t.Fatalf("reopen plan: %d %s", r.code, r.raw)
	}
	relR := web.post(base+"/releases", map[string]any{"version": "2026.09.21"})
	if relR.code != 201 || relR.body["status"] != "open" || relR.body["gate"] != nil {
		t.Fatalf("release: %d %s", relR.code, relR.raw)
	}
	relID := relR.body["id"].(string)
	if r := web.post(base+"/releases", map[string]any{"version": "2026.09.21"}); r.code != 409 {
		t.Errorf("duplicate version: %d %s", r.code, r.raw)
	}
	evr := web.post(base+"/releases/"+relID+"/evaluate", map[string]any{})
	gate, _ := evr.body["gate"].(map[string]any)
	if evr.code != 200 || gate == nil || gate["verdict"] == "no_evidence" || len(gate["rules"].([]any)) != 5 || evr.body["status"] != "open" {
		t.Fatalf("evaluate: %d %s", evr.code, evr.raw)
	}
	if r := web.post(base+"/releases/"+relID+"/decision", map[string]any{"status": "approved"}); r.code != 403 {
		t.Errorf("member deciding a release: %d %s", r.code, r.raw)
	}
	dec := carolc.post(base+"/releases/"+relID+"/decision", map[string]any{"status": "approved", "note": "go"})
	if dec.code != 200 || dec.body["decidedBy"] != "carol@example.com" || dec.body["status"] != "approved" || dec.body["gate"] == nil {
		t.Errorf("decision: %d %s", dec.code, dec.raw)
	}
	if r := bobc.get(base + "/releases/" + relID); r.code != 404 {
		t.Errorf("bob reading checkout's release: %d", r.code)
	}
	if r := bobc.get("/v1/projects/" + string(search.ID) + "/test-cases"); r.code != 200 || len(r.items()) != 0 {
		t.Errorf("viewer listing his own project's cases: %d %s", r.code, r.raw)
	}
	if r := bobc.post("/v1/projects/"+string(search.ID)+"/test-cases", map[string]any{"title": "nope"}); r.code != 403 {
		t.Errorf("viewer writing a case: %d %s", r.code, r.raw)
	}
	// CI reads the gate with a project:read token (§6.8.4 #2); the ingest
	// token, a foreign token and no token are all refused.
	readTok, _ := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(checkout.ID)}, Permissions: []authz.Permission{authz.PermProjectRead}, Name: "deploy"})
	ci := newClient(t, srv)
	if r := ci.do("GET", "/v1/releases/"+relID+"/gate", nil, map[string]string{"Authorization": "Bearer " + readTok.Plaintext}); r.code != 200 || r.body["gate"].(map[string]any)["verdict"] != gate["verdict"] || r.body["status"] != "approved" {
		t.Errorf("CI gate: %d %s", r.code, r.raw)
	}
	if r := ci.do("GET", "/v1/releases/"+relID+"/gate", nil, map[string]string{"Authorization": "Bearer " + tok.Plaintext}); r.code != 404 {
		t.Errorf("CI gate with ingest-only token: %d %s", r.code, r.raw)
	}
	if r := ci.do("GET", "/v1/releases/"+relID+"/gate", nil, nil); r.code != 401 {
		t.Errorf("CI gate without token: %d", r.code)
	}

	// ---- Logout ----
	if r := web.post("/v1/session/logout", map[string]any{}); r.code != 204 {
		t.Errorf("logout: %d %s", r.code, r.raw)
	}
	if r := web.get("/v1/projects"); r.code != 401 {
		t.Errorf("projects after logout: %d", r.code)
	}
	if r := web.get("/v1/session"); r.code != 200 || r.body["user"] != nil {
		t.Errorf("bootstrap after logout: %d %s", r.code, r.raw)
	}
	if !strings.Contains(reg.Expose(), `route="GET /v1/projects/{projectId}/runs"`) {
		t.Error("route-pattern metrics lost behind the session middleware")
	}
}
