package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/billing"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/ingest"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/mail"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/testmgmt"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
)

// The dashboard's HTTP surface is frontend/API_CONTRACT.md, implemented
// here in cmd/api on purpose: it is coordination across query, tenancy,
// authz and triage (§6.9.3 rule 3), and query itself may import neither
// authz nor triage. Every project handler does the same three things —
// session principal, ProjectScope, Authorize — before touching the read
// model, so no endpoint can forget the tenant check (§11.3.3).
//
//	GET  /v1/projects?limit&cursor
//	GET  /v1/projects/{projectId}/runs?limit&cursor&q&status&environment&sort
//	GET  /v1/projects/{projectId}/runs/{runId}
//	GET  /v1/projects/{projectId}/runs/{runId}/results?limit&cursor
//	GET  /v1/projects/{projectId}/results/{resultId}
//	POST /v1/projects/{projectId}/results/{resultId}/triage-events   {type, payload}
//	GET  /v1/projects/{projectId}/identities?view&limit&cursor
//	GET  /v1/projects/{projectId}/identities/{identityId}
//	GET  /v1/projects/{projectId}/identities/{identityId}/runs?limit&cursor
//
// Lists answer {items: [...], nextCursor: null | "..."}. Not in the
// contract but kept for the operator: GET …/metrics/triage and …/metrics/trend.

type dashboard struct {
	tenancy  tenancy.Service
	authz    authz.Service
	query    query.Service
	triage   triage.Service
	idents   identity.Service
	tm       testmgmt.Service
	artifact artifact.Service
	store    objstore.Store
	logger   *slog.Logger

	ratePerMinute int // per signed-in user; 0 = no limit
	// mailer sends invitations (MAIL=smtp); nil = mail off. publicURL builds
	// the links.
	mailer    mail.Sender
	publicURL string
	// imports runs JUnit backfill through the ingest pipeline (imports.go).
	imports ingest.Deps
	// meter reads the §10.6 shadow meter (meter.go).
	meter billing.Service
}

// routes mounts the dashboard behind a per-user rate limit (§11.6 API4):
// the key is the session user, so one runaway tab cannot starve the api,
// and an anonymous caller (no session) is keyed by nothing and passes on to
// the 401 the handlers answer.
func (d *dashboard) routes(mux httpx.Mux) {
	limit := func(next http.Handler) http.Handler { return next }
	if d.ratePerMinute > 0 {
		limit = httpx.RateLimit(d.ratePerMinute, 2*d.ratePerMinute, func(r *http.Request) string {
			if p, ok := authz.PrincipalFrom(r.Context()); ok && p.Kind == authz.PrincipalUser {
				return "user:" + p.UserID
			}
			return ""
		})
	}
	handle := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, limit(fn)) }
	handle("GET /v1/projects", d.projects)
	handle("GET /v1/projects/{projectId}/runs", d.runs)
	handle("GET /v1/projects/{projectId}/runs/{runId}", d.run)
	handle("GET /v1/projects/{projectId}/runs/{runId}/results", d.results)
	handle("GET /v1/projects/{projectId}/results/{resultId}", d.resultDetail)
	handle("POST /v1/projects/{projectId}/results/{resultId}/triage-events", d.triageEvent)
	handle("GET /v1/projects/{projectId}/artifacts/{artifactId}/content", d.artifactContent)
	handle("GET /v1/projects/{projectId}/identities", d.identities)
	handle("GET /v1/projects/{projectId}/identities/{identityId}", d.identity)
	handle("GET /v1/projects/{projectId}/identities/{identityId}/runs", d.identityRuns)
	handle("GET /v1/projects/{projectId}/metrics/triage", d.triageMetrics)
	handle("GET /v1/projects/{projectId}/metrics/trend", d.trend)
	d.identityOpsRoutes(handle)
	d.testmgmtRoutes(handle)
	d.memberRoutes(handle)
	d.invitationRoutes(handle)
	d.importRoutes(handle)
	d.roiRoutes(handle)
	d.meterRoutes(handle)
	d.tokenRoutes(handle)
	d.projectCreateRoutes(handle)
	// CI reads a release gate with an ingest-style bearer token (project:read).
	mux.Handle("GET /v1/releases/{releaseId}/gate", authz.Middleware(d.authz)(http.HandlerFunc(d.gateForCI)))
}

