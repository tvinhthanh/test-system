package identity

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Loader for the Playwright JSON reporter.
// The schema below was read off a REAL report (testdata/playwright-report.json), not guessed.

type pwReport struct {
	Suites []pwSuite `json:"suites"`
	Stats  pwStats   `json:"stats"`
}

type pwStats struct {
	Expected   int `json:"expected"`
	Skipped    int `json:"skipped"`
	Unexpected int `json:"unexpected"`
	Flaky      int `json:"flaky"`
}

type pwSuite struct {
	Title  string    `json:"title"`
	File   string    `json:"file"`
	Specs  []pwSpec  `json:"specs"`
	Suites []pwSuite `json:"suites"`
}

type pwSpec struct {
	Title string   `json:"title"`
	ID    string   `json:"id"`
	Tests []pwTest `json:"tests"`
}

type pwTest struct {
	ProjectName    string     `json:"projectName"`
	Status         string     `json:"status"`         // expected | unexpected | skipped | flaky
	ExpectedStatus string     `json:"expectedStatus"` // passed | skipped | failed
	Results        []pwResult `json:"results"`
}

type pwResult struct {
	Status   string `json:"status"`
	Duration int    `json:"duration"`
	Retry    int    `json:"retry"`
}

// LoadedTest is one (spec x project) entry normalised into the SPEC model.
type LoadedTest struct {
	Identity     Identity
	PlaywrightID string // spec.id, Playwright's own identifier
	FinalStatus  string // per the §8.4 enum
	// AttemptStatus holds one entry per retry, in the §8.4 TestResultAttempt
	// enum. §8.4 models each attempt as its own entity, so the reporter keeps
	// them separate rather than reporting only the final verdict.
	AttemptStatus []string
	Attempts      int
	DurationMs    int
}

// Playwright exposes TWO status vocabularies at two different levels, and they
// are not the same enum (finding N41):
//
//	test.status      : expected | unexpected | skipped | flaky
//	results[].status : passed | failed | timedOut | skipped | interrupted
//
// §8.4's finalStatus enum includes timed_out and interrupted, and NEITHER is
// visible at test level — a timeout surfaces as test.status "unexpected".
// Mapping from test.status alone silently collapses timeouts into plain
// failures, which destroys the signal §8.7's failure taxonomy needs to tell
// flaky_timing apart from a real assertion failure.
//
// So the mapping must read both levels.

// mapAttemptStatus converts one results[] entry to the §8.4 attempt enum.
func mapAttemptStatus(s string) string {
	switch s {
	case "passed":
		return "passed"
	case "failed":
		return "failed"
	case "timedOut":
		return "timed_out"
	case "skipped":
		return "skipped"
	case "interrupted":
		return "interrupted"
	default:
		return "interrupted"
	}
}

// mapStatus derives TestResult.finalStatus (§8.4) from the test-level verdict
// plus the final attempt, so timeouts and interruptions survive.
//
// Playwright determines `flaky` itself within a run; the reporter carries that
// value through and never re-derives it (N36). FlakySignal (§8.6) does the
// cross-run aggregation — a separate job.
func mapStatus(t pwTest) string {
	last := ""
	if n := len(t.Results); n > 0 {
		last = mapAttemptStatus(t.Results[n-1].Status)
	}

	switch t.Status {
	case "flaky":
		return "flaky_pass"
	case "skipped":
		return "skipped"
	case "expected":
		if t.ExpectedStatus == "skipped" {
			return "skipped"
		}
		return "passed"
	case "unexpected":
		// Preserve the shape of the final attempt: timed_out and interrupted are
		// distinct outcomes in §8.4, not flavours of "failed".
		if last == "timed_out" || last == "interrupted" {
			return last
		}
		return "failed"
	default:
		return "interrupted"
	}
}

// LoadPlaywrightReport reads a JSON report and returns normalised tests.
func LoadPlaywrightReport(path string) ([]LoadedTest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read report: %w", err)
	}
	var r pwReport
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}
	var out []LoadedTest
	for _, s := range r.Suites {
		walkSuite(s, nil, s.File, &out)
	}
	return out, nil
}

// walkSuite descends the suite tree, accumulating titlePath.
//
// CAREFUL: the file-level suite carries the FILE PATH as its title, and on
// Windows that path uses backslashes. It must be excluded from titlePath, or
// the fingerprint duplicates FilePath and becomes OS-dependent.
func walkSuite(s pwSuite, path []string, file string, out *[]LoadedTest) {
	p := path
	if s.Title != "" && s.Title != file && !isFileTitle(s.Title) {
		p = append(append([]string{}, path...), s.Title)
	}
	f := file
	if s.File != "" {
		f = s.File
	}
	for _, spec := range s.Specs {
		for _, t := range spec.Tests {
			dur := 0
			for _, res := range t.Results {
				dur += res.Duration
			}
			*out = append(*out, LoadedTest{
				Identity: Identity{
					Framework: "playwright",
					FilePath:  f,
					TitlePath: append(append([]string{}, p...), spec.Title),
					// Playwright's projectName is the canonical parameterisation axis.
					ParamSignature: NormalizeParams(map[string]string{"project": t.ProjectName}),
				},
				PlaywrightID:  spec.ID,
				FinalStatus:   mapStatus(t),
				AttemptStatus: attemptStatuses(t),
				Attempts:      len(t.Results),
				DurationMs:    dur,
			})
		}
	}
	for _, c := range s.Suites {
		walkSuite(c, p, f, out)
	}
}

func isFileTitle(t string) bool {
	return strings.HasSuffix(t, ".ts") || strings.HasSuffix(t, ".js") ||
		strings.HasSuffix(t, ".tsx") || strings.HasSuffix(t, ".mjs")
}

// attemptStatuses maps every retry to the §8.4 TestResultAttempt enum.
// §8.4 models each attempt as its own entity, so the reporter must keep them
// separate rather than reporting only the final verdict.
func attemptStatuses(t pwTest) []string {
	out := make([]string, 0, len(t.Results))
	for _, r := range t.Results {
		out = append(out, mapAttemptStatus(r.Status))
	}
	return out
}
