package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

type handlers struct {
	Deps
	requests *metric.Counter   // §6.4: error rate per endpoint per project
	events   *metric.Counter   // accepted vs duplicate
	byToken  *metric.Counter   // accepted per token (§11.4 volume alert)
	latency  *metric.Histogram // per endpoint
}

func newHandlers(d Deps) *handlers {
	return &handlers{
		Deps:     d,
		requests: d.Metrics.Counter("ingest_requests_total", "Ingest requests by endpoint, project and status class.", "endpoint", "project", "status"),
		events:   d.Metrics.Counter("ingest_events_total", "Events received, by project and result.", "project", "result"),
		byToken:  d.Metrics.Counter("ingest_events_by_token_total", "Accepted events by project and ingest token id — §11.4: a token whose volume jumps 10× its weekly baseline is a leaked or misconfigured pipeline.", "project", "token"),
		latency:  d.Metrics.Histogram("ingest_request_seconds", "Ingest request latency by endpoint.", metric.DefaultLatencyBuckets, "endpoint"),
	}
}

// caller is what every endpoint establishes first: the token's project and
// its ancestry, already authorized for ingest.
type caller struct {
	principal authz.Principal
	project   tenancy.ProjectID
	scope     tenancy.Scope
}

// authorize is the one path from bearer to project (§7.2, §11.3.3). Ingest
// tokens must be project-scoped: the URL carries no project, so the token
// is the only thing that says where the data goes.
func (h *handlers) authorize(w http.ResponseWriter, r *http.Request) (caller, bool) {
	p, ok := authz.PrincipalFrom(r.Context())
	if !ok || p.Kind != authz.PrincipalToken {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "ingest requires an API token", nil)
		return caller{}, false
	}
	if p.TokenScope.Level != authz.LevelProject {
		httpx.WriteError(w, http.StatusForbidden, "TOKEN_SCOPE", "ingest tokens must be scoped to one project", map[string]any{"scopeLevel": string(p.TokenScope.Level)})
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
	return caller{principal: p, project: project, scope: scope}, true
}

// decode reads at most limit bytes of JSON into v. Over the limit is 413
// BATCH_TOO_LARGE (§7.4); anything else that fails to parse is 400.
func (h *handlers) decode(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "send application/json", nil)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "BATCH_TOO_LARGE", "request body over limit", map[string]any{"maxBytes": limit})
			return false
		}
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "malformed JSON: "+err.Error(), nil)
		return false
	}
	if dec.More() {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "trailing data after JSON value", nil)
		return false
	}
	_, _ = io.Copy(io.Discard, r.Body)
	return true
}

