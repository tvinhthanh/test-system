package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
)

// Wire types for §7.3. Unknown fields are tolerated (a newer reporter
// against an N−1 server, §6.5); known fields are checked strictly.

type runRequest struct {
	LocalRunID     string          `json:"localRunId"`
	EnvironmentKey string          `json:"environmentKey"`
	Trigger        json.RawMessage `json:"trigger"`
	Framework      json.RawMessage `json:"framework"`
	Reporter       json.RawMessage `json:"reporter"`
	StartedAt      time.Time       `json:"startedAt"`
}

// trigger is the part of Trigger the server reads. ciRunUrl is stored and
// rendered, never fetched (§7.10).
type trigger struct {
	CommitSHA string `json:"commitSha"`
	Ref       string `json:"ref"`
}

type runResponse struct {
	RunID       string            `json:"runId"`
	Limits      limits            `json:"limits"`
	IdentityMap map[string]string `json:"identityMap"`
}

// limits is what the reporter must read instead of hard-coding (§7.3).
// The artifact caps are artifact.Caps (§10.4), the one place they live.
type limits struct {
	MaxEventsPerBatch int                     `json:"maxEventsPerBatch"`
	MaxBatchBytes     int                     `json:"maxBatchBytes"`
	ArtifactBytes     map[artifact.Kind]int64 `json:"artifactBytes"`
}

type batchRequest struct {
	Events []eventRequest `json:"events"`
}

type eventRequest struct {
	Sequence        *int64       `json:"sequence"`
	Type            string       `json:"type"`
	ClientTestID    string       `json:"clientTestId"`
	OccurrenceIndex int          `json:"occurrenceIndex"`
	Attempt         *int         `json:"attempt"`
	Identity        *identityRaw `json:"identity"`
	Status          string       `json:"status"`
	Outcome         string       `json:"outcome"`
	DurationMs      int          `json:"durationMs"`
	Error           *errorRaw    `json:"error"`
	WorkerID        string       `json:"workerId"`
	StartedAt       time.Time    `json:"startedAt"`
	FinishedAt      time.Time    `json:"finishedAt"`
}

type identityRaw struct {
	Framework      string   `json:"framework"`
	FilePath       string   `json:"filePath"`
	TitlePath      []string `json:"titlePath"`
	ParamSignature string   `json:"paramSignature"`
}

