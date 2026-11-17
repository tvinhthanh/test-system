// Package sandbox is the contract between the worker and sandboxd, the
// service that runs a team's own test scripts — a Playwright project or a
// k6 script — in a throwaway container (QĐ-19 tier 4).
//
// The worker never touches Docker. It sends sandboxd one Job: the archive,
// what kind of script it is and the environment's address; sandboxd runs it
// on a network of its own with no way out but a forwarder to that one host
// (guard.go, forward.go), and answers with the exit code, the log's tail and
// the report files (JUnit for Playwright, k6's summary). Nothing is kept
// between two runs.
package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/outbound"
)

// Kinds of script.
const (
	KindPlaywright = "playwright"
	KindK6         = "k6"
)

// Limits shared by the api (on upload), the worker and sandboxd.
const (
	MaxArchiveBytes = 10 << 20  // the .tar.gz as uploaded
	MaxUnpacked     = 100 << 20 // its files, unpacked
	MaxFiles        = 5000
	MaxEntries      = 2 * MaxFiles // files and folders together
	MaxLogBytes     = 64 << 10     // the tail of the script's output kept
	MaxReportBytes  = 8 << 20      // one report file
)

// Job is one script run.
type Job struct {
	ID      string `json:"id"`      // the script run's id: names the container and network
	Kind    string `json:"kind"`    // playwright | k6
	Base    string `json:"base"`    // the environment's address, e.g. https://staging.example.com
	Archive []byte `json:"archive"` // .tar.gz (base64 in JSON)
	// Entry: the k6 script, or the Playwright config ("" = the project's
	// default playwright.config.*), a path inside the archive.
	Entry string `json:"entry"`
	// Playwright only: --project and --grep.
	Project string `json:"project,omitempty"`
	Grep    string `json:"grep,omitempty"`
	// k6 only: they replace the script's own scenarios (CLI shortcuts win).
	VUs     int `json:"vus,omitempty"`
	Seconds int `json:"seconds,omitempty"`
	// Timeout ends the run (the container is killed).
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// Result is what came out.
type Result struct {
	ExitCode int    `json:"exitCode"`
	TimedOut bool   `json:"timedOut"`
	Log      string `json:"log"` // the tail of stdout+stderr
	// Files: junit.xml (Playwright), summary.json (k6); absent when the
	// script made none.
	Files map[string][]byte `json:"files"`
	// Error: the run could not happen (bad archive, image missing …) —
	// said to the tester as is.
	Error string `json:"error,omitempty"`
}

// Runner runs a job (Client, or a fake in tests).
type Runner interface {
	Run(ctx context.Context, job Job) (Result, error)
}

// Client calls sandboxd at a fixed address (configuration, never a
// request's) with its shared token, through platform/outbound.
type Client struct {
	url   string
	token string
	http  *outbound.Client
}

// NewClient is the worker's client; maxSeconds is the longest run it asks
// for (SCRIPT_MAX_SECONDS), which bounds the wait for an answer.
func NewClient(baseURL, token string, maxSeconds int) (*Client, error) {
	oc, err := outbound.New(time.Duration(maxSeconds)*time.Second+5*time.Minute, baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{url: strings.TrimSuffix(baseURL, "/"), token: token, http: oc.WithMaxBody(64 << 20)}, nil
}

var ErrBusy = errors.New("sandbox: sandboxd is busy")

func (c *Client) Run(ctx context.Context, job Job) (Result, error) {
	body, err := json.Marshal(job)
	if err != nil {
		return Result{}, err
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Send(ctx, http.MethodPost, c.url+"/run", h, body)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusServiceUnavailable:
		return Result{}, ErrBusy
	default:
		return Result{}, fmt.Errorf("sandbox: sandboxd answered %d: %s", resp.StatusCode, strings.TrimSpace(string(raw[:min(len(raw), 300)])))
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("sandbox: sandboxd's answer: %w", err)
	}
	return out, nil
}

// Tail keeps the last n bytes of s, on a line start when it can, as text
// Postgres stores (Clean): a script's output is whatever it printed.
func Tail(s string, n int) string {
	s = Clean(s)
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 && i < 200 {
		s = s[i+1:]
	}
	for len(s) > 0 && !utf8.RuneStart(s[0]) { // never half a character
		s = s[1:]
	}
	return "…\n" + s
}

// Clean makes s storable as Postgres text: valid UTF-8, no NUL bytes
// (either one fails the whole UPDATE). It never grows s.
func Clean(s string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(s, ""), "\x00", "")
}