func (h *handlers) createRun(w http.ResponseWriter, r *http.Request) {
	rec := observed(r)
	c, ok := h.authorize(w, r)
	if !ok {
		return
	}
	rec.project = string(c.project)
	if !h.protocolOK(w, r) {
		return
	}
	var req runRequest
	if !h.decode(w, r, MaxRunBytes, &req) {
		return
	}
	in, err := req.validate()
	if err != nil {
		h.invalid(w, err)
		return
	}
	in.ProjectID = string(c.project)

	var run results.Run
	var created bool
	err = h.DB.WithTx(r.Context(), func(ctx context.Context) error {
		env, err := h.Tenancy.EnsureEnvironment(ctx, c.project, req.EnvironmentKey)
		if err != nil {
			return fmt.Errorf("environment: %w", err)
		}
		in.EnvironmentID = string(env.ID)
		run, created, err = h.Results.CreateRun(ctx, in)
		return err
	})
	if err != nil {
		switch {
		case errors.Is(err, tenancy.ErrInvalid), errors.Is(err, results.ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
		case errors.Is(err, tenancy.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "PROJECT_NOT_FOUND", "project not found", nil)
		default:
			h.internal(w, r, "create run", err)
		}
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK // §7.2 #1: resend returns the existing run
	}
	h.write(w, status, runResponse{
		RunID:       string(run.ID),
		Limits:      limits{MaxEventsPerBatch: MaxEventsPerBatch, MaxBatchBytes: MaxBatchBytes, ArtifactBytes: artifact.Caps},
		IdentityMap: map[string]string{},
	})
}

func (h *handlers) appendEvents(w http.ResponseWriter, r *http.Request) {
	rec := observed(r)
	c, ok := h.authorize(w, r)
	if !ok {
		return
	}
	rec.project = string(c.project)
	if !h.protocolOK(w, r) {
		return
	}
	run, ok := h.lookupRun(w, r, c)
	if !ok {
		return
	}
	var req batchRequest
	if !h.decode(w, r, MaxBatchBytes, &req) {
		return
	}
	if len(req.Events) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "events must not be empty", nil)
		return
	}
	if len(req.Events) > MaxEventsPerBatch {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "BATCH_TOO_LARGE", "too many events in one batch", map[string]any{"maxEvents": MaxEventsPerBatch, "got": len(req.Events)})
		return
	}
	attempts := make([]results.AttemptEvent, 0, len(req.Events))
	refs := map[string]identity.Ref{} // one per clientTestId
	for i := range req.Events {
		a, err := req.Events[i].toAttempt()
		if err != nil {
			var unsupported *unsupportedEvent
			if errors.As(err, &unsupported) {
				httpx.WriteError(w, http.StatusBadRequest, "UNSUPPORTED_EVENT_TYPE", err.Error(), map[string]any{"index": i, "type": unsupported.kind})
				return
			}
			h.invalidAt(w, i, err)
			return
		}
		attempts = append(attempts, a)
		if _, seen := refs[a.ClientTestID]; !seen {
			id := req.Events[i].Identity
			refs[a.ClientTestID] = identity.Ref{Key: a.ClientTestID, Signals: identity.Signals{Framework: id.Framework, FilePath: id.FilePath, TitlePath: id.TitlePath, ParamSignature: id.ParamSignature}}
		}
	}

	// Tier 1 (§7.6) first: exact alias match only, one read, so a known test
	// lands with its stable id in the same row write instead of a second
	// update of every row (test/load, §14.2). A failure here is not the
	// reporter's problem — the events are stored unresolved and tier 2
	// resolves them at finish.
	identityMap := map[string]string{}
	list := make([]identity.Ref, 0, len(refs))
	for _, ref := range refs {
		list = append(list, ref)
	}
	if m, err := h.Identity.ResolveInline(r.Context(), string(c.project), list); err != nil {
		h.Logger.Warn("inline resolve failed", "request_id", httpx.RequestIDFrom(r.Context()), "project", c.project, "err", err)
	} else if len(m) > 0 {
		identityMap = m
		for i := range attempts {
			attempts[i].StableTestID = m[attempts[i].ClientTestID]
		}
	}

	var appended results.Appended
	err := h.DB.WithTx(r.Context(), func(ctx context.Context) error {
		var err error
		appended, err = h.Results.AppendAttempts(ctx, run.ID, attempts)
		return err
	})
	if err != nil {
		switch {
		case errors.Is(err, results.ErrRunFinished):
			httpx.WriteError(w, http.StatusConflict, "RUN_FINISHED", "run is no longer accepting events", nil)
		case errors.Is(err, results.ErrInvalid):
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
		case errors.Is(err, results.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "RUN_NOT_FOUND", "run not found", nil)
		default:
			h.internal(w, r, "append events", err)
		}
		return
	}
	h.events.Add(float64(appended.Accepted), string(c.project), "accepted")
	h.events.Add(float64(appended.Duplicates), string(c.project), "duplicate")
	h.byToken.Add(float64(appended.Accepted), string(c.project), c.principal.TokenID)

	h.write(w, http.StatusOK, batchResponse{Accepted: appended.Accepted, Duplicates: appended.Duplicates, IdentityMap: identityMap})
}

func (h *handlers) finishRun(w http.ResponseWriter, r *http.Request) {
	rec := observed(r)
	c, ok := h.authorize(w, r)
	if !ok {
		return
	}
	rec.project = string(c.project)
	if !h.protocolOK(w, r) {
		return
	}
	run, ok := h.lookupRun(w, r, c)
	if !ok {
		return
	}
	var req finishRequest
	if r.ContentLength != 0 {
		if !h.decode(w, r, MaxRunBytes, &req) {
			return
		}
	}
	if req.FinishedAt.IsZero() {
		req.FinishedAt = time.Now()
	}
	var done results.Run
	err := h.DB.WithTx(r.Context(), func(ctx context.Context) error {
		var err error
		done, err = h.Results.FinishRun(ctx, run.ID, req.FinishedAt)
		if err != nil {
			return err
		}
		// Tier 2 (§7.6) runs in the worker. The job commits with the finish
		// (§6.9.6 outbox) and is keyed by run, so a retried finish enqueues
		// nothing new.
		_, _, err = h.Queue.Add(ctx, queue.Enqueue{Kind: JobRunFinished, Key: string(done.ID), Payload: runJob{ProjectID: string(c.project), RunID: string(done.ID)}})
		return err
	})
	if err != nil {
		if errors.Is(err, results.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "RUN_NOT_FOUND", "run not found", nil)
			return
		}
		h.internal(w, r, "finish run", err)
		return
	}
	h.write(w, http.StatusOK, finishResponse{RunID: string(done.ID), Status: string(done.Status), FlakyCount: done.FlakyCount, FinishedAt: done.FinishedAt})
}

