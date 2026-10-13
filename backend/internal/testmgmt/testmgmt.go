// Package testmgmt is the Test Management Plane (SPEC §6.8, P2, QĐ-12):
// requirements, manual test cases, the link from a test case to a
// TestIdentity, test plans with manual executions, and releases with the
// server-side quality gate of §6.8.4.
//
// Why it exists (§6.8.1): TestRail has stored test cases for fifteen years;
// the value here is the third link — a manual case tied to a stableTestId,
// so automation status and flaky history flow up to the requirement and the
// release. Consequently every link targets identity's TestIdentity.id and
// nothing else: never a run, never a result, never a clientTestId.
//
// Boundaries (§6.8.6): manual execution is not a Run; the gate reads the
// Intelligence Plane through Evidence and recomputes neither flaky nor
// TTRC. Domain fan-out: identity only (link validation). Owns migration
// 0013.
//
// This file is the whole contract (§6.9.4).
package testmgmt

import (
	"context"
	"errors"
	"time"
)

type Priority string

const (
	P0 Priority = "p0"
	P1 Priority = "p1"
	P2 Priority = "p2"
	P3 Priority = "p3"
)

// RequirementStatus is §6.8.2's enum.
type RequirementStatus string

const (
	ReqDraft   RequirementStatus = "draft"
	ReqActive  RequirementStatus = "active"
	ReqDone    RequirementStatus = "done"
	ReqDropped RequirementStatus = "dropped"
)

