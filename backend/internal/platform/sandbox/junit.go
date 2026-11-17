package sandbox

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

type xSuites struct {
	XMLName xml.Name `xml:"testsuites"`
	Suites  []xSuite `xml:"testsuite"`
}

type xSuite struct {
	Name     string  `xml:"name,attr"`
	Tests    int     `xml:"tests,attr"`
	Failures int     `xml:"failures,attr"`
	Errors   int     `xml:"errors,attr"`
	Cases    []xCase `xml:"testcase"`
}

type xCase struct {
	Name      string    `xml:"name,attr"`
	ClassName string    `xml:"classname,attr"`
	File      string    `xml:"file,attr,omitempty"`
	Failure   *xProblem `xml:"failure,omitempty"`
	Error     *xProblem `xml:"error,omitempty"`
	SystemOut string    `xml:"system-out,omitempty"`
}

type xProblem struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func render(s xSuite) []byte {
	for _, c := range s.Cases {
		s.Tests++
		if c.Failure != nil {
			s.Failures++
		}
		if c.Error != nil {
			s.Errors++
		}
	}
	out, _ := xml.MarshalIndent(xSuites{Suites: []xSuite{s}}, "", "  ")
	return append([]byte(xml.Header), out...)
}

// RunError is a report of a run that made no report of its own: one test
// in error, so the failure shows where every result does.
func RunError(kind, entry, message, log string) []byte {
	return render(xSuite{Name: kind + " · " + entry, Cases: []xCase{{
		Name: kind + " run", ClassName: kind, File: entry,
		Error: &xProblem{Message: cut(message, 500), Body: Tail(log, 16<<10)},
	}}})
}

// k6's --summary-export: metrics with their thresholds, and the checks of
// the root group and its groups (a map in older k6, a list in newer).
type k6Summary struct {
	Metrics   map[string]map[string]json.RawMessage `json:"metrics"`
	RootGroup k6Group                               `json:"root_group"`
}

type k6Group struct {
	Name   string          `json:"name"`
	Path   string          `json:"path"`
	Checks json.RawMessage `json:"checks"`
	Groups json.RawMessage `json:"groups"`
}

type k6Check struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Passes int    `json:"passes"`
	Fails  int    `json:"fails"`
}

// mapOrList reads {"k": v, …} or [v, …] into a list, in a stable order.
func mapOrList[T any](raw json.RawMessage) []T {
	if len(raw) == 0 {
		return nil
	}
	var list []T
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var m map[string]T
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		list = append(list, m[k])
	}
	return list
}

func (g k6Group) checks(depth int) []k6Check {
	out := mapOrList[k6Check](g.Checks)
	if depth > 8 {
		return out
	}
	for _, sub := range mapOrList[k6Group](g.Groups) {
		out = append(out, sub.checks(depth+1)...)
	}
	return out
}

// K6JUnit turns a k6 summary into a JUnit report: one test per threshold
// ("http_req_duration p(95)<500") and per check, and one for the run when
// k6 failed for another reason. k6's exit code is the judge of the
// thresholds (99: some crossed), so a summary format that changes how it
// marks them cannot turn a failed run green.
func K6JUnit(entry string, summary []byte, exitCode int, log string) []byte {
	var s k6Summary
	if err := json.Unmarshal(summary, &s); err != nil {
		return RunError(KindK6, entry, fmt.Sprintf("k6 exited %d without a summary", exitCode), log)
	}
	suite := xSuite{Name: "k6 · " + entry}
	crossed := 0
	names := make([]string, 0, len(s.Metrics))
	for m := range s.Metrics {
		names = append(names, m)
	}
	sort.Strings(names)
	for _, metric := range names {
		raw, ok := s.Metrics[metric]["thresholds"]
		if !ok {
			continue
		}
		var th map[string]json.RawMessage
		if json.Unmarshal(raw, &th) != nil {
			continue
		}
		exprs := make([]string, 0, len(th))
		for e := range th {
			exprs = append(exprs, e)
		}
		sort.Strings(exprs)
		for _, expr := range exprs {
			c := xCase{Name: metric + " " + expr, ClassName: "k6.threshold", File: entry}
			// Legacy export: true = crossed. handleSummary shape: {ok: false}.
			failed := false
			var b bool
			var o struct {
				OK *bool `json:"ok"`
			}
			if json.Unmarshal(th[expr], &b) == nil {
				failed = b
			} else if json.Unmarshal(th[expr], &o) == nil && o.OK != nil {
				failed = !*o.OK
			}
			if failed && exitCode == 99 {
				c.Failure = &xProblem{Message: metric + " crossed " + expr, Body: metricLine(s.Metrics[metric])}
				crossed++
			}
			suite.Cases = append(suite.Cases, c)
		}
	}
	if exitCode == 99 && crossed == 0 {
		suite.Cases = append(suite.Cases, xCase{Name: "k6 thresholds", ClassName: "k6.threshold", File: entry,
			Failure: &xProblem{Message: "k6 reported thresholds crossed (exit 99)", Body: Tail(log, 16<<10)}})
	}
	for _, ch := range s.RootGroup.checks(0) {
		name := ch.Path
		if name == "" {
			name = ch.Name
		}
		name = strings.TrimPrefix(strings.ReplaceAll(name, "::", " › "), " › ")
		c := xCase{Name: "check " + name, ClassName: "k6.check", File: entry}
		if ch.Fails > 0 {
			c.Failure = &xProblem{Message: fmt.Sprintf("%d of %d failed", ch.Fails, ch.Fails+ch.Passes)}
		}
		suite.Cases = append(suite.Cases, c)
	}
	if exitCode != 0 && exitCode != 99 {
		suite.Cases = append(suite.Cases, xCase{Name: "k6 run", ClassName: "k6", File: entry,
			Error: &xProblem{Message: fmt.Sprintf("k6 exited %d", exitCode), Body: Tail(log, 16<<10)}})
	}
	if len(suite.Cases) == 0 {
		suite.Cases = append(suite.Cases, xCase{Name: "k6 run", ClassName: "k6", File: entry})
	}
	return render(suite)
}

func metricLine(m map[string]json.RawMessage) string {
	keys := []string{}
	for k := range m {
		if k != "thresholds" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+string(m[k]))
	}
	return strings.Join(parts, " ")
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