// lookupRun resolves {localRunId} inside the caller's project; a run of
// another project is indistinguishable from a missing one (§7.2).
func (h *handlers) lookupRun(w http.ResponseWriter, r *http.Request, c caller) (results.Run, bool) {
	local := r.PathValue("localRunId")
	if !isUUID(local) {
		httpx.WriteError(w, http.StatusNotFound, "RUN_NOT_FOUND", "run not found", nil)
		return results.Run{}, false
	}
	run, err := h.Results.GetRun(r.Context(), string(c.project), local)
	if err != nil {
		if errors.Is(err, results.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "RUN_NOT_FOUND", "run not found", nil)
			return results.Run{}, false
		}
		h.internal(w, r, "lookup run", err)
		return results.Run{}, false
	}
	return run, true
}

// protocolOK enforces §6.5: N and N−1 are served; older → 426. The header
// is optional so curl and early reporters work; when present it must parse.
func (h *handlers) protocolOK(w http.ResponseWriter, r *http.Request) bool {
	v := r.Header.Get("X-Protocol-Version")
	if v == "" {
		return true
	}
	major, _, _ := strings.Cut(v, ".")
	n, err := strconv.Atoi(major)
	if err != nil || n < 1 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PROTOCOL_VERSION", "X-Protocol-Version must look like 1.1", nil)
		return false
	}
	if n > 1 {
		httpx.WriteError(w, http.StatusUpgradeRequired, "UPGRADE_REQUIRED", "this server speaks protocol 1.x", map[string]any{"supported": []string{"1"}})
		return false
	}
	return true
}

func (h *handlers) invalid(w http.ResponseWriter, err error) {
	var inv *invalid
	if errors.As(err, &inv) {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", inv.msg, map[string]any{"field": inv.field})
		return
	}
	httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
}

func (h *handlers) invalidAt(w http.ResponseWriter, index int, err error) {
	var inv *invalid
	if errors.As(err, &inv) {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", inv.msg, map[string]any{"index": index, "field": "events[" + strconv.Itoa(index) + "]." + inv.field})
		return
	}
	httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), map[string]any{"index": index})
}

// internal answers 500 without the cause (§11.5) and logs it with the
// correlation id so the two can be joined.
func (h *handlers) internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	httpx.Cause(r.Context(), fmt.Errorf("ingest %s: %w", what, err))
	httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL", "internal error; quote the X-Request-Id when reporting", nil)
}

func (h *handlers) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// observation records one request's outcome for the §6.4 ingestion metrics.
// Everything inside observe writes through a capturing writer so the
// status is seen whoever wrote it — auth, the rate limit or the handler.
type observation struct {
	h        *handlers
	endpoint string
	project  string
	start    time.Time
	rec      *statusCapture
}

type observationKey struct{}

// observe is the outermost ingest middleware: one observation per request,
// reachable by the handler through observed(r) to set the project.
func (h *handlers) observe(endpoint string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sc := &statusCapture{ResponseWriter: w, status: http.StatusOK}
		ob := &observation{h: h, endpoint: endpoint, project: "-", start: time.Now(), rec: sc}
		defer ob.done()
		next.ServeHTTP(sc, r.WithContext(context.WithValue(r.Context(), observationKey{}, ob)))
	})
}

// observed returns the request's observation; a detached one (never
// recorded) when the handler is mounted without observe, as in unit tests.
func observed(r *http.Request) *observation {
	if ob, ok := r.Context().Value(observationKey{}).(*observation); ok {
		return ob
	}
	return &observation{project: "-", start: time.Now(), rec: &statusCapture{}}
}

func (o *observation) done() {
	class := strconv.Itoa(o.rec.status/100) + "xx"
	o.h.requests.Inc(o.endpoint, o.project, class)
	o.h.latency.Observe(time.Since(o.start).Seconds(), o.endpoint)
}

type statusCapture struct {
	http.ResponseWriter
	status int
}

func (s *statusCapture) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