type page struct {
	Items      any     `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

func envelope[T any](p query.Page[T]) page {
	out := page{Items: p.Items}
	if p.NextCursor != "" {
		c := p.NextCursor
		out.NextCursor = &c
	}
	return out
}

// user requires a logged-in human.
func (d *dashboard) user(w http.ResponseWriter, r *http.Request) (authz.Principal, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok || p.Kind != authz.PrincipalUser {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "log in first", nil)
		return authz.Principal{}, false
	}
	return p, true
}

// project resolves {projectId} and authorizes action on it: 404 when the
// user has no standing in the tenant, 403 when the role is too low.
func (d *dashboard) project(w http.ResponseWriter, r *http.Request, p authz.Principal, action authz.Action) (tenancy.Scope, bool) {
	id := tenancy.ProjectID(r.PathValue("projectId"))
	scope, err := d.tenancy.ProjectScope(r.Context(), id)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
			return tenancy.Scope{}, false
		}
		d.internal(w, r, err)
		return tenancy.Scope{}, false
	}
	res := resourceOf(scope)
	switch err := d.authz.Authorize(r.Context(), p, res, action); {
	case err == nil:
		return scope, true
	case errors.Is(err, authz.ErrNoAccess):
		httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
	case errors.Is(err, authz.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "your role does not allow this", nil)
	default:
		d.internal(w, r, err)
	}
	return tenancy.Scope{}, false
}

// viewer is the common prologue of every read.
func (d *dashboard) viewer(w http.ResponseWriter, r *http.Request) (tenancy.Scope, bool) {
	p, ok := d.user(w, r)
	if !ok {
		return tenancy.Scope{}, false
	}
	return d.project(w, r, p, authz.ActionView)
}

func limitParam(r *http.Request, def int) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if n <= 0 {
		return def
	}
	return n
}

type projectDTO struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	WorkspaceName string `json:"workspaceName"`
	// Actions the caller may perform here (§11.3.2 matrix), so the UI hides
	// what it must not offer; the server still checks every call.
	Actions []authz.Action `json:"actions"`
}

// projects: the projects a user may read, via memberships at any level,
// sorted by name, offset-paged (the visible set is small and computed in
// memory; the cursor is still opaque and bound to the user).
func (d *dashboard) projects(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	ms, err := d.authz.ListMemberships(r.Context(), p.UserID)
	if err != nil {
		d.internal(w, r, err)
		return
	}
	// Each membership reaches the projects under its node, so the roles a
	// project is reached through are exactly the chain Authorize walks —
	// no second query per project.
	index := map[tenancy.ProjectID]int{}
	roles := map[tenancy.ProjectID][]authz.Role{}
	var all []projectDTO
	for _, m := range ms {
		projects, err := d.tenancy.ListProjects(r.Context(), string(m.Scope.Level), m.Scope.ID)
		if err != nil && !errors.Is(err, tenancy.ErrNotFound) {
			d.internal(w, r, err)
			return
		}
		for _, pr := range projects {
			if _, ok := index[pr.ID]; !ok {
				index[pr.ID] = len(all)
				all = append(all, projectDTO{ID: string(pr.ID), Name: pr.Name, WorkspaceName: pr.WorkspaceName})
			}
			roles[pr.ID] = append(roles[pr.ID], m.Role)
		}
	}
	for id, i := range index {
		all[i].Actions = authz.ActionsFor(roles[id])
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Name != all[j].Name {
			return all[i].Name < all[j].Name
		}
		return all[i].ID < all[j].ID
	})
	limit := limitParam(r, 50)
	if limit > 100 {
		limit = 100
	}
	offset := 0
	if c := r.URL.Query().Get("cursor"); c != "" {
		raw, err := base64.RawURLEncoding.DecodeString(c)
		parts := strings.SplitN(string(raw), ":", 2)
		if err != nil || len(parts) != 2 || parts[0] != p.UserID {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is not valid for this list", nil)
			return
		}
		offset, _ = strconv.Atoi(parts[1])
		if offset < 0 || offset > len(all) {
			offset = len(all)
		}
	}
	end := offset + limit
	out := page{Items: []projectDTO{}}
	if end > len(all) {
		end = len(all)
	}
	if offset < end {
		out.Items = all[offset:end]
	}
	if end < len(all) {
		c := base64.RawURLEncoding.EncodeToString([]byte(p.UserID + ":" + strconv.Itoa(end)))
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
}

func (d *dashboard) runs(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	qs := r.URL.Query()
	if s := qs.Get("sort"); s != "" && s != "-startedAt" {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_SORT", "only sort=-startedAt is supported", nil)
		return
	}
	pg, err := d.query.Runs(r.Context(), string(scope.ProjectID), query.RunFilter{
		Q: qs.Get("q"), Status: qs.Get("status"), Environment: qs.Get("environment"), Limit: limitParam(r, 20), Cursor: qs.Get("cursor"),
	})
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, envelope(pg))
}

func (d *dashboard) run(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	run, err := d.query.Run(r.Context(), string(scope.ProjectID), r.PathValue("runId"))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (d *dashboard) results(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	pg, err := d.query.Results(r.Context(), string(scope.ProjectID), r.PathValue("runId"), limitParam(r, 50), r.URL.Query().Get("cursor"))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, envelope(pg))
}

func (d *dashboard) resultDetail(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	det, err := d.query.ResultDetail(r.Context(), string(scope.ProjectID), r.PathValue("resultId"))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, det)
}

func (d *dashboard) identities(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	qs := r.URL.Query()
	pg, err := d.query.Identities(r.Context(), string(scope.ProjectID), qs.Get("view"), limitParam(r, 20), qs.Get("cursor"))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, envelope(pg))
}

func (d *dashboard) identity(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	it, err := d.query.Identity(r.Context(), string(scope.ProjectID), r.PathValue("identityId"))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (d *dashboard) identityRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	pg, err := d.query.IdentityRuns(r.Context(), string(scope.ProjectID), r.PathValue("identityId"), limitParam(r, 20), r.URL.Query().Get("cursor"))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, envelope(pg))
}

type triageRequest struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// P1 types the UI may emit (contract): FAILURE_SEEN needs a viewer, the
// rest member or above (§11.3.2). Category acceptance stays with the
// operator CLI until the UI has a category picker.
var uiTriageTypes = map[triage.EventType]authz.Action{
	triage.FailureSeen:      authz.ActionView,
	triage.MarkedFlaky:      authz.ActionTriage,
	triage.MarkedKnownIssue: authz.ActionTriage,
	triage.Resolved:         authz.ActionTriage,
}

// triageEvent is the UI's only way to move TTRC (§8.3).
func (d *dashboard) triageEvent(w http.ResponseWriter, r *http.Request) {
	p, ok := d.user(w, r)
	if !ok {
		return
	}
	var req triageRequest
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON", nil)
		return
	}
	action, allowed := uiTriageTypes[triage.EventType(req.Type)]
	if !allowed {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "type must be FAILURE_SEEN, MARKED_FLAKY, MARKED_KNOWN_ISSUE or RESOLVED", map[string]any{"field": "type"})
		return
	}
	scope, ok := d.project(w, r, p, action)
	if !ok {
		return
	}
	ev, created, err := d.triage.Record(r.Context(), triage.Record{
		ProjectID: string(scope.ProjectID), TestResultID: r.PathValue("resultId"), Type: triage.EventType(req.Type),
		ActorType: triage.ActorUser, ActorID: p.UserID, Payload: req.Payload,
	})
	if err != nil {
		switch {
		case errors.Is(err, triage.ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
		case errors.Is(err, triage.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "RESULT_NOT_FOUND", "test result not found", nil)
		default:
			d.internal(w, r, err)
		}
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	u, _ := d.authz.GetUser(r.Context(), p.UserID)
	writeJSON(w, status, query.TriageEvent{ID: ev.ID, Type: string(ev.Type), ActorName: u.Email, OccurredAt: ev.OccurredAt})
}

func days(r *http.Request, def int) time.Time {
	n, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if n <= 0 || n > 365 {
		n = def
	}
	return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
}

func (d *dashboard) triageMetrics(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	m, err := d.query.Triage(r.Context(), string(scope.ProjectID), days(r, 30))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"failures": m.Failures, "triaged": m.Triaged, "seen": m.Seen,
		"untriagedRate": m.UntriagedRate, "triageCoverage": m.TriageCoverage,
		"ttrcMedianSeconds": nullableSeconds(m.TTRCMedian, m.HasTTRC), "timeToFirstViewSeconds": nullableSeconds(m.TimeToFirstView, m.HasFirst),
	})
}

func nullableSeconds(d time.Duration, ok bool) *float64 {
	if !ok {
		return nil
	}
	s := d.Seconds()
	return &s
}

func (d *dashboard) trend(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	rows, err := d.query.CategoryTrend(r.Context(), string(scope.ProjectID), days(r, 90))
	if err != nil {
		d.queryErr(w, r, err)
		return
	}
	if rows == nil {
		rows = []query.TrendBucket{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (d *dashboard) queryErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, query.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found", nil)
	case errors.Is(err, query.ErrBadCursor):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_CURSOR", "cursor is not valid for this list; restart from the first page", nil)
	case errors.Is(err, query.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_QUERY", err.Error(), nil)
	default:
		d.internal(w, r, err)
	}
}

func (d *dashboard) internal(w http.ResponseWriter, r *http.Request, err error) {
	httpx.Cause(r.Context(), fmt.Errorf("dashboard: %w", err))
	httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "internal error; quote the X-Request-Id when reporting", nil)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// inlineTypes is the §11.5 allowlist: only these render in the browser tab;
// everything else is Content-Disposition: attachment. HTML, SVG and
// anything a browser would execute never get here — AllowedContentType
// refused them at declare — but the default is still "download", so a new
// kind cannot become inline by accident.
var inlineTypes = map[string]bool{
	"image/png": true, "image/webp": true, "image/jpeg": true,
	"video/webm": true, "video/mp4": true,
	"text/plain": true,
}

// artifactContent streams a verified artifact's bytes to a member of its
// project (§11.5: attachment by default, nosniff, never cached by a shared
// cache). The api relays from the object store rather than redirecting to
// a presigned URL so these headers are ours on every store, including the
// dev one; at design-partner volumes the relay is cheap, and switching to a
// presigned GET with response-content-disposition is a local change here.
func (d *dashboard) artifactContent(w http.ResponseWriter, r *http.Request) {
	scope, ok := d.viewer(w, r)
	if !ok {
		return
	}
	a, err := d.artifact.Get(r.Context(), string(scope.ProjectID), r.PathValue("artifactId"))
	if err != nil {
		if errors.Is(err, artifact.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "artifact not found", nil)
			return
		}
		d.internal(w, r, err)
		return
	}
	if a.Status != artifact.StatusVerified {
		// Pending, rejected, expired: there is nothing trustworthy to serve.
		httpx.WriteError(w, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "artifact not found", nil)
		return
	}
	body, err := d.store.Get(r.Context(), a.StorageKey)
	if err != nil {
		if errors.Is(err, objstore.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "artifact not found", nil)
			return
		}
		httpx.Cause(r.Context(), err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "object store unavailable; retry", nil)
		return
	}
	defer body.Close()
	disposition := "attachment"
	if inlineTypes[a.ContentType] {
		disposition = "inline"
	}
	// The name is the reporter's (untrusted): quoted, with quotes and
	// control characters removed, and the RFC 5987 form for anything non-ASCII.
	w.Header().Set("Content-Disposition", disposition+"; "+dispositionFilename(a.Name))
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if a.ActualBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(a.ActualBytes, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

// dispositionFilename renders filename= (ASCII, sanitized) and filename*=
// (UTF-8, percent-encoded) for a Content-Disposition header.
func dispositionFilename(name string) string {
	ascii := make([]byte, 0, len(name))
	for _, r := range name {
		switch {
		case r == '"' || r == '\\' || r < 0x20 || r == 0x7f:
			ascii = append(ascii, '_')
		case r > 0x7e:
			ascii = append(ascii, '_')
		default:
			ascii = append(ascii, byte(r))
		}
	}
	if len(ascii) == 0 {
		ascii = []byte("artifact")
	}
	return `filename="` + string(ascii) + `"; filename*=UTF-8''` + url.PathEscape(name)
}
