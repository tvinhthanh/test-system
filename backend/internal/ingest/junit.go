package ingest

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// JUnit XML backfill (§6.8.5, QĐ-13): one report file becomes one historic
// run, built from the same events a reporter sends, so it flows straight
// into §8.4 and through the same tier-1/tier-2 resolve, flaky fold and
// TTRC as live data. This file is the parser — pure, no I/O.
//
// Mapping, decided once (GATES "JUnit backfill"):
//
//	<testcase>                         one TestResult
//	  file="…"                         identity.filePath; else classname with
//	                                   dots as slashes (Java: a class is a file)
//	  classname beyond the file        the describes (pytest: tests.test_pay.TestCard
//	                                   with file tests/test_pay.py → ["TestCard"])
//	  name="pay[visa-3ds]"             title "pay", paramSignature "param=visa-3ds" —
//	                                   JUnit has no parameters (§6.8.5), a trailing
//	                                   [...] is where pytest and JUnit 5 put them
//	  <failure>/<error>                failed; type or message naming a timeout → timed_out
//	  <skipped>                        skipped
//	  none                             passed
//	  <flakyFailure>/<flakyError>      Surefire reruns that failed before the pass:
//	                                   earlier attempts, outcome flaky (N36: the
//	                                   framework decided, we carry it)
//	  <rerunFailure>/<rerunError>      Surefire reruns of a test that never passed:
//	                                   more failed attempts, outcome unexpected
//
// Times: a testsuite's timestamp (UTC when it names no zone) plus the
// durations of the cases before, since JUnit has no per-case start. A file
// with no timestamp needs startedAt from the caller — history dated "now"
// would be wrong in every trend.

// MaxJUnitBytes caps one report (a big Surefire aggregate is a few MB).
const MaxJUnitBytes = 20 << 20

// ErrJUnit is a report that cannot be read; the message says where.
var ErrJUnit = errors.New("junit")

type junitSuites struct {
	Suites []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name      string       `xml:"name,attr"`
	Timestamp string       `xml:"timestamp,attr"`
	File      string       `xml:"file,attr"`
	Cases     []junitCase  `xml:"testcase"`
	Suites    []junitSuite `xml:"testsuite"` // nested suites (Jest, some Ant reports)
}

