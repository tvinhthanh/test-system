// Package identity owns TestIdentity, TestIdentityAlias and the Review Queue
// (§6.9.5, §8.5) and runs the §9 pipeline in two tiers (§6.9.10, §7.6):
//
//   - ResolveInline, inside the ingest request: exact alias match only,
//     O(1) via the partial unique index. Absorbs > 90% of events.
//   - ResolveDeferred, at run.finish: candidate set + scoring for the
//     fingerprints tier 1 did not know, now that the run's presence
//     information is complete. N39 measured recall 0.992 → 0.300 when this is
//     attempted at 50% visibility; the two entry points exist so nobody
//     collapses them into one.
//
// Merge, UndoMerge and SplitFrom are three different operations (N43), all
// audited. The scoring itself lives in internal/match, pure and tested
// against the prototype's numbers; this package is the database half.
//
// This file is the whole contract (§6.9.4). identity imports platform/* and
// nothing else under internal/: results calls it, not the other way round.
package identity

import (
	"context"
	"errors"
	"time"
)

// Signals is the raw identity a reporter sends with every event (§7.3).
type Signals struct {
	Framework      string   `json:"framework"`
	FilePath       string   `json:"filePath"`
	TitlePath      []string `json:"titlePath"`
	ParamSignature string   `json:"paramSignature"`
}

// Fingerprint is the §9.2 hash of the signals as this server computes it —
// the key every alias is stored under. Exposed so operators (cmd/admin
// split) and reporter tests can name a fingerprint without reimplementing
// the algorithm.
func (s Signals) Fingerprint() string { return toMatch(s).Fingerprint() }

// Ref is one test to resolve: an opaque key the caller wants back (ingest
// uses clientTestId) plus its signals.
type Ref struct {
	Key     string
	Signals Signals
}

// Identity is §8.5 TestIdentity.
type Identity struct {
	ID                 string
	ProjectID          string
	Status             Status
	MergedIntoID       string
	Framework          string
	CanonicalFilePath  string
	CanonicalTitlePath []string
	ParamSignature     string
	TemplateKey        string
	CurrentFingerprint string
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
}

type Status string

const (
	StatusActive  Status = "active"
	StatusMerged  Status = "merged"
	StatusRetired Status = "retired"
)

// AliasSource records how a fingerprint came to point at an identity.
type AliasSource string

const (
	SourceExact       AliasSource = "exact"
	SourceAutoMatch   AliasSource = "auto_match"
	SourceManualMerge AliasSource = "manual_merge"
	SourceManualSplit AliasSource = "manual_split"
)

// Alias is §8.5 TestIdentityAlias. ValidTo zero means currently in force.
type Alias struct {
	ID          string
	IdentityID  string
	ProjectID   string
	Fingerprint string
	Source      AliasSource
	MatchScore  float64
	CreatedBy   string
	ValidFrom   time.Time
	ValidTo     time.Time
}

// Outcome is what ResolveDeferred did for one Ref, for logs and metrics.
type Outcome struct {
	Key        string
	IdentityID string
	Decision   Decision
	Score      float64
	Gap        float64
}

type Decision string

const (
	DecisionExact     Decision = "exact_match"
	DecisionAutoMatch Decision = "auto_match"
	DecisionReview    Decision = "review_queue"
	DecisionNew       Decision = "new_identity"
)

// ReviewItem is one pair waiting for a human (§9.3 step 4, §9.4).
type ReviewItem struct {
	ID            string
	ProjectID     string
	IdentityID    string // the identity created at resolve time
	CandidateID   string // the suggested rename source
	Fingerprint   string
	Score         float64
	RunnerUpID    string
	RunnerUpScore float64
	Gap           float64
	Breakdown     map[string]float64
	RunID         string
	Status        ReviewStatus
	DecidedBy     string
	DecidedAt     time.Time
	CreatedAt     time.Time
}

type ReviewStatus string

const (
	ReviewPending ReviewStatus = "pending"
	ReviewMerged  ReviewStatus = "merged" // reviewer confirmed the rename: identity merged into candidate
	ReviewKept    ReviewStatus = "kept"   // reviewer kept them separate
)

var (
	ErrNotFound = errors.New("identity: not found")
	ErrInvalid  = errors.New("identity: invalid input")
	// ErrConflict: the operation contradicts current state (merging into a
	// retired identity, splitting into the current owner, ...).
	ErrConflict = errors.New("identity: conflict")
)

// Service is the contract. Writes join the caller's transaction (§6.9.6).
type Service interface {
	// ResolveInline is tier 1 (§7.6): exact alias match only, never creates.
	// Returns Key → identity id for the refs it knows; unknown keys are
	// absent. Touches last_seen_at of matched identities.
	ResolveInline(ctx context.Context, projectID string, refs []Ref) (map[string]string, error)

	// ResolveDeferred is tier 2: for each ref, exact match first, else the
	// §9.3 candidate/score/threshold pipeline, creating identities, aliases
	// and review items as decided. presentIDs are the identities already
	// seen in this run through tier 1 — the N34 signal. Every ref comes back
	// resolved. runID is recorded on review items only.
	ResolveDeferred(ctx context.Context, projectID, runID string, refs []Ref, presentIDs []string) (map[string]string, []Outcome, error)

	// ResolveAt answers "which identity owned this fingerprint at time t"
	// for historical backfill after a split (§8.5).
	ResolveAt(ctx context.Context, projectID, fingerprint string, at time.Time) (string, error)

	Get(ctx context.Context, id string) (Identity, error)
	Aliases(ctx context.Context, id string) ([]Alias, error)

	// Merge folds src into dst: every alias of src now points at dst, src is
	// retired with merged_into_id = dst. Retroactive by design (§9.4).
	Merge(ctx context.Context, projectID, dst, src, actor string) error
	// UndoMerge restores the aliases a Merge moved and reactivates src. It
	// is not SplitFrom (N43): the whole window since the merge is repaired.
	UndoMerge(ctx context.Context, projectID, src, actor string) error
	// SplitFrom closes the current alias of fingerprint at t and opens a
	// new identity owning it from t on. History before t is untouched.
	SplitFrom(ctx context.Context, projectID, fingerprint string, at time.Time, actor string) (newID string, err error)

	ListReview(ctx context.Context, projectID string, limit int) ([]ReviewItem, error)
	// DecideReview records a human verdict: ReviewMerged merges the new
	// identity into the candidate; ReviewKept leaves them separate.
	DecideReview(ctx context.Context, itemID string, decision ReviewStatus, actor string) error
}
