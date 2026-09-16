package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Deps is what the endpoints and sweeps need; cmd/api and cmd/worker
// assemble it.
type Deps struct {
	DB       *pg.DB
	Tenancy  tenancy.Service
	Authz    authz.Service
	Results  results.Service
	Artifact Service
	Metrics  *metric.Registry
	Logger   *slog.Logger
}

const maxBody = 64 << 10

type handlers struct {
	Deps
	uploads *metric.Counter
	bytes   *metric.Counter
}

// Routes mounts §7.2 endpoints 4 and 6 behind bearer auth; endpoint 5 is
// the presigned URL itself and never touches this server.
func Routes(mux *http.ServeMux, d Deps) {
	h := &handlers{
		Deps:    d,
		uploads: d.Metrics.Counter("artifact_uploads_total", "Artifact declare/complete outcomes by project (§6.4 artifact row).", "project", "result"),
		bytes:   d.Metrics.Counter("artifact_bytes_total", "Verified artifact bytes by project and kind (§6.4 cost row).", "project", "kind"),
	}
	auth := authz.Middleware(d.Authz)
	mux.Handle("POST /v1/artifacts", auth(http.HandlerFunc(h.declare)))
	mux.Handle("POST /v1/artifacts/{artifactId}/complete", auth(http.HandlerFunc(h.complete)))
}

type caller struct {
	project tenancy.ProjectID
	scope   tenancy.Scope
}

// authorize mirrors ingest's: a project-scoped token with project:ingest.
func (h *handlers) authorize(w http.ResponseWriter, r *http.Request) (caller, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok || p.Kind != authz.PrincipalToken {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "artifact upload requires an API token", nil)
		return caller{}, false
	}
	if p.TokenScope.Level != authz.LevelProject {
		httpx.WriteError(w, http.StatusForbidden, "TOKEN_SCOPE", "ingest tokens must be scoped to one project", nil)
		return caller{}, false
	}
	project := tenancy.ProjectID(p.TokenScope.ID)
	scope, err := h.Tenancy.ProjectScope(r.Context(), project)
	if err != nil {
		if errors.Is(err, tenancy.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
			return caller{}, false
		}
		h.internal(w, r, "project scope", err)
		return caller{}, false
	}
	res := authz.Resource{OrgID: string(scope.OrgID), WorkspaceID: string(scope.WorkspaceID), ProjectID: string(scope.ProjectID)}
	switch err := h.Authz.Authorize(r.Context(), p, res, authz.ActionIngest); {
	case err == nil:
	case errors.Is(err, authz.ErrNoAccess):
		httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
		return caller{}, false
	case errors.Is(err, authz.ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "token lacks project:ingest", nil)
		return caller{}, false
	default:
		h.internal(w, r, "authorize", err)
		return caller{}, false
	}
	return caller{project: project, scope: scope}, true
}

type declareRequest struct {
	LocalRunID      string `json:"localRunId"`
	ClientTestID    string `json:"clientTestId"`
	OccurrenceIndex int    `json:"occurrenceIndex"`
	Attempt         *int   `json:"attempt"`
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	ContentType     string `json:"contentType"`
	SizeBytes       int64  `json:"sizeBytes"`
	SHA256          string `json:"sha256"`
}

type declareResponse struct {
	ArtifactID string         `json:"artifactId"`
	Status     Status         `json:"status"`
	ExpiresAt  time.Time      `json:"expiresAt"`
	Upload     *putResponse   `json:"upload,omitempty"`
	Multipart  *multipartResp `json:"multipart,omitempty"`
	Limits     map[Kind]int64 `json:"limits"`
	Linked     bool           `json:"linked"`
}

type putResponse struct {
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expiresAt"`
}