type errorRaw struct {
	Message   string `json:"message"`
	Stack     string `json:"stack,omitempty"`
	Snippet   string `json:"snippet,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type batchResponse struct {
	Accepted    int               `json:"accepted"`
	Duplicates  int               `json:"duplicates"`
	IdentityMap map[string]string `json:"identityMap"`
}

type finishRequest struct {
	FinishedAt time.Time `json:"finishedAt"`
}

type finishResponse struct {
	RunID      string    `json:"runId"`
	Status     string    `json:"status"`
	FlakyCount int       `json:"flakyCount"`
	FinishedAt time.Time `json:"finishedAt"`
}

// Bounds on free-text fields, so one stack trace cannot bloat a row. A cut
// field is marked truncated rather than silently shortened.
const (
	maxMessage   = 8 << 10
	maxStack     = 64 << 10
	maxSnippet   = 16 << 10
	maxTrigger   = 4 << 10
	maxTitlePath = 32
	maxTitle     = 1024
	maxFilePath  = 1024
)

// invalid carries the field name into the §7.1 error envelope's details.
type invalid struct {
	field, msg string
}

func (e *invalid) Error() string { return e.field + ": " + e.msg }

func bad(field, msg string) error { return &invalid{field, msg} }

func (r *runRequest) validate() (results.NewRun, error) {
	if !isUUID(r.LocalRunID) {
		return results.NewRun{}, bad("localRunId", "must be a UUID")
	}
	if !tenancy.ValidSlug(r.EnvironmentKey) {
		return results.NewRun{}, bad("environmentKey", "lower-case letters, digits and single hyphens, at most 64 chars")
	}
	if r.StartedAt.IsZero() {
		return results.NewRun{}, bad("startedAt", "required, RFC 3339")
	}
	for name, raw := range map[string]json.RawMessage{"trigger": r.Trigger, "framework": r.Framework, "reporter": r.Reporter} {
		if len(raw) > 0 && !isJSONObject(raw) {
			return results.NewRun{}, bad(name, "must be an object")
		}
	}
	if len(r.Trigger) > maxTrigger {
		return results.NewRun{}, bad("trigger", fmt.Sprintf("at most %d bytes", maxTrigger))
	}
	var tr trigger
	_ = json.Unmarshal(r.Trigger, &tr) // already known to be an object or empty
	return results.NewRun{
		LocalRunID: strings.ToLower(r.LocalRunID),
		Trigger:    r.Trigger,
		Framework:  r.Framework,
		Reporter:   r.Reporter,
		CommitSHA:  tr.CommitSHA,
		Branch:     strings.TrimPrefix(tr.Ref, "refs/heads/"),
		StartedAt:  r.StartedAt,
	}, nil
}

// toAttempt validates one event and maps it to what results stores.
func (e *eventRequest) toAttempt() (results.AttemptEvent, error) {
	if e.Type != "test.finished" {
		return results.AttemptEvent{}, &unsupportedEvent{e.Type}
	}
	if e.Sequence == nil || *e.Sequence < 0 {
		return results.AttemptEvent{}, bad("sequence", "required, non-negative integer")
	}
	if e.ClientTestID == "" || len(e.ClientTestID) > 256 {
		return results.AttemptEvent{}, bad("clientTestId", "required, at most 256 chars")
	}
	if e.OccurrenceIndex < 0 {
		return results.AttemptEvent{}, bad("occurrenceIndex", "non-negative")
	}
	if e.Attempt == nil || *e.Attempt < 0 {
		return results.AttemptEvent{}, bad("attempt", "required on every test.* event (§7.3), 0-based")
	}
	id := e.Identity
	switch {
	case id == nil:
		return results.AttemptEvent{}, bad("identity", "required")
	case id.Framework == "":
		return results.AttemptEvent{}, bad("identity.framework", "required")
	case id.FilePath == "" || len(id.FilePath) > maxFilePath:
		return results.AttemptEvent{}, bad("identity.filePath", fmt.Sprintf("required, at most %d chars", maxFilePath))
	case len(id.TitlePath) == 0 || len(id.TitlePath) > maxTitlePath:
		return results.AttemptEvent{}, bad("identity.titlePath", fmt.Sprintf("1 to %d elements", maxTitlePath))
	}
	for i, t := range id.TitlePath {
		if t == "" || len(t) > maxTitle {
			return results.AttemptEvent{}, bad(fmt.Sprintf("identity.titlePath[%d]", i), fmt.Sprintf("non-empty, at most %d chars", maxTitle))
		}
	}
	status := results.AttemptStatus(e.Status)
	switch status {
	case results.AttemptPassed, results.AttemptFailed, results.AttemptSkipped, results.AttemptTimedOut, results.AttemptInterrupted:
	default:
		return results.AttemptEvent{}, bad("status", "one of passed|failed|skipped|timed_out|interrupted")
	}
	outcome := results.Outcome(e.Outcome)
	switch outcome {
	case results.OutcomeExpected, results.OutcomeUnexpected, results.OutcomeFlaky, results.OutcomeSkipped:
	default:
		return results.AttemptEvent{}, bad("outcome", "one of expected|unexpected|flaky|skipped (the framework's verdict, N41)")
	}
	if e.DurationMs < 0 {
		return results.AttemptEvent{}, bad("durationMs", "non-negative")
	}

	identity, _ := json.Marshal(id)
	var errJSON json.RawMessage
	if e.Error != nil {
		errJSON, _ = json.Marshal(e.Error.bounded())
	}
	return results.AttemptEvent{
		Sequence:        *e.Sequence,
		ClientTestID:    e.ClientTestID,
		OccurrenceIndex: e.OccurrenceIndex,
		Attempt:         *e.Attempt,
		DisplayName:     id.TitlePath[len(id.TitlePath)-1],
		SuiteName:       strings.Join(id.TitlePath[:len(id.TitlePath)-1], " › "),
		IdentityRaw:     identity,
		Status:          status,
		Outcome:         outcome,
		DurationMs:      e.DurationMs,
		Error:           errJSON,
		WorkerID:        e.WorkerID,
		StartedAt:       e.StartedAt,
		FinishedAt:      e.FinishedAt,
	}, nil
}

func (e errorRaw) bounded() errorRaw {
	cut := func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		e.Truncated = true
		// Cut on a rune boundary so the stored JSON stays valid UTF-8.
		for n > 0 && s[n]&0xC0 == 0x80 {
			n--
		}
		return s[:n]
	}
	e.Message = cut(e.Message, maxMessage)
	e.Stack = cut(e.Stack, maxStack)
	e.Snippet = cut(e.Snippet, maxSnippet)
	return e
}

type unsupportedEvent struct{ kind string }

func (u *unsupportedEvent) Error() string { return "unsupported event type " + u.kind }

func isJSONObject(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "{") && json.Valid(raw)
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