// Requirement is §6.8.2 plus a priority: §6.8.4's gate speaks of "p0
// requirements", which the entity table forgot to carry (N62).
type Requirement struct {
	ID          string
	ProjectID   string
	ExternalKey string // JIRA-123, Linear id; "" when none
	Title       string
	Priority    Priority
	Status      RequirementStatus
	CoveredBy   int // test cases linked through RequirementCoverage
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AutomationStatus string

const (
	ManualOnly          AutomationStatus = "manual_only"
	AutomationCandidate AutomationStatus = "automation_candidate"
	Automated           AutomationStatus = "automated"
	Obsolete            AutomationStatus = "obsolete"
)

// Step is one row of a manual case. Rendered as text by every consumer.
type Step struct {
	Action   string `json:"action"`
	Expected string `json:"expected"`
}

type TestCase struct {
	Preconditions    string
	TestData         string
	Audit            []CaseChange
	AuditTruncated   bool
	ID               string
	ProjectID        string
	SuiteID          string
	SuiteName        string
	ExternalKey      string
	Title            string
	Steps            []Step
	Expected         string
	Priority         Priority
	IsCritical       bool
	AutomationStatus AutomationStatus
	OwnerID          string
	Links            []Link   // present on Get, nil on lists
	Requirements     []string // requirement ids covered, present on Get
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CaseChange struct {
	ID            string    `json:"id"`
	Actor         string    `json:"actor"`
	Action        string    `json:"action"`
	ChangedFields []string  `json:"changedFields"`
	CreatedAt     time.Time `json:"createdAt"`
}

type LinkType string

const (
	LinkExact    LinkType = "exact"
	LinkPartial  LinkType = "partial"
	LinkSuperset LinkType = "superset"
)

// Link is TestCaseAutomationLink (§6.8.2) — the plane's load-bearing row.
type Link struct {
	ID           string
	TestCaseID   string
	StableTestID string
	LinkType     LinkType
	CreatedBy    string
	CreatedAt    time.Time
}

type Suite struct {
	ID        string
	ProjectID string
	ParentID  string
	Name      string
	Cases     int
	CreatedAt time.Time
}

type PlanStatus string

const (
	PlanDraft  PlanStatus = "draft"
	PlanActive PlanStatus = "active"
	PlanClosed PlanStatus = "closed"
)

type Plan struct {
	Cases            []PlanCase
	ID               string
	ProjectID        string
	Name             string
	EnvironmentID    string
	Status           PlanStatus
	Executions       []Execution // latest per case, present on Get
	ExecutionHistory []Execution // newest 200, present on Get
	HistoryTruncated bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PlanCase struct {
	TestCase       TestCase
	AssigneeUserID string
	AssigneeEmail  string
	AssignedBy     string
	AssignedAt     *time.Time
	WorkStatus     string
}

type PlanCaseUpdate struct {
	TestCaseID     string
	AssigneeUserID *string
	AssigneeEmail  *string
	WorkStatus     *string
}

type PlanPatch struct {
	Status            *PlanStatus
	CaseIDs           *[]string
	CaseUpdate        *PlanCaseUpdate
	ExpectedUpdatedAt time.Time
}

type ExecutionStatus string

const (
	ExecPassed  ExecutionStatus = "passed"
	ExecFailed  ExecutionStatus = "failed"
	ExecBlocked ExecutionStatus = "blocked"
	ExecSkipped ExecutionStatus = "skipped"
)

// Execution is ManualExecution (§6.8.2): one person ran one case once.
type Execution struct {
	ID           string
	PlanID       string
	TestCaseID   string
	CaseTitle    string
	ExecutedBy   string
	Status       ExecutionStatus
	Note         string
	ExecutedAt   time.Time
	CaseSnapshot *CaseSnapshot
	StepResults  []StepResult
	BugURL       string
	RetestOf     string
}

type CaseSnapshot struct {
	Preconditions *string   `json:"preconditions"`
	TestData      *string   `json:"testData"`
	Title         string    `json:"title"`
	Steps         []Step    `json:"steps"`
	Expected      string    `json:"expected"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type StepResult struct {
	StepIndex int             `json:"stepIndex"`
	Status    ExecutionStatus `json:"status"`
	Actual    string          `json:"actual"`
}

type ReleaseStatus string

const (
	ReleaseOpen     ReleaseStatus = "open"
	ReleaseBlocked  ReleaseStatus = "blocked"
	ReleaseApproved ReleaseStatus = "approved"
	ReleaseShipped  ReleaseStatus = "shipped"
)

type Release struct {
	ID           string
	ProjectID    string
	Version      string
	Branch       string // "" = newest finished run on any branch
	Status       ReleaseStatus
	DecidedBy    string
	DecidedAt    time.Time
	DecisionNote string
	Gate         *GateResult // last evaluation, nil until Evaluate ran
	EvaluatedAt  time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// GateLevel is what a rule does when it fails (§6.8.4 #3: declared per
// rule, never inferred, and warn never silently becomes blocked).
type GateLevel string

const (
	LevelBlocked GateLevel = "blocked"
	LevelWarn    GateLevel = "warn"
)

// GateVerdict is the whole gate's answer, machine-readable for CI (§6.8.4 #2).
type GateVerdict string

const (
	VerdictApproved   GateVerdict = "approved"
	VerdictWarn       GateVerdict = "warn"
	VerdictBlocked    GateVerdict = "blocked"
	VerdictNoEvidence GateVerdict = "no_evidence" // no finished run to judge
)

type GateRule struct {
	ID      string         `json:"id"`
	Level   GateLevel      `json:"level"`
	Passed  bool           `json:"passed"`
	Summary string         `json:"summary"`
	Details map[string]any `json:"details,omitempty"`
	// Applies is false for rules that have no meaning yet (prod smoke is P3,
	// QĐ-8) or lack data (first release has no previous coverage).
	Applies bool `json:"applies"`
}

type GateResult struct {
	Verdict     GateVerdict `json:"verdict"`
	EvaluatedAt time.Time   `json:"evaluatedAt"`
	RunID       string      `json:"runId,omitempty"`
	Branch      string      `json:"branch,omitempty"`
	Rules       []GateRule  `json:"rules"`
	// P0Coverage is kept so the next release can compare (§6.8.4 rule 4).
	P0Coverage *float64 `json:"p0Coverage,omitempty"`
}

// Policy holds the §6.8.4 thresholds, all [ASSUMPTION] until partners
// calibrate them; one place, so a change is a diff against the SPEC table.
type Policy struct {
	FlakyRateWarn float64 // suite flaky rate above this → warn (SPEC: 3 %)
}

var DefaultPolicy = Policy{FlakyRateWarn: 0.03}

// --- what the gate reads (§6.8.6: "Release gate đọc từ Intelligence Plane") ---

// ResultEvidence is one test result of the judged run, as the read model
// sees it: identity resolved, verdict, and whether a human decided on it.
type ResultEvidence struct {
	ResultID     string
	StableTestID string
	FinalStatus  string // passed | failed | skipped | timed_out | interrupted | flaky_pass
	Triaged      bool   // has HYPOTHESIS_ACCEPTED / MARKED_FLAKY / MARKED_KNOWN_ISSUE / RESOLVED
}

type RunEvidence struct {
	RunID      string
	Branch     string
	FinishedAt time.Time
	Total      int
	FlakyPass  int
	Results    []ResultEvidence
}

// Evidence is the consumer-side interface (§6.9.4) the gate needs from the
// Intelligence Plane; cmd/api satisfies it with query. Nothing here is
// recomputed — flaky and triage verdicts arrive decided.
type Evidence interface {
	// LatestFinishedRun returns the newest finished run of the project, on
	// the branch when branch != "", and ok=false when there is none.
	LatestFinishedRun(ctx context.Context, projectID, branch string) (run RunEvidence, ok bool, err error)
	// ROIFacts is what §6.8.3 counts over [from, to): measured by the
	// Intelligence Plane from TriageEvent and attempts, never estimated.
	ROIFacts(ctx context.Context, projectID string, from, to time.Time) (ROIFacts, error)
}

// ROIFacts are the measured inputs of §6.8.3 for one window.
type ROIFacts struct {
	Failures int // failures observed in the window
	Triaged  int // of them, with an accepted hypothesis (the TTRC stop, §8.3)
	// TTRCSum and TTRCMedian are over the triaged ones.
	TTRCSum    time.Duration
	TTRCMedian time.Duration
	// FlakyRetry is the CI time spent on attempts that failed before the
	// test passed (flaky_pass results): §6.8.3's first flaky term.
	FlakyRetry time.Duration
	// MarkedFlaky counts failures a person marked flaky in the window.
	MarkedFlaky int
}

// ---- ROI (§6.8.3, QĐ-13) ----

type BaselineKind string

const (
	Retrospective BaselineKind = "retrospective"
	Prospective   BaselineKind = "prospective"
)

// Baseline is one §5.4 measurement of TTRC before the platform, entered by
// a person (append-only; the newest prospective applies, else the newest
// retrospective).
type Baseline struct {
	ID                 string
	ProjectID          string
	Kind               BaselineKind
	Failures           int
	Median             time.Duration
	P90                time.Duration // 0 = not given
	RerunPassRate      *float64
	FlakyInvestigation time.Duration // 0 = not measured
	PeriodFrom         time.Time
	PeriodTo           time.Time
	Note               string
	EnteredBy          string
	EnteredAt          time.Time
}

type NewBaseline struct {
	Kind               BaselineKind
	Failures           int
	Median             time.Duration
	P90                time.Duration
	RerunPassRate      *float64
	FlakyInvestigation time.Duration
	PeriodFrom         time.Time
	PeriodTo           time.Time
	Note               string
}

// ROI is §6.8.3 for one month, in hours — never money: an hourly rate is
// the reader's to apply (rule 2). Saved is nil without a baseline (rule 3:
// "chưa đủ dữ liệu"); coverage and untriaged rate always travel with it
// (rule 1).
type ROI struct {
	From, To       time.Time
	Facts          ROIFacts
	TriageCoverage float64
	UntriagedRate  float64
	Baseline       *Baseline
	// SavedHours = Σ (baseline median − TTRC) over triaged failures; may be
	// negative (slower than before) and is shown as is.
	SavedHours *float64
	// FlakyHours = FlakyRetry + MarkedFlaky × baseline flaky investigation;
	// InvestigationCounted says whether the second term is in.
	FlakyHours           float64
	InvestigationCounted bool
	// FewTriaged: under ROIMinTriaged triaged failures — the number is shown
	// with a warning, it is not yet a trend.
	FewTriaged bool
}

// ROIMinTriaged is the sample under which ROI is flagged as thin.
const ROIMinTriaged = 10

// --- inputs ---

type NewRequirement struct {
	ExternalKey string
	Title       string
	Priority    Priority // "" = p2
}

type RequirementPatch struct {
	Title    *string
	Priority *Priority
	Status   *RequirementStatus
}

type NewTestCase struct {
	Preconditions string
	TestData      string
	Actor         string
	SuiteID       string
	ExternalKey   string
	Title         string
	Steps         []Step
	Expected      string
	Priority      Priority // "" = p2
	IsCritical    bool
	OwnerID       string
}

type TestCasePatch struct {
	Preconditions     *string
	TestData          *string
	ExpectedUpdatedAt time.Time
	Actor             string
	SuiteID           *string
	Title             *string
	Steps             *[]Step
	Expected          *string
	Priority          *Priority
	IsCritical        *bool
	AutomationStatus  *AutomationStatus
	OwnerID           *string
}

type CaseFilter struct {
	SuiteID          string
	AutomationStatus AutomationStatus
	Critical         *bool
	Query            string // title/external key substring
	Limit            int
}

type CaseExport struct {
	CSV        string    `json:"csv"`
	Count      int       `json:"count"`
	ExportedAt time.Time `json:"exportedAt"`
}

type NewPlan struct {
	CaseIDs       *[]string // nil selects current active cases; empty creates an empty plan
	Actor         string
	Name          string
	EnvironmentID string
}

type NewExecution struct {
	CaseUpdatedAt time.Time // optional optimistic check against the case displayed by the client
	TestCaseID    string
	Status        ExecutionStatus
	Note          string
	ExecutedAt    time.Time // zero = now
	StepResults   []StepResult
	BugURL        string
	RetestOf      string
}

type NewRelease struct {
	Version string
	Branch  string
}

// Decision is a human's word on a release; it may override the gate, and
// the name is recorded (§6.8.4 #1).
type Decision struct {
	Status ReleaseStatus // blocked | approved | shipped
	Note   string
}

// ImportReport is what a CSV import did (§6.8.5, CSV first).
type ImportReport struct {
	Created      int
	Updated      int
	Skipped      int      // rows without a title
	Errors       []string // "row 7: priority \"urgent\" not in p0..p3" — the row is skipped
	Suites       int      // suites created on the way
	Requirements int      // requirements created from the requirement column
}

var (
	ErrNotFound = errors.New("testmgmt: not found")
	ErrInvalid  = errors.New("testmgmt: invalid input")
	// ErrConflict: the write contradicts state — duplicate version, link to
	// an identity that is merged/retired, decision on a shipped release.
	ErrConflict = errors.New("testmgmt: conflict")
)

// Service is the contract. Every method is scoped by projectID; an id from
// another project answers ErrNotFound (§11.3.3: the tenant condition is in
// the query, never a filter afterwards).
type Service interface {
	// PurgeProject deletes everything this module keeps for a project that
	// tenancy has soft-deleted and whose restore window has passed (§11.3.2
	// "xóa project"); cmd/worker calls every module, tenancy last. Idempotent:
	// a purge cut short is finished by the next one.
	PurgeProject(ctx context.Context, projectID string) error

	// Suites
	CreateSuite(ctx context.Context, projectID, parentID, name string) (Suite, error)
	ListSuites(ctx context.Context, projectID string) ([]Suite, error)

	// Requirements
	CreateRequirement(ctx context.Context, projectID string, in NewRequirement) (Requirement, error)
	UpdateRequirement(ctx context.Context, projectID, id string, p RequirementPatch) (Requirement, error)
	ListRequirements(ctx context.Context, projectID string, limit int) ([]Requirement, error)
	GetRequirement(ctx context.Context, projectID, id string) (Requirement, error)

	// Test cases and their two kinds of link
	CreateTestCase(ctx context.Context, projectID string, in NewTestCase) (TestCase, error)
	UpdateTestCase(ctx context.Context, projectID, id string, p TestCasePatch) (TestCase, error)
	ListTestCases(ctx context.Context, projectID string, f CaseFilter) ([]TestCase, error)
	ExportCases(ctx context.Context, projectID string, f CaseFilter, ids []string) (CaseExport, error)
	GetTestCase(ctx context.Context, projectID, id string) (TestCase, error)
	// LinkAutomation ties a case to a TestIdentity of the same project. The
	// identity must be active (a merged or retired one answers ErrConflict).
	// The case becomes `automated` on its first link.
	LinkAutomation(ctx context.Context, projectID, testCaseID, stableTestID string, t LinkType, actor string) (Link, error)
	UnlinkAutomation(ctx context.Context, projectID, testCaseID, linkID string) error
	Cover(ctx context.Context, projectID, requirementID, testCaseID string) error
	Uncover(ctx context.Context, projectID, requirementID, testCaseID string) error
	// ImportCSV upserts test cases by external key (or title when the key
	// column is absent). Header names are case-insensitive: key, title,
	// suite, steps, expected, priority, critical, requirement.
	ImportCSV(ctx context.Context, projectID string, csvText string) (ImportReport, error)
	ImportCSVAs(ctx context.Context, projectID string, csvText, actor string) (ImportReport, error)

	// Plans and manual executions
	CreatePlan(ctx context.Context, projectID string, in NewPlan) (Plan, error)
	ListPlans(ctx context.Context, projectID string) ([]Plan, error)
	GetPlan(ctx context.Context, projectID, id string) (Plan, error)
	SetPlanStatus(ctx context.Context, projectID, id string, status PlanStatus) (Plan, error)
	UpdatePlan(ctx context.Context, projectID, id string, patch PlanPatch, actor string) (Plan, error)
	RecordExecution(ctx context.Context, projectID, planID string, in NewExecution, actor string) (Execution, error)
	// ExecutionForEvidence resolves one execution of one plan for an
	// evidence upload (§6.8.2 "Artifact dùng lại"): the artifact row is
	// artifact's, so this is the only place the ownership and the closed-plan
	// rule are checked. ErrNotFound for a foreign id, ErrConflict when the
	// plan is closed — the same rule as recording.
	ExecutionForEvidence(ctx context.Context, projectID, planID, id string) (Execution, error)

	// Releases and the gate
	CreateRelease(ctx context.Context, projectID string, in NewRelease) (Release, error)
	ListReleases(ctx context.Context, projectID string) ([]Release, error)
	GetRelease(ctx context.Context, projectID, id string) (Release, error)
	// Evaluate runs the §6.8.4 policy against the newest finished run and
	// stores the result on the release. It never changes Release.Status:
	// the gate advises, a person decides.
	Evaluate(ctx context.Context, projectID, id string) (Release, error)
	Decide(ctx context.Context, projectID, id string, d Decision, actor string) (Release, error)
	// ROI (§6.8.3, QĐ-13). AddBaseline records a measured baseline (§5.4)
	// with who entered it; ROI computes one calendar month (UTC) — the month
	// holding `month`.
	AddBaseline(ctx context.Context, projectID string, in NewBaseline, actor string) (Baseline, error)
	Baselines(ctx context.Context, projectID string) ([]Baseline, error)
	ROI(ctx context.Context, projectID string, month time.Time) (ROI, error)

	// ProjectOfRelease is for the CI gate route, whose URL carries no project:
	// the caller authorizes against the answer before reading anything.
	ProjectOfRelease(ctx context.Context, releaseID string) (projectID string, err error)
}