type multipartResp struct {
	UploadID  string    `json:"uploadId"`
	PartSize  int64     `json:"partSize"`
	Parallel  int       `json:"maxParallel"`
	Parts     []PartURL `json:"parts"`
	Received  []int     `json:"received"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func (h *handlers) declare(w http.ResponseWriter, r *http.Request) {
	c, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var req declareRequest
	if !h.decode(w, r, &req) {
		return
	}
	if req.Attempt == nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "attempt is required on every artifact (§7.7)", map[string]any{"field": "attempt"})
		return
	}
	run, err := h.Results.GetRun(r.Context(), string(c.project), strings.ToLower(req.LocalRunID))
	if err != nil {
		if errors.Is(err, results.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "RUN_NOT_FOUND", "run not found", nil)
			return
		}
		h.internal(w, r, "lookup run", err)
		return
	}
	plan := "free"
	if org, err := h.Tenancy.GetOrganization(r.Context(), c.scope.OrgID); err == nil {
		plan = org.PlanID
	}
	a, up, created, err := h.Artifact.Declare(r.Context(), Declare{
		ProjectID: string(c.project), RunID: string(run.ID), Plan: plan,
		ClientTestID: req.ClientTestID, OccurrenceIndex: req.OccurrenceIndex, Attempt: *req.Attempt,
		Kind: Kind(req.Kind), Name: req.Name, ContentType: req.ContentType, SizeBytes: req.SizeBytes, SHA256: strings.ToLower(req.SHA256),
	})
	if err != nil {
		var tooLarge *TooLargeError
		switch {
		case errors.As(err, &tooLarge):
			// The run stays valid (§10.4); only this artifact is refused.
			h.uploads.Inc(string(c.project), "too_large")
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "ARTIFACT_TOO_LARGE", err.Error(), map[string]any{"kind": tooLarge.Kind, "limit": tooLarge.Limit, "sizeBytes": tooLarge.Got})
		case errors.Is(err, ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
		case errors.Is(err, ErrGone):
			httpx.WriteError(w, http.StatusGone, "ARTIFACT_GONE", "artifact expired or deleted; declare it under a new name", nil)
		case errors.Is(err, ErrStore):
			h.uploads.Inc(string(c.project), "store_error")
			h.Logger.Warn("object store", "request_id", httpx.RequestIDFrom(r.Context()), "err", err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "object store unavailable; retry", nil)
		default:
			h.internal(w, r, "declare", err)
		}
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK // an earlier declare of the same artifact (§7.2 #4)
	}
	resp := declareResponse{ArtifactID: a.ID, Status: a.Status, ExpiresAt: a.ExpiresAt, Limits: Caps, Linked: a.AttemptID != ""}
	if up.Put != nil {
		resp.Upload = &putResponse{Method: "PUT", URL: up.Put.URL, Headers: up.Put.Headers, ExpiresAt: up.Put.ExpiresAt}
	}
	if up.Multipart != nil {
		m := up.Multipart
		resp.Multipart = &multipartResp{UploadID: m.UploadID, PartSize: m.PartSize, Parallel: m.Parallel, Parts: m.Parts, Received: m.Received, ExpiresAt: m.ExpiresAt}
		if resp.Multipart.Received == nil {
			resp.Multipart.Received = []int{}
		}
	}
	h.write(w, status, resp)
}

type completeRequest struct {
	SHA256 string `json:"sha256"`
	Parts  []struct {
		PartNumber int    `json:"partNumber"`
		ETag       string `json:"etag"`
	} `json:"parts"`
}

type completeResponse struct {
	ArtifactID string    `json:"artifactId"`
	Status     Status    `json:"status"`
	SizeBytes  int64     `json:"sizeBytes"`
	Linked     bool      `json:"linked"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

func (h *handlers) complete(w http.ResponseWriter, r *http.Request) {
	c, ok := h.authorize(w, r)
	if !ok {
		return
	}
	var req completeRequest
	if r.ContentLength != 0 {
		if !h.decode(w, r, &req) {
			return
		}
	}
	in := Complete{SHA256: strings.ToLower(req.SHA256)}
	for _, p := range req.Parts {
		in.Parts = append(in.Parts, CompletedPart{Number: p.PartNumber, ETag: p.ETag})
	}
	a, verified, err := h.Artifact.Finish(r.Context(), string(c.project), r.PathValue("artifactId"), in)
	if err != nil {
		var mismatch *MismatchError
		switch {
		case errors.As(err, &mismatch):
			h.uploads.Inc(string(c.project), "rejected")
			httpx.WriteError(w, http.StatusUnprocessableEntity, mismatch.Code, err.Error(), map[string]any{"expected": mismatch.Expected, "actual": mismatch.Actual})
		case errors.Is(err, ErrUploadIncomplete):
			httpx.WriteError(w, http.StatusConflict, "UPLOAD_INCOMPLETE", err.Error(), nil)
		case errors.Is(err, ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "ARTIFACT_NOT_FOUND", "artifact not found", nil)
		case errors.Is(err, ErrGone):
			httpx.WriteError(w, http.StatusGone, "ARTIFACT_GONE", "artifact expired or deleted", nil)
		case errors.Is(err, ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
		case errors.Is(err, ErrStore):
			h.uploads.Inc(string(c.project), "store_error")
			h.Logger.Warn("object store", "request_id", httpx.RequestIDFrom(r.Context()), "err", err)
			httpx.WriteError(w, http.StatusServiceUnavailable, "STORE_UNAVAILABLE", "object store unavailable; retry complete", nil)
		default:
			h.internal(w, r, "complete", err)
		}
		return
	}
	if verified {
		h.uploads.Inc(string(c.project), "verified")
		h.bytes.Add(float64(a.ActualBytes), string(c.project), string(a.Kind))
	}
	h.write(w, http.StatusOK, completeResponse{ArtifactID: a.ID, Status: a.Status, SizeBytes: a.ActualBytes, Linked: a.AttemptID != "", ExpiresAt: a.ExpiresAt})
}

func (h *handlers) decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "send application/json", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON: "+err.Error(), nil)
		return false
	}
	return true
}

