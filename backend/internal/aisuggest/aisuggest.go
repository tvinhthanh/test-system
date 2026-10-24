// Package aisuggest drafts test cases with a language model (QĐ-15): a
// tester picks a test type, names the site (or an environment) and says in
// a few words what to cover; the worker reads the page's outline through the
// URL checker (SSRF-guarded, never fetched here), asks the model for cases
// and stores the drafts. Nothing becomes a test case until the tester picks
// a draft — the dashboard then creates it through testmgmt like any other.
//
// The model is always the organization's choice in the dashboard (default
// llm.DefaultModel). Whose key pays is its choice too: its own Anthropic API
// key (ai_settings, sealed with platform/secretbox) or the server's
// LLM_API_KEY. No usable key: the feature says so instead of queueing work
// that cannot run.
//
// Owns ai_settings and case_suggestions (migration 0039). Imports only
// platform/*. This file is the module's whole contract (§6.9.4).
package aisuggest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
)

// JobDraft is the queue kind of one suggestion; the key is its id.
const JobDraft = "aisuggest.draft"

const (
	// MaxQueued suggestions may wait per project at once.
	MaxQueued = 2
	// DailyLimit suggestions per project in 24 hours — each one is a paid
	// model call on someone's key.
	DailyLimit = 30
	// MaxCount drafts per suggestion.
	MaxCount = 15
	// Retention before the worker prunes a suggestion.
	Retention = 90 * 24 * time.Hour
)

// TestTypes are the case taxonomy's usage tags and purposes (migration
// 0033) — what the start flow offers and what a plan records (0038).
var TestTypes = []string{"smoke", "regression", "e2e", "functional", "performance", "security", "usability", "accessibility", "compatibility", "recovery"}

// ValidTestType reports whether s is one of TestTypes.
func ValidTestType(s string) bool {
	for _, t := range TestTypes {
		if t == s {
			return true
		}
	}
	return false
}

// Key sources.
const (
	SourceOrganization = "organization"
	SourceServer       = "server"
)

// Settings is an organization's choices: the model, and whether it pays
// with its own key (only the key's hint is readable) or the server's.
type Settings struct {
	OrgID     string
	Model     string
	OwnKey    bool
	KeyHint   string // the own key's last four characters; "" with the server's
	UpdatedBy string
	UpdatedAt time.Time
}

// SaveInput is one save of the settings page. OwnKey with APIKey ""
// keeps the stored key; OwnKey false forgets any stored key.
type SaveInput struct {
	Model  string
	OwnKey bool
	APIKey string
	Actor  string
}

// Access is what a project of the organization would use now.
type Access struct {
	Available bool
	Source    string // SourceOrganization | SourceServer | ""
	Model     string
}

type Step struct {
	Action   string `json:"action"`
	Expected string `json:"expected"`
}

// Draft is one proposed test case. Its text came from a model that read an
// untrusted page: the dashboard shows it as text and the tester decides.
type Draft struct {
	Title         string `json:"title"`
	Priority      string `json:"priority"` // p0..p3
	Preconditions string `json:"preconditions"`
	Steps         []Step `json:"steps"`
	Why           string `json:"why"`
}

const (
	StatusQueued = "queued"
	StatusDone   = "done"
	StatusError  = "error"
)

type Suggestion struct {
	ID            string
	ProjectID     string
	EnvironmentID string
	TestType      string
	URL           string
	Context       string
	Count         int
	Status        string
	Drafts        []Draft
	PageRead      bool // the page outline reached the model
	Model         string
	KeySource     string
	InputTokens   int
	OutputTokens  int
	Error         string
	RequestedBy   string
	CreatedAt     time.Time
	FinishedAt    time.Time
}

type NewSuggestion struct {
	ProjectID     string
	OrgID         string // the project's organization: whose key pays
	EnvironmentID string
	TestType      string
	URL           string // "" = no page to read
	Context       string // what the tester wrote, ≤ 4000 bytes
	Count         int    // 0 = 8
	Actor         string
}

// LLM is what this module needs of platform/llm.
type LLM interface {
	JSON(ctx context.Context, req llm.Request) (llm.Result, error)
	CheckKey(ctx context.Context, apiKey, model string) error
}

// PageReader returns a page's outline (title, headings, links, forms,
// buttons, some text) from the URL checker. nil: pages are not read
// (URLCHECK=off) and drafts come from the description alone.
type PageReader interface {
	Outline(ctx context.Context, url string) (json.RawMessage, error)
}

type Deps struct {
	DB        *pg.DB
	Pepper    []byte // TOKEN_PEPPER: seals organizations' keys
	LLM       LLM
	Pages     PageReader
	ServerKey string // LLM_API_KEY; "" = none
}

var (
	ErrNotFound = errors.New("aisuggest: not found")
	ErrInvalid  = errors.New("aisuggest: invalid input")
	// ErrNotConfigured: neither the organization nor the server has a key.
	ErrNotConfigured = errors.New("aisuggest: no API key configured")
	// ErrBusy: MaxQueued waiting, or DailyLimit reached, in the project.
	ErrBusy = errors.New("aisuggest: too many suggestions")
)

type Service interface {
	// Settings returns the organization's choices; false when it made none
	// (the server's key, if any, with llm.DefaultModel).
	Settings(ctx context.Context, orgID string) (Settings, bool, error)
	// SaveSettings stores the choices after checking with the provider that
	// the key that will pay may use the model (llm.ErrKey / llm.ErrModel
	// otherwise); nothing is stored on a refusal.
	SaveSettings(ctx context.Context, orgID string, in SaveInput) (Settings, error)
	// DeleteSettings forgets the choices and any stored key; false if none.
	DeleteSettings(ctx context.Context, orgID string) (bool, error)
	Access(ctx context.Context, orgID string) (Access, error)
	// Credentials is the key and model a job of the organization uses now,
	// for other modules' jobs (autotest, QĐ-16): the organization's own key,
	// else the server's. ok false: there is no key anywhere.
	Credentials(ctx context.Context, orgID string) (key, model, source string, ok bool, err error)
	// Request queues a suggestion with its job, after the key and limit checks.
	Request(ctx context.Context, in NewSuggestion) (Suggestion, error)
	Get(ctx context.Context, projectID, id string) (Suggestion, error)
	// Worker consumes JobDraft; cmd/worker runs it.
	Worker(concurrency int) *queue.Worker
	Prune(ctx context.Context, before time.Time, limit int) (int, error)
	PurgeProject(ctx context.Context, projectID string) error
	PurgeOrganization(ctx context.Context, orgID string) error
}
