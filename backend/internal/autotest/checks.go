package autotest

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Check steps (QĐ-19): a case stops and checks the page it is on. They take
// no element; the value says how strict the check is. Mirrors
// checker/src/inspect.mjs, which validates again before running one.
//
//	check_accessibility  "" | minor | moderate | serious | critical — fail on an
//	                     axe-core WCAG A/AA problem at that impact or worse ("" = serious)
//	check_performance    "" | a budget such as "lcp=2500 cls=0.1 ttfb=800" (ms;
//	                     lcp, fcp, ttfb, load, cls) — "" = the Core Web Vitals "good" lines
//	check_security       "" | strict — fail on a high finding (strict: medium too)
var (
	CheckActions = []string{"check_accessibility", "check_performance", "check_security"}
	impacts      = []string{"minor", "moderate", "serious", "critical"}
	budgetPart   = regexp.MustCompile(`(?i)^([a-z]+)\s*(?:=|<=|<)\s*([0-9]+(?:\.[0-9]+)?)$`)
	budgetSplit  = regexp.MustCompile(`[\s,;]+`)
	budgetKeys   = []string{"lcp", "fcp", "ttfb", "load", "cls"}
)

// IsCheck reports whether action is a check step.
func IsCheck(action string) bool { return slices.Contains(CheckActions, action) }

// checkValue reports what is wrong with a check step's value, or nil.
func checkValue(action, value string) error {
	v := strings.TrimSpace(value)
	switch action {
	case "check_accessibility":
		if v != "" && !slices.Contains(impacts, v) {
			return fmt.Errorf(`check_accessibility value is "" or one of %s`, strings.Join(impacts, ", "))
		}
	case "check_performance":
		_, err := ParseBudget(v)
		return err
	case "check_security":
		if v != "" && v != "strict" {
			return fmt.Errorf(`check_security value is "" or "strict"`)
		}
	}
	return nil
}

// ParseBudget reads a check_performance value: "" is the default budget.
func ParseBudget(value string) (map[string]float64, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]float64{"lcp": 2500, "cls": 0.1, "ttfb": 800}, nil
	}
	out := map[string]float64{}
	for _, part := range budgetSplit.Split(strings.TrimSpace(value), -1) {
		if part == "" {
			continue
		}
		m := budgetPart.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("budget %q is not metric=number (lcp, fcp, ttfb, load in ms; cls)", part)
		}
		key := strings.ToLower(m[1])
		if !slices.Contains(budgetKeys, key) {
			return nil, fmt.Errorf("unknown metric %q: lcp, fcp, ttfb, load or cls", m[1])
		}
		n, _ := strconv.ParseFloat(m[2], 64)
		if (key == "cls" && n > 10) || (key != "cls" && (n < 1 || n > 120_000)) {
			return nil, fmt.Errorf("%s budget out of range", key)
		}
		out[key] = n
	}
	return out, nil
}

// describeCheck is a check step in words.
func describeCheck(s Step) string {
	v := strings.TrimSpace(s.Value)
	switch s.Do {
	case "check_accessibility":
		if v == "" {
			v = "serious"
		}
		return "Check accessibility (WCAG A/AA, fail at " + v + " or worse)"
	case "check_performance":
		if v == "" {
			return "Check page speed (LCP ≤ 2.5 s, CLS ≤ 0.1, first byte ≤ 800 ms)"
		}
		return "Check page speed (" + v + ")"
	case "check_security":
		if v == "strict" {
			return "Check security headers and cookies (fail on medium findings too)"
		}
		return "Check security headers and cookies"
	}
	return s.Do
}
