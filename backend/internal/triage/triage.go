// Package triage owns TriageEvent and AiHypothesis (§6.9.5, §8.3, §8.7).
// TriageEvent is the single source of truth for TTRC: every metric in §8.3
// is derived from these rows and nothing else, and the rows are
// append-only. P1-min records events by hand (cmd/admin); the dashboard
// (login) and AI (P1.5, QĐ-4) are later writers of the same table.
//
// Accepting a hypothesis also stamps the §8.7 category onto the
// TestResult through results — the one write this module makes outside
// its own tables, and it goes through results' contract.
//
// This file is the whole contract (§6.9.4). Domain fan-out: results only.
package triage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
)

// EventType is §8.3's enum.
type EventType string

const (
	FailureSeen        EventType = "FAILURE_SEEN"
	HypothesisProposed EventType = "HYPOTHESIS_PROPOSED"
	HypothesisAccepted EventType = "HYPOTHESIS_ACCEPTED"
	HypothesisRejected EventType = "HYPOTHESIS_REJECTED"
	MarkedFlaky        EventType = "MARKED_FLAKY"
	MarkedKnownIssue   EventType = "MARKED_KNOWN_ISSUE"
	Assigned           EventType = "ASSIGNED"
	Resolved           EventType = "RESOLVED"
)

type ActorType string

const (
	ActorUser   ActorType = "USER"
	ActorAI     ActorType = "AI"
	ActorSystem ActorType = "SYSTEM"
)

// Category is the closed §8.7 taxonomy, shared by humans and AI.
type Category string

const (
	CategoryAppBug             Category = "app_bug"
	CategoryTestBug            Category = "test_bug"
	CategoryFlakyTiming        Category = "flaky_timing"
	CategoryEnvInfra           Category = "env_infra"
	CategoryEnvData            Category = "env_data"
	CategoryDependencyExternal Category = "dependency_external"
	CategoryUnknown            Category = "unknown"
)

var categories = map[Category]bool{
	CategoryAppBug: true, CategoryTestBug: true, CategoryFlakyTiming: true, CategoryEnvInfra: true,
	CategoryEnvData: true, CategoryDependencyExternal: true, CategoryUnknown: true,
}

// ValidCategory reports whether c is in §8.7.
func ValidCategory(c Category) bool { return categories[c] }

// Event is §8.3 TriageEvent.
type Event struct {
	ID           string
	ProjectID    string
	TestResultID string
	RunID        string
	Type         EventType
	ActorType    ActorType
	ActorID      string
	HypothesisID string
	Payload      json.RawMessage
	OccurredAt   time.Time
}

// Record is what a caller supplies. TestResultID must belong to
// ProjectID (checked through results). Category is required for
// HYPOTHESIS_ACCEPTED without a hypothesis (a human's own verdict) and is
// stamped on the result; Payload carries anything else (note, assignee).
type Record struct {
	ProjectID    string
	TestResultID string
	Type         EventType
	ActorType    ActorType
	ActorID      string
	HypothesisID string
	Category     Category
	Payload      map[string]any
	OccurredAt   time.Time // zero = now
}

var (
	ErrNotFound      = errors.New("triage: not found")
	ErrInvalid       = errors.New("triage: invalid input")
	ErrNotConfigured = errors.New("triage: AI not configured")
	ErrDailyLimit    = errors.New("triage: daily AI limit reached")
	ErrDecided       = errors.New("triage: hypothesis already decided")
)

const JobInvestigation = "ai.investigation"

type InvestigationStatus string

const (
	InvestigationQueued        InvestigationStatus = "queued"
	InvestigationCollecting    InvestigationStatus = "collecting"
	InvestigationInvestigating InvestigationStatus = "investigating"
	InvestigationDone          InvestigationStatus = "done"
	InvestigationError         InvestigationStatus = "error"
)

type EvidenceFact struct {
	Source string `json:"source"`
	Fact   string `json:"fact"`
}
type Hypothesis struct {
	ID              string         `json:"id"`
	Category        Category       `json:"category"`
	Confidence      float64        `json:"confidence"`
	Evidence        []EvidenceFact `json:"evidence"`
	SuggestedAction string         `json:"suggestedAction"`
	Decision        string         `json:"decision,omitempty"`
}
type Investigation struct {
	ID           string              `json:"id"`
	ProjectID    string              `json:"-"`
	ResultID     string              `json:"-"`
	Status       InvestigationStatus `json:"status"`
	RequestedBy  string              `json:"requestedBy"`
	Provider     string              `json:"provider,omitempty"`
	Model        string              `json:"model,omitempty"`
	KeySource    string              `json:"keySource,omitempty"`
	TurnCount    int                 `json:"turnCount"`
	ToolCount    int                 `json:"toolCount"`
	InputTokens  int                 `json:"inputTokens"`
	OutputTokens int                 `json:"outputTokens"`
	Error        string              `json:"error,omitempty"`
	Hypothesis   *Hypothesis         `json:"hypothesis,omitempty"`
	CreatedAt    time.Time           `json:"createdAt"`
	StartedAt    *time.Time          `json:"startedAt,omitempty"`
	FinishedAt   *time.Time          `json:"finishedAt,omitempty"`
}
type NewInvestigation struct{ ProjectID, OrgID, ResultID, Actor string }
type Decision struct {
	ProjectID, ResultID, HypothesisID, Actor, Decision, Note string
	Category                                                 Category
}

type EvidenceReader interface {
	InvestigationEvidence(ctx context.Context, projectID, resultID string) (json.RawMessage, string, string, error)
}
type CredentialSource interface {
	Credentials(ctx context.Context, orgID string) (llm.Account, string, bool, error)
}
type InvestigatorLLM interface {
	JSON(context.Context, llm.Request) (llm.Result, error)
}
type InvestigationDeps struct {
	Evidence EvidenceReader
	Keys     CredentialSource
	LLM      InvestigatorLLM
}
type Option func(*service)

func WithInvestigation(d InvestigationDeps) Option {
	return func(s *service) { s.evidence, s.keys, s.llm = d.Evidence, d.Keys, d.LLM }
}

// Service is the contract. Writes join the caller's transaction.
type Service interface {
	// PurgeResults deletes triage of results about to be purged with their
	// run (§11.3.2); results says which.
	PurgeResults(ctx context.Context, resultIDs []string) error

	// PurgeProject deletes everything this module keeps for a project that
	// tenancy has soft-deleted and whose restore window has passed (§11.3.2
	// "xóa project"); cmd/worker calls every module, tenancy last. Idempotent:
	// a purge cut short is finished by the next one.
	PurgeProject(ctx context.Context, projectID string) error

	// Record appends one event. FAILURE_SEEN from the same user on the same
	// result within 24 h is collapsed into the earlier row (§8.3 dedupe)
	// and returned with created=false.
	Record(ctx context.Context, in Record) (ev Event, created bool, err error)
	// ListForResult returns a result's events, oldest first.
	ListForResult(ctx context.Context, projectID, testResultID string) ([]Event, error)
	RequestInvestigation(ctx context.Context, in NewInvestigation) (Investigation, bool, error)
	GetInvestigation(ctx context.Context, projectID, resultID, id string) (Investigation, error)
	ListInvestigations(ctx context.Context, projectID, resultID string, limit int) ([]Investigation, error)
	DecideHypothesis(ctx context.Context, in Decision) (Event, error)
	InvestigationWorker(concurrency int) *queue.Worker
}