type junitCase struct {
	Name          string         `xml:"name,attr"`
	ClassName     string         `xml:"classname,attr"`
	File          string         `xml:"file,attr"`
	Time          string         `xml:"time,attr"`
	Failures      []junitProblem `xml:"failure"`
	Errors        []junitProblem `xml:"error"`
	Skipped       *struct{}      `xml:"skipped"`
	FlakyFailures []junitProblem `xml:"flakyFailure"`
	FlakyErrors   []junitProblem `xml:"flakyError"`
	RerunFailures []junitProblem `xml:"rerunFailure"`
	RerunErrors   []junitProblem `xml:"rerunError"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
	Stack   string `xml:"stackTrace"` // Surefire puts the rerun's stack here
	Time    string `xml:"time,attr"`
}

// junitReport is a parsed file: its attempts, in file order, and the span
// they cover.
type junitReport struct {
	Attempts   []results.AttemptEvent
	Tests      int
	StartedAt  time.Time
	FinishedAt time.Time
}

// parseJUnit reads a report. framework names the identity namespace
// (default "junit"); startedAt dates a file that carries no timestamp.
func parseJUnit(body []byte, framework string, startedAt time.Time) (junitReport, error) {
	suites, err := decodeJUnit(body)
	if err != nil {
		return junitReport{}, err
	}
	var rep junitReport
	var seq int64
	occurrences := map[string]int{}
	var walk func(s junitSuite, file string, clock time.Time) (time.Time, error)
	walk = func(s junitSuite, file string, clock time.Time) (time.Time, error) {
		if s.Timestamp != "" {
			t, err := parseJUnitTime(s.Timestamp)
			if err != nil {
				return clock, fmt.Errorf("%w: testsuite %q timestamp %q: want ISO 8601", ErrJUnit, s.Name, s.Timestamp)
			}
			clock = t
		}
		if s.File != "" {
			file = s.File
		}
		for _, c := range s.Cases {
			if clock.IsZero() {
				return clock, fmt.Errorf("%w: no timestamp on testsuite %q — pass startedAt (the date the run happened)", ErrJUnit, s.Name)
			}
			if rep.StartedAt.IsZero() || clock.Before(rep.StartedAt) {
				rep.StartedAt = clock
			}
			attempts, end, err := junitAttempts(c, s, file, framework, clock)
			if err != nil {
				return clock, err
			}
			id := attempts[0].ClientTestID
			for i := range attempts {
				seq++
				attempts[i].Sequence = seq
				attempts[i].OccurrenceIndex = occurrences[id]
			}
			occurrences[id]++
			rep.Attempts = append(rep.Attempts, attempts...)
			rep.Tests++
			clock = end
			if end.After(rep.FinishedAt) {
				rep.FinishedAt = end
			}
		}
		for _, child := range s.Suites {
			next, err := walk(child, file, clock)
			if err != nil {
				return clock, err
			}
			clock = next
		}
		return clock, nil
	}
	for _, s := range suites {
		if _, err := walk(s, "", startedAt); err != nil {
			return junitReport{}, err
		}
	}
	if rep.Tests == 0 {
		return junitReport{}, fmt.Errorf("%w: no <testcase> in the report", ErrJUnit)
	}
	return rep, nil
}

// decodeJUnit accepts <testsuites> or a bare <testsuite> root.
func decodeJUnit(body []byte) ([]junitSuite, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	// XML entities beyond the five predefined ones are refused by
	// encoding/xml (no DTD, no external entities — no XXE, no billion
	// laughs); Strict stays on.
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: no XML element — not a JUnit report", ErrJUnit)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: not XML: %v", ErrJUnit, err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "testsuites":
			var all junitSuites
			if err := dec.DecodeElement(&all, &start); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrJUnit, err)
			}
			return all.Suites, nil
		case "testsuite":
			var one junitSuite
			if err := dec.DecodeElement(&one, &start); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrJUnit, err)
			}
			return []junitSuite{one}, nil
		default:
			return nil, fmt.Errorf("%w: root element <%s>, want <testsuites> or <testsuite>", ErrJUnit, start.Name.Local)
		}
	}
}

// parseJUnitTime reads the timestamps reports write: RFC 3339, or the
// Surefire/Ant form with no zone (taken as UTC).
func parseJUnitTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errors.New("bad time")
}

func seconds(s string) time.Duration {
	f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 64)
	if err != nil || f < 0 || f > 7*24*3600 {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

// junitSignals is §7.3 identity for one testcase (mapping in the header).
func junitSignals(c junitCase, s junitSuite, file, framework string) (identity.Signals, error) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return identity.Signals{}, fmt.Errorf("%w: a <testcase> in %q has no name", ErrJUnit, s.Name)
	}
	param := ""
	if i := strings.LastIndex(name, "["); i > 0 && strings.HasSuffix(name, "]") {
		param, name = "param="+name[i+1:len(name)-1], strings.TrimSpace(name[:i])
	}
	class := strings.TrimSpace(c.ClassName)
	if c.File != "" {
		file = c.File
	}
	var describes []string
	switch {
	case file != "":
		describes = classDescribes(class, name, file)
	case class != "":
		file = strings.ReplaceAll(class, ".", "/")
	case s.Name != "":
		file = s.Name
	default:
		return identity.Signals{}, fmt.Errorf("%w: testcase %q has no file, classname or suite name to place it", ErrJUnit, name)
	}
	title := append(describes, name)
	if framework == "" {
		framework = "junit"
	}
	return identity.Signals{Framework: framework, FilePath: file, TitlePath: title, ParamSignature: param}, nil
}

// classDescribes is what of classname is not already the file: the
// classes a test sits in. The classname is matched against the tail of the
// file's module path (tests/test_pay.py → tests.test_pay, test_pay;
// src/test/java/com/acme/PayTest.java → …, com.acme.PayTest), so pytest's
// "tests.test_pay.TestCard" gives ["TestCard"] and Java's
// "com.acme.PayTest" gives none — the file already names the class. A
// classname unrelated to the file (Jest writes "<describe> <title>") is a
// describe only when it is a plain name, not the title again.
func classDescribes(class, name, file string) []string {
	if class == "" {
		return nil
	}
	module := strings.TrimSuffix(strings.ReplaceAll(strings.ReplaceAll(file, "\\", "/"), "/", "."), extOf(file))
	parts := strings.Split(strings.Trim(module, "."), ".")
	for i := range parts {
		suffix := strings.Join(parts[i:], ".")
		if class == suffix {
			return nil
		}
		if strings.HasPrefix(class, suffix+".") {
			return strings.Split(strings.TrimPrefix(class, suffix+"."), ".")
		}
	}
	if strings.Contains(class, ".") || class == name || strings.HasSuffix(class, name) {
		return nil
	}
	return []string{class}
}

func extOf(p string) string {
	slash := strings.LastIndexAny(p, "/\\")
	if dot := strings.LastIndex(p, "."); dot > slash {
		return strings.ReplaceAll(p[dot:], "\\", "/")
	}
	return ""
}

// junitAttempts is one testcase's attempts, earliest first, starting at
// clock; it returns when the last one ended.
func junitAttempts(c junitCase, s junitSuite, file, framework string, clock time.Time) ([]results.AttemptEvent, time.Time, error) {
	sig, err := junitSignals(c, s, file, framework)
	if err != nil {
		return nil, clock, err
	}
	if len(sig.FilePath) > maxFilePath || len(sig.TitlePath) > maxTitlePath {
		return nil, clock, fmt.Errorf("%w: testcase %q: file path or class nesting too long", ErrJUnit, c.Name)
	}
	for _, t := range sig.TitlePath {
		if t == "" || len(t) > maxTitle {
			return nil, clock, fmt.Errorf("%w: testcase %q: empty or over-long name part", ErrJUnit, c.Name)
		}
	}
	raw, _ := json.Marshal(identityRaw{Framework: sig.Framework, FilePath: sig.FilePath, TitlePath: sig.TitlePath, ParamSignature: sig.ParamSignature})
	ct := sig.ClientTestID()
	total := seconds(c.Time)

	type step struct {
		status  results.AttemptStatus
		problem *junitProblem
		took    time.Duration
	}
	var steps []step
	final := step{status: results.AttemptPassed}
	outcome := results.OutcomeExpected
	failedAs := func(p junitProblem) results.AttemptStatus {
		if isTimeout(p) {
			return results.AttemptTimedOut
		}
		return results.AttemptFailed
	}
	switch {
	case c.Skipped != nil && len(c.Failures)+len(c.Errors) == 0:
		final.status, outcome = results.AttemptSkipped, results.OutcomeSkipped
	case len(c.Failures)+len(c.Errors) > 0:
		p := append(c.Failures, c.Errors...)[0]
		final = step{status: failedAs(p), problem: &p}
		outcome = results.OutcomeUnexpected
		for _, r := range append(c.RerunFailures, c.RerunErrors...) {
			r := r
			steps = append(steps, step{status: failedAs(r), problem: &r, took: seconds(r.Time)})
		}
	default:
		for _, f := range append(c.FlakyFailures, c.FlakyErrors...) {
			f := f
			steps = append(steps, step{status: failedAs(f), problem: &f, took: seconds(f.Time)})
		}
		if len(steps) > 0 {
			outcome = results.OutcomeFlaky
		}
	}
	// The testcase's time is the final attempt's in Surefire; reruns carry
	// their own.
	final.took = total
	steps = append(steps, final)

	out := make([]results.AttemptEvent, 0, len(steps))
	for i, st := range steps {
		ev := results.AttemptEvent{
			ClientTestID: ct,
			Attempt:      i,
			DisplayName:  sig.TitlePath[len(sig.TitlePath)-1],
			SuiteName:    strings.Join(sig.TitlePath[:len(sig.TitlePath)-1], " › "),
			IdentityRaw:  raw,
			Status:       st.status,
			Outcome:      outcome,
			DurationMs:   int(st.took / time.Millisecond),
			StartedAt:    clock,
			FinishedAt:   clock.Add(st.took),
		}
		if st.problem != nil {
			ev.Error, _ = json.Marshal(problemError(*st.problem).bounded())
		}
		out = append(out, ev)
		clock = clock.Add(st.took)
	}
	return out, clock, nil
}

func isTimeout(p junitProblem) bool {
	for _, s := range []string{p.Type, p.Message} {
		l := strings.ToLower(s)
		if strings.Contains(l, "timeout") || strings.Contains(l, "timed out") {
			return true
		}
	}
	return false
}

// problemError is the §8.4 error of an attempt: the message, the type in
// front of it when both exist, the body (or Surefire's stackTrace) as stack.
func problemError(p junitProblem) errorRaw {
	msg := strings.TrimSpace(p.Message)
	if t := strings.TrimSpace(p.Type); t != "" && !strings.HasPrefix(msg, t) {
		if msg == "" {
			msg = t
		} else {
			msg = t + ": " + msg
		}
	}
	stack := strings.TrimSpace(p.Text)
	if stack == "" {
		stack = strings.TrimSpace(p.Stack)
	}
	if msg == "" {
		msg, _, _ = strings.Cut(stack, "\n")
	}
	return errorRaw{Message: msg, Stack: stack}
}
