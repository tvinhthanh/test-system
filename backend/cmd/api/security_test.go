package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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

// §11.5 at the application layer and §11.6 API4/API9, on the served
// handler: response headers, CORS only for listed origins, a per-user
// dashboard rate limit, and the route inventory pinned to a file so a route
// cannot appear or vanish without a diff in review.

func newSecurityServer(t *testing.T, ratePerMinute int) (*httptest.Server, *Harness2) {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false, DashboardRatePerMinute: ratePerMinute}
	handler, router := buildHandler(cfg, db, store, metric.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	ctx := context.Background()
	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	org, _ := ten.CreateOrganization(ctx, "Sec", "sec")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "main")
	p, _ := ten.CreateProject(ctx, ws.ID, "app", "")
	u, _ := az.CreateUser(ctx, "sec@example.com")
	_ = az.SetPassword(ctx, u.ID, "a passphrase long enough", "")
	_ = az.VerifyEmail(ctx, u.ID)
	_, _ = az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(p.ID)}, authz.RoleMember)
	return srv, &Harness2{project: string(p.ID), user: u.ID, patterns: router.Patterns()}
}

type Harness2 struct {
	project, user string
	patterns      []string
}

func login(t *testing.T, srv *httptest.Server) *client {
	t.Helper()
	c := newClient(t, srv)
	c.bootstrap()
	if r := c.post("/v1/session", map[string]string{"email": "sec@example.com", "password": "a passphrase long enough"}); r.code != 200 {
		t.Fatalf("login: %d %s", r.code, r.raw)
	}
	c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
	return c
}

func TestBrowserPolicyHeadersAndCORS(t *testing.T) {
	srv, _ := newSecurityServer(t, 0)

	res, err := http.Get(srv.URL + "/v1/session")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "Cache-Control": "no-store", "Referrer-Policy": "no-referrer", "X-Frame-Options": "DENY"} {
		if got := res.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if res.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS sent over plain HTTP in dev — it would poison localhost")
	}

	preflight := func(from string) *http.Response {
		req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/v1/session", nil)
		req.Header.Set("Origin", from)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type, x-csrf-token")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	ok := preflight(origin)
	if ok.StatusCode != 204 || ok.Header.Get("Access-Control-Allow-Origin") != origin || ok.Header.Get("Access-Control-Allow-Credentials") != "true" ||
		!strings.Contains(ok.Header.Get("Access-Control-Allow-Headers"), "X-CSRF-Token") {
		t.Errorf("preflight from the dashboard origin: %d %v", ok.StatusCode, ok.Header)
	}
	bad := preflight("https://evil.example")
	if bad.StatusCode != 403 || bad.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("preflight from a foreign origin: %d ACAO=%q", bad.StatusCode, bad.Header.Get("Access-Control-Allow-Origin"))
	}
	// A plain request with a foreign Origin gets no CORS grant either.
	req, _ := http.NewRequest("GET", srv.URL+"/v1/session", nil)
	req.Header.Set("Origin", "https://evil.example")
	plain, _ := http.DefaultClient.Do(req)
	plain.Body.Close()
	if plain.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("foreign origin was granted CORS on a plain request")
	}
	if strings.ToLower(plain.Header.Get("Access-Control-Allow-Origin")) == "*" {
		t.Error("wildcard CORS on a credentialed API")
	}
}

func TestDashboardRateLimitPerUser(t *testing.T) {
	// 6/min → burst 12: the 13th rapid request is refused with a Retry-After.
	srv, h := newSecurityServer(t, 6)
	c := login(t, srv)
	base := "/v1/projects/" + h.project
	codes := map[int]int{}
	var retryAfter string
	for i := 0; i < 20; i++ {
		r := c.get(base + "/runs")
		codes[r.code]++
		if r.code == 429 {
			retryAfter = "seen"
		}
	}
	if codes[200] < 10 || codes[429] < 5 || codes[200]+codes[429] != 20 {
		t.Errorf("codes: %v — want ~12 × 200 then 429s", codes)
	}
	if retryAfter == "" {
		t.Error("no 429 observed")
	}
	// The limit is per user: the session routes and an anonymous caller are
	// not keyed and keep answering.
	if r := c.get("/v1/session"); r.code != 200 {
		t.Errorf("session bootstrap limited: %d", r.code)
	}
	if r := newClient(t, srv).get(base + "/runs"); r.code != 401 {
		t.Errorf("anonymous caller: %d, want 401 (not limited, not served)", r.code)
	}
}

// §11.6 API9: the mounted routes are the inventory. deploy/routes.txt is
// the checked-in list; adding or removing a route without updating it
// fails here, so the diff is in the review.
func TestRouteInventoryMatchesFile(t *testing.T) {
	_, h := newSecurityServer(t, 0)
	got := append([]string(nil), h.patterns...)
	sort.Strings(got)
	path := filepath.Join("..", "..", "deploy", "routes.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var want []string
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		want = append(want, line)
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("mounted routes differ from deploy/routes.txt\n--- mounted ---\n%s\n--- file ---\n%s\nUpdate the file with the routes you meant to add or remove.", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, p := range got {
		_, path, _ := strings.Cut(p, " ")
		if path != "/healthz" && !strings.HasPrefix(path, "/v1/") {
			t.Errorf("route outside /v1/: %s (§11.6 API9: only /v1/)", p)
		}
	}
	t.Logf("%d routes mounted, %d in the file", len(got), len(want))
}
