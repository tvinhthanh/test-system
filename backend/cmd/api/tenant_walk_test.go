package main

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
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

// §14.3, release gate: "user A calls every endpoint with a resource of org B
// → 100% 403/404". This walks the patterns the served Router actually has,
// so a route added later is either classified here or fails the build of
// the gate — a sampled list would have let it through silently.
//
// Three foreign principals per resource route:
//   - a member of org B on the dashboard, naming org A's ids (wrong project);
//   - the same member naming their own project but org A's sub-resource ids
//     (right project, foreign run/result/identity — the classic IDOR);
//   - org B's ingest token naming org A's localRunId / artifactId.
//
// And principal-kind confusion: a session cookie on the reporter routes and
// a bearer token on the dashboard routes are refused outright.
func TestEveryRouteRefusesCrossTenant(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	store := memory.New()
	t.Cleanup(store.Close)
	cfg := config.Config{Env: "dev", TokenPepper: "test-pepper-test-pepper-test-pepper!", DashboardOrigins: []string{origin}, CookieSecure: false}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, router := buildHandler(cfg, db, store, metric.New(), logger)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	ten, az := tenancy.New(db), authz.New(db, []byte(cfg.TokenPepper))
	type org struct {
		project tenancy.ProjectID
		token   string
		user    *client
	}
	seed := func(slug, email, password string) org {
		o, err := ten.CreateOrganization(ctx, strings.ToUpper(slug), slug)
		if err != nil {
			t.Fatal(err)
		}
		ws, _ := ten.CreateWorkspace(ctx, o.ID, "main")
		p, _ := ten.CreateProject(ctx, ws.ID, slug+"-app", "")
		u, _ := az.CreateUser(ctx, email)
		_ = az.SetPassword(ctx, u.ID, password, "")
		_ = az.VerifyEmail(ctx, u.ID)
		// Member, not viewer: a 403 must come from the tenant boundary, never from the role.
		_, _ = az.GrantRole(ctx, u.ID, authz.ScopeRef{Level: authz.LevelProject, ID: string(p.ID)}, authz.RoleMember)
		tok, _ := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(p.ID)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "ci"})
		c := newClient(t, srv)
		c.bootstrap()
		if r := c.post("/v1/session", map[string]string{"email": email, "password": password}); r.code != 200 {
			t.Fatalf("login %s: %d %s", email, r.code, r.raw)
		}
		c.csrf, _ = c.get("/v1/session").body["csrfToken"].(string)
		return org{project: p.ID, token: tok.Plaintext, user: c}
	}
	a := seed("alpha", "a@alpha.example", "alpha passphrase long enough")
	b := seed("beta", "b@beta.example", "beta passphrase long enough")

	// Org A owns one run with a failure, one declared artifact, one identity.
	res := results.New(db)
	jobs := ingest.NewJobs(ingest.Deps{DB: db, Queue: queue.New(db), Tenancy: ten, Authz: az, Results: res, Identity: identity.New(db), Metrics: metric.New(), Logger: logger})
	worker := jobs.Worker(1)
	bearerA := map[string]string{"Authorization": "Bearer " + a.token}
	rep := newClient(t, srv)
	const localRunA = "0198c3e1-7b1a-7c3d-9e4f-00000000aaaa"
	event := map[string]any{
		"sequence": 1, "type": "test.finished", "clientTestId": "ct_pay", "occurrenceIndex": 0, "attempt": 0,
		"identity": map[string]any{"framework": "playwright", "filePath": "tests/pay.spec.ts", "titlePath": []string{"Pay", "declines"}, "paramSignature": ""},
		"status":   "failed", "outcome": "unexpected", "durationMs": 10,
		"error": map[string]any{"message": "boom", "stack": "at pay.spec.ts:1"},
	}
	r := rep.do("POST", "/v1/runs", map[string]any{"localRunId": localRunA, "environmentKey": "ci", "trigger": map[string]any{"ref": "refs/heads/main", "commitSha": "a1"}, "startedAt": time.Now().UTC().Format(time.RFC3339)}, bearerA)
	if r.code != 201 {
		t.Fatalf("run A: %d %s", r.code, r.raw)
	}
	runA := r.body["runId"].(string)
	if r := rep.do("POST", "/v1/runs/"+localRunA+"/events", map[string]any{"events": []any{event}}, bearerA); r.code != 200 {
		t.Fatalf("events A: %d %s", r.code, r.raw)
	}
	declare := map[string]any{"localRunId": localRunA, "clientTestId": "ct_pay", "occurrenceIndex": 0, "attempt": 0, "name": "shot.png", "kind": "screenshot", "contentType": "image/png", "sizeBytes": 1024, "sha256": strings.Repeat("ab", 32)}
	r = rep.do("POST", "/v1/artifacts", declare, bearerA)
	if r.code != 201 {
		t.Fatalf("declare A: %d %s", r.code, r.raw)
	}
	artifactA := r.body["artifactId"].(string)
	if r := rep.do("POST", "/v1/runs/"+localRunA+"/finish", map[string]any{}, bearerA); r.code != 200 {
		t.Fatalf("finish A: %d %s", r.code, r.raw)
	}
	if _, err := worker.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	baseA := "/v1/projects/" + string(a.project)
	resultA := a.user.get(baseA + "/runs/" + runA + "/results").items()[0].(map[string]any)["id"].(string)
	identityA := a.user.get(baseA + "/identities").items()[0].(map[string]any)["id"].(string)
	// A pending review item of org A (the resolver only queues one for an
	// ambiguous rename, so it is seeded directly; the walk needs its id, not
	// its provenance).
	var itemA string
	if err := db.Q(ctx).QueryRow(ctx, `
		INSERT INTO review_queue_items (project_id, identity_id, candidate_id, fingerprint, score, gap, breakdown, run_id)
		VALUES ($1, $2, $2, 'fp1:walk', 0.8, 0.1, '{}', $3) RETURNING id`, string(a.project), identityA, runA).Scan(&itemA); err != nil {
		t.Fatal(err)
	}

	// Org A's Test Management rows (P2): a requirement, a case linked to
	// A's identity, a plan, a release — created through the api as A.
	reqA := a.user.post(baseA+"/requirements", map[string]any{"title": "Card payments", "priority": "p0"}).body["id"].(string)
	caseA := a.user.post(baseA+"/test-cases", map[string]any{"title": "Pay with saved card", "isCritical": true}).body["id"].(string)
	linkA := a.user.post(baseA+"/test-cases/"+caseA+"/links", map[string]any{"stableTestId": identityA}).body["id"].(string)
	planA := a.user.post(baseA+"/test-plans", map[string]any{"name": "Sprint 12"}).body["id"].(string)
	releaseA := a.user.post(baseA+"/releases", map[string]any{"version": "2026.09.21"}).body["id"].(string)
	if reqA == "" || caseA == "" || linkA == "" || planA == "" || releaseA == "" {
		t.Fatalf("seeding A's test management rows failed: %q %q %q %q %q", reqA, caseA, linkA, planA, releaseA)
	}

	// Org A's ids by path parameter name. A parameter this map does not
	// know is a route the walk cannot exercise — fail loudly, do not skip.
	foreign := map[string]string{
		"projectId": string(a.project), "runId": runA, "resultId": resultA, "identityId": identityA,
		"localRunId": localRunA, "artifactId": artifactA, "itemId": itemA,
		"requirementId": reqA, "testCaseId": caseA, "linkId": linkA, "planId": planA, "releaseId": releaseA,
	}
	// Bodies that pass validation, so a 400 can never stand in for the tenant check.
	bodies := map[string]any{
		"POST /v1/projects/{projectId}/results/{resultId}/triage-events":   map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}},
		"POST /v1/runs/{localRunId}/events":                                map[string]any{"events": []any{event}},
		"POST /v1/runs/{localRunId}/finish":                                map[string]any{},
		"POST /v1/artifacts/{artifactId}/complete":                         map[string]any{"sha256": strings.Repeat("ab", 32)},
		"POST /v1/artifacts":                                               declare,
		"POST /v1/projects/{projectId}/review/{itemId}":                    map[string]any{"decision": "kept"},
		"POST /v1/projects/{projectId}/identities/{identityId}/merge":      map[string]any{"into": identityA},
		"POST /v1/projects/{projectId}/identities/{identityId}/undo-merge": map[string]any{},
		"POST /v1/projects/{projectId}/identities/{identityId}/split":      map[string]any{"fingerprint": "fp1:walk", "at": time.Now().UTC().Format(time.RFC3339)},
		// Test Management (P2). Valid bodies, so only the tenant check can refuse.
		"POST /v1/projects/{projectId}/suites":                         map[string]any{"name": "Walk"},
		"POST /v1/projects/{projectId}/requirements":                   map[string]any{"title": "Walk"},
		"PATCH /v1/projects/{projectId}/requirements/{requirementId}":  map[string]any{"title": "Walk"},
		"POST /v1/projects/{projectId}/test-cases":                     map[string]any{"title": "Walk"},
		"POST /v1/projects/{projectId}/test-cases/import":              map[string]any{"csv": "title\nWalk"},
		"PATCH /v1/projects/{projectId}/test-cases/{testCaseId}":       map[string]any{"title": "Walk"},
		"POST /v1/projects/{projectId}/test-cases/{testCaseId}/links":  map[string]any{"stableTestId": identityA},
		"POST /v1/projects/{projectId}/test-plans":                     map[string]any{"name": "Walk"},
		"PATCH /v1/projects/{projectId}/test-plans/{planId}":           map[string]any{"status": "active"},
		"POST /v1/projects/{projectId}/test-plans/{planId}/executions": map[string]any{"testCaseId": caseA, "status": "passed"},
		"POST /v1/projects/{projectId}/releases":                       map[string]any{"version": "walk"},
		"POST /v1/projects/{projectId}/releases/{releaseId}/evaluate":  map[string]any{},
		"POST /v1/projects/{projectId}/releases/{releaseId}/decision":  map[string]any{"status": "approved"},
	}
	// Routes with no tenant resource in the path — each one's tenant
	// property is asserted separately below, none is simply skipped.
	exempt := map[string]bool{
		"GET /healthz": true, "GET /v1/session": true, "POST /v1/session": true, "POST /v1/session/logout": true,
		"GET /v1/projects": true, "POST /v1/runs": true, "POST /v1/artifacts": true,
	}
	param := regexp.MustCompile(`\{([a-zA-Z]+)\}`)
	fill := func(path string, ids map[string]string) string {
		return param.ReplaceAllStringFunc(path, func(m string) string {
			name := m[1 : len(m)-1]
			v, ok := ids[name]
			if !ok {
				t.Fatalf("no cross-tenant fixture for path parameter %q in %s — extend the walk", name, path)
			}
			return v
		})
	}
	refused := func(what string, r resp) {
		t.Helper()
		if r.code != 403 && r.code != 404 {
			t.Errorf("%s: got %d, want 403/404 — %s", what, r.code, r.raw)
		}
	}

	patterns := router.Patterns()
	if len(patterns) < 15 {
		t.Fatalf("router recorded only %d patterns: %v", len(patterns), patterns)
	}
	walked := 0
	for _, pat := range patterns {
		method, path, _ := strings.Cut(pat, " ")
		body := bodies[pat]
		if method == "GET" {
			body = nil
		}
		switch {
		case exempt[pat]:
			continue
		case strings.HasPrefix(path, "/v1/projects/"):
			walked++
			// Wrong project: every id belongs to org A.
			refused("B member → "+pat, b.user.do(method, fill(path, foreign), body, nil))
			// Right project, foreign sub-resource: only projectId is B's. A
			// path whose only parameter is the project is B's own list — legit.
			if len(param.FindAllString(path, -1)) > 1 {
				mixed := map[string]string{}
				for k, v := range foreign {
					mixed[k] = v
				}
				mixed["projectId"] = string(b.project)
				refused("B member (own project, A's ids) → "+pat, b.user.do(method, fill(path, mixed), body, nil))
			}
			// A bearer token is not a dashboard principal.
			if r := rep.do(method, fill(path, foreign), body, bearerA); r.code != 401 {
				t.Errorf("bearer token on %s: %d %s", pat, r.code, r.raw)
			}
		case strings.HasPrefix(path, "/v1/releases/"):
			// CI gate (§6.8.4 #2): a bearer route whose URL names no project.
			walked++
			refused("B token → "+pat, rep.do(method, fill(path, foreign), nil, map[string]string{"Authorization": "Bearer " + b.token}))
			// A's own ingest token lacks project:read — the permission
			// boundary answers like the tenant boundary.
			refused("A ingest token → "+pat, rep.do(method, fill(path, foreign), nil, bearerA))
			if r := a.user.do(method, fill(path, foreign), nil, nil); r.code != 401 {
				t.Errorf("session cookie on %s: %d %s", pat, r.code, r.raw)
			}
		case strings.HasPrefix(path, "/v1/runs/") || strings.HasPrefix(path, "/v1/artifacts/"):
			walked++
			if body == nil {
				t.Fatalf("no request body fixture for %s — extend the walk", pat)
			}
			refused("B token → "+pat, rep.do(method, fill(path, foreign), body, map[string]string{"Authorization": "Bearer " + b.token}))
			// A session cookie is not a reporter principal.
			if r := a.user.do(method, fill(path, foreign), body, nil); r.code != 401 {
				t.Errorf("session cookie on %s: %d %s", pat, r.code, r.raw)
			}
		default:
			t.Fatalf("unclassified route %q — add it to the cross-tenant walk (§14.3)", pat)
		}
	}
	if walked < 36 {
		t.Errorf("walked only %d resource routes: %v", walked, patterns)
	}

	// The exempt routes' tenant properties.
	if r := b.user.get("/v1/projects"); len(r.items()) != 1 || r.items()[0].(map[string]any)["id"] != string(b.project) {
		t.Errorf("B's project list leaks: %s", r.raw)
	}
	// Declaring against A's localRunId with B's token: B has no such run.
	refused("B token → POST /v1/artifacts (A's localRunId)", rep.do("POST", "/v1/artifacts", declare, map[string]string{"Authorization": "Bearer " + b.token}))
	// (project, localRunId) is unique per project, not global: B reusing
	// A's localRunId gets its own run, never A's.
	r = rep.do("POST", "/v1/runs", map[string]any{"localRunId": localRunA, "environmentKey": "ci", "trigger": map[string]any{"ref": "refs/heads/main", "commitSha": "b1"}, "startedAt": time.Now().UTC().Format(time.RFC3339)}, map[string]string{"Authorization": "Bearer " + b.token})
	if r.code != 201 || r.body["runId"] == runA {
		t.Errorf("B run with A's localRunId: %d %s", r.code, r.raw)
	}
	if r := a.user.get(baseA + "/runs"); len(r.items()) != 1 {
		t.Errorf("A sees %d runs after B reused the localRunId: %s", len(r.items()), r.raw)
	}
	// Session routes carry no tenant resource. Logout without a session
	// cookie is an idempotent 204; it must not touch the bearer token.
	if r := rep.do("POST", "/v1/session/logout", map[string]any{}, bearerA); r.code != 204 {
		t.Errorf("logout with a bearer token: %d %s", r.code, r.raw)
	}
	if r := rep.do("POST", "/v1/runs/"+localRunA+"/finish", map[string]any{}, bearerA); r.code != 200 {
		t.Errorf("token A after a stray logout: %d %s", r.code, r.raw)
	}
	t.Logf("walked %d of %d patterns with three foreign principals each", walked, len(patterns))
}
