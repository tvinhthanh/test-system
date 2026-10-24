package autotest

import (
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// Framework is the identity namespace of auto-run tests in the results
// plane: the JUnit import files them under it.
const Framework = "atp-auto"

// CaseFile is the file a case's test is filed under — its case id, so the
// test keeps its identity (and history) when the case is renamed.
func CaseFile(caseID string) string { return "atp-auto/" + caseID }

type junitSuites struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name      string      `xml:"name,attr"`
	Tests     int         `xml:"tests,attr"`
	Failures  int         `xml:"failures,attr"`
	Errors    int         `xml:"errors,attr"`
	Time      string      `xml:"time,attr"`
	Timestamp string      `xml:"timestamp,attr"`
	Cases     []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	File      string        `xml:"file,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitProblem `xml:"failure,omitempty"`
	Error     *junitProblem `xml:"error,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

type junitProblem struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

// JUnit is a finished run as a JUnit report: one testcase per case, the
// step log as the failure text, console errors as system-out. The JUnit
// import (ingest) turns it into a test run.
func JUnit(run Run, envKey string) []byte {
	started := run.StartedAt
	if started.IsZero() {
		started = run.CreatedAt
	}
	finished := run.FinishedAt
	if finished.IsZero() {
		finished = time.Now().UTC()
	}
	suite := junitSuite{
		Name:      "ATP auto test · " + envKey,
		Timestamp: started.UTC().Format("2006-01-02T15:04:05"),
		Time:      seconds(int(finished.Sub(started).Milliseconds())),
	}
	for _, c := range run.Cases {
		if c.Status == CasePending {
			continue
		}
		jc := junitCase{Name: c.Title, ClassName: Framework, File: CaseFile(c.CaseID), Time: seconds(c.DurationMs)}
		log := stepLog(c)
		switch c.Status {
		case CaseFailed:
			jc.Failure = &junitProblem{Message: cut(c.Error, 500), Type: "step", Body: log}
			suite.Failures++
		case CaseError:
			jc.Error = &junitProblem{Message: cut(c.Error, 500), Type: "setup", Body: log}
			suite.Errors++
		}
		if len(c.Console) > 0 {
			jc.SystemOut = strings.Join(c.Console, "\n")
		}
		suite.Cases = append(suite.Cases, jc)
		suite.Tests++
	}
	b, _ := xml.MarshalIndent(junitSuites{Suites: []junitSuite{suite}}, "", "  ")
	return append([]byte(xml.Header), b...)
}

func seconds(ms int) string { return fmt.Sprintf("%.3f", float64(max(ms, 0))/1000) }

// stepLog is the case's steps with what happened to each, for a person
// reading the failure.
func stepLog(c RunCase) string {
	var b strings.Builder
	for i, st := range c.Spec.Steps {
		status := "not run"
		if i < len(c.Results) {
			status = c.Results[i].Status
		}
		fmt.Fprintf(&b, "%d. [%s] %s", i+1, status, sayOf(st))
		if i < len(c.Results) {
			r := c.Results[i]
			if r.Healed != nil {
				fmt.Fprintf(&b, " — repaired by AI: %s", r.Healed.Reason)
			} else if r.Error != "" {
				fmt.Fprintf(&b, " — %s", r.Error)
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
