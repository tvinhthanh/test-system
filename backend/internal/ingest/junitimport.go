package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// JUnitImport is how one JUnit report becomes a run (junit.go has the
// mapping). The caller has authorized the project: a project:ingest token
// (POST /v1/imports/junit) or a person who may configure it (the
// dashboard's upload, cmd/api).
type JUnitImport struct {
	EnvironmentKey string    // default "ci"
	Framework      string    // identity namespace, default "junit"
	Branch         string    // refs/heads/ is dropped
	CommitSHA      string    //
	StartedAt      time.Time // required only when the report has no timestamp
	LocalRunID     string    // default: derived from the report, so a re-import is the same run
	Source         string    // who imported, stored in the run's trigger ("token:<id>", "user:<email>")
}

// JUnitResult is what the import did.
type JUnitResult struct {
	RunID      string    `json:"runId"`
	LocalRunID string    `json:"localRunId"`
	Created    bool      `json:"created"` // false: this report was imported before
	Tests      int       `json:"tests"`
	Attempts   int       `json:"attempts"`
	Duplicates int       `json:"duplicates"`
	Status     string    `json:"status"`
	FlakyCount int       `json:"flakyCount"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
}

// ErrImportInvalid wraps a bad option (not the report); the text names it.
var ErrImportInvalid = errors.New("invalid import")

// ErrImportConflict: the localRunId is a run a reporter made — an import
// never adds to it (two sources in one run would be neither).
var ErrImportConflict = errors.New("localRunId belongs to a run a reporter created; importing never adds to it")

const importReporter = "atp-junit-import"

// ImportJUnit writes one report as a finished run and enqueues its tier-2
// resolve, exactly as a reporter's create → events → finish would.
// Idempotent on LocalRunID: the same report again adds nothing.
func (d Deps) ImportJUnit(ctx context.Context, project tenancy.ProjectID, in JUnitImport, report []byte) (JUnitResult, error) {
	if in.EnvironmentKey == "" {
		in.EnvironmentKey = "ci"
	}
	if !tenancy.ValidSlug(in.EnvironmentKey) {
		return JUnitResult{}, fmt.Errorf("%w: environment: lower-case letters, digits and single hyphens", ErrImportInvalid)
	}
	if in.Framework == "" {
		in.Framework = "junit"
	}
	if !tenancy.ValidSlug(in.Framework) {
		return JUnitResult{}, fmt.Errorf("%w: framework: a short lower-case name like junit, pytest, jest", ErrImportInvalid)
	}
	in.Branch = strings.TrimPrefix(strings.TrimSpace(in.Branch), "refs/heads/")
	if len(in.Branch) > 255 || len(in.CommitSHA) > 64 || len(in.Source) > 320 {
		return JUnitResult{}, fmt.Errorf("%w: branch, commit or source too long", ErrImportInvalid)
	}
	rep, err := parseJUnit(report, in.Framework, in.StartedAt.UTC())
	if err != nil {
		return JUnitResult{}, err
	}
	if in.LocalRunID == "" {
		in.LocalRunID = reportRunID(report, in.EnvironmentKey, in.Framework)
	} else if !isUUID(in.LocalRunID) {
		return JUnitResult{}, fmt.Errorf("%w: localRunId must be a UUID", ErrImportInvalid)
	}

	trigger, _ := json.Marshal(map[string]string{"ref": in.Branch, "commitSha": in.CommitSHA, "source": "junit-import", "importedBy": in.Source})
	framework, _ := json.Marshal(map[string]string{"name": in.Framework, "format": "junit-xml"})
	reporter, _ := json.Marshal(map[string]string{"name": importReporter, "version": "1"})
	var run results.Run
	var created bool
	err = d.DB.WithTx(ctx, func(ctx context.Context) error {
		env, err := d.Tenancy.EnsureEnvironment(ctx, project, in.EnvironmentKey)
		if err != nil {
			return err
		}
		run, created, err = d.Results.CreateRun(ctx, results.NewRun{
			ProjectID: string(project), EnvironmentID: string(env.ID), LocalRunID: strings.ToLower(in.LocalRunID),
			Trigger: trigger, Framework: framework, Reporter: reporter,
			CommitSHA: in.CommitSHA, Branch: in.Branch, StartedAt: rep.StartedAt,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, tenancy.ErrInvalid) || errors.Is(err, results.ErrInvalid) {
			return JUnitResult{}, fmt.Errorf("%w: %v", ErrImportInvalid, err)
		}
		return JUnitResult{}, err
	}
	if !created {
		var by struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(run.Reporter, &by) != nil || by.Name != importReporter {
			return JUnitResult{}, ErrImportConflict
		}
	}
	out := JUnitResult{RunID: string(run.ID), LocalRunID: run.LocalRunID, Created: created, Tests: rep.Tests, StartedAt: rep.StartedAt, FinishedAt: rep.FinishedAt}

	// A run finished by an earlier import of this report: nothing to add.
	if run.FinishedAt.IsZero() {
		// Tier 1 per batch, as the events endpoint does.
		for start := 0; start < len(rep.Attempts); start += MaxEventsPerBatch {
			batch := rep.Attempts[start:min(start+MaxEventsPerBatch, len(rep.Attempts))]
			refs := map[string]identity.Ref{}
			for _, a := range batch {
				if _, ok := refs[a.ClientTestID]; !ok {
					var sig identity.Signals
					_ = json.Unmarshal(a.IdentityRaw, &sig)
					refs[a.ClientTestID] = identity.Ref{Key: a.ClientTestID, Signals: sig}
				}
			}
			list := make([]identity.Ref, 0, len(refs))
			for _, r := range refs {
				list = append(list, r)
			}
			if m, err := d.Identity.ResolveInline(ctx, string(project), list); err == nil {
				for i := range batch {
					batch[i].StableTestID = m[batch[i].ClientTestID]
				}
			} else if d.Logger != nil {
				d.Logger.Warn("junit import: inline resolve failed, tier 2 will", "project", project, "err", err)
			}
			appended, err := d.Results.AppendAttempts(ctx, run.ID, batch)
			if err != nil {
				return JUnitResult{}, fmt.Errorf("append: %w", err)
			}
			out.Attempts += appended.Accepted
			out.Duplicates += appended.Duplicates
		}
		err = d.DB.WithTx(ctx, func(ctx context.Context) error {
			done, err := d.Results.FinishRun(ctx, run.ID, rep.FinishedAt)
			if err != nil {
				return err
			}
			run = done
			_, _, err = d.Queue.Add(ctx, queue.Enqueue{Kind: JobRunFinished, Key: string(done.ID), Payload: runJob{ProjectID: string(project), RunID: string(done.ID)}})
			return err
		})
		if err != nil {
			return JUnitResult{}, fmt.Errorf("finish: %w", err)
		}
	} else {
		out.Duplicates = len(rep.Attempts)
	}
	out.Status, out.FlakyCount = string(run.Status), run.FlakyCount
	return out, nil
}

// reportRunID is a UUID (v8 layout) from the report's bytes and where it
// goes: importing the same file twice is one run (§7.1 idempotency), the
// same file into another environment is another.
func reportRunID(report []byte, env, framework string) string {
	h := sha256.New()
	h.Write([]byte("atp-junit-import\x00" + env + "\x00" + framework + "\x00"))
	h.Write(report)
	b := h.Sum(nil)[:16]
	b[6] = (b[6] & 0x0f) | 0x80 // version 8
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	x := hex.EncodeToString(b)
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}

// JUnitOptions reads an import's options from the query string — the same
// for the token route and the dashboard upload.
//
//	?environment=ci&framework=junit&branch=main&commit=<sha>&startedAt=<RFC 3339>&localRunId=<uuid>
func JUnitOptions(q url.Values) (JUnitImport, error) {
	in := JUnitImport{
		EnvironmentKey: q.Get("environment"), Framework: q.Get("framework"),
		Branch: q.Get("branch"), CommitSHA: q.Get("commit"), LocalRunID: q.Get("localRunId"),
	}
	if s := q.Get("startedAt"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return in, fmt.Errorf("%w: startedAt: want RFC 3339, e.g. 2026-03-01T09:00:00Z", ErrImportInvalid)
		}
		in.StartedAt = t
	}
	return in, nil
}

// ReadJUnitBody reads an XML body up to MaxJUnitBytes; it writes the error
// answer itself and reports whether to go on.
func ReadJUnitBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/xml") && !strings.HasPrefix(ct, "text/xml") {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "send the JUnit report as application/xml", nil)
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxJUnitBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "REPORT_TOO_LARGE", "JUnit report over the limit; split it by suite", map[string]any{"maxBytes": MaxJUnitBytes})
			return nil, false
		}
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "could not read the body", nil)
		return nil, false
	}
	return body, true
}

// WriteJUnitError answers an ImportJUnit error; false when it is the
// caller's to log as internal.
func WriteJUnitError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrJUnit):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_JUNIT", err.Error(), nil)
	case errors.Is(err, ErrImportInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error(), nil)
	case errors.Is(err, ErrImportConflict):
		httpx.WriteError(w, http.StatusConflict, "RUN_CONFLICT", err.Error(), map[string]any{"field": "localRunId"})
	default:
		return false
	}
	return true
}

// importJUnit is POST /v1/imports/junit with a project:ingest token: CI or
// a script backfilling history file by file.
func (h *handlers) importJUnit(w http.ResponseWriter, r *http.Request) {
	rec := observed(r)
	c, ok := h.authorize(w, r)
	if !ok {
		return
	}
	rec.project = string(c.project)
	in, err := JUnitOptions(r.URL.Query())
	if err != nil {
		WriteJUnitError(w, err)
		return
	}
	body, ok := ReadJUnitBody(w, r)
	if !ok {
		return
	}
	in.Source = "token:" + c.principal.TokenID
	res, err := h.Deps.ImportJUnit(r.Context(), c.project, in, body)
	if err != nil {
		if !WriteJUnitError(w, err) {
			h.internal(w, r, "junit import", err)
		}
		return
	}
	h.events.Add(float64(res.Attempts), string(c.project), "accepted")
	h.events.Add(float64(res.Duplicates), string(c.project), "duplicate")
	h.byToken.Add(float64(res.Attempts), string(c.project), c.principal.TokenID)
	status := http.StatusCreated
	if !res.Created {
		status = http.StatusOK
	}
	h.write(w, status, res)
}