func (h *handlers) internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	h.Logger.Error("artifact "+what, "request_id", httpx.RequestIDFrom(r.Context()), "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "internal error; quote the X-Request-Id when reporting", nil)
}

func (h *handlers) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Jobs is the worker-side half: the three sweeps of §7.7/§10.4/§6.4.
type Jobs struct {
	Deps
	overdue *metric.Gauge
	swept   *metric.Counter
}

// NewJobs registers the sweep metrics. Call once per process.
func NewJobs(d Deps) *Jobs {
	return &Jobs{
		Deps:    d,
		overdue: d.Metrics.Gauge("artifacts_unlinked_overdue", "Artifacts unlinked for more than 1 h (§6.4 alert when > 0)."),
		swept:   d.Metrics.Counter("artifact_sweeps_total", "Sweep actions by kind.", "action"),
	}
}

// Sweep links, expires and aborts; each step is independent so one
// failing store call does not stop the others.
func (j *Jobs) Sweep(ctx context.Context) error {
	var errs []error
	linked, overdue, err := j.Artifact.LinkPending(ctx)
	if err != nil {
		errs = append(errs, err)
	}
	j.overdue.Set(float64(overdue))
	j.swept.Add(float64(linked), "linked")
	if n, err := j.Artifact.ExpireRetention(ctx); err != nil {
		errs = append(errs, err)
	} else {
		j.swept.Add(float64(n), "expired")
	}
	if n, err := j.Artifact.AbortAbandoned(ctx); err != nil {
		errs = append(errs, err)
	} else {
		j.swept.Add(float64(n), "abandoned")
	}
	if overdue > 0 {
		j.Logger.Warn("artifacts unlinked over 1h", "count", overdue)
	}
	return errors.Join(errs...)
}
