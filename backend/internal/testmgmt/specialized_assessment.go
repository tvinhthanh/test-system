package testmgmt

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type PerformanceThresholds struct {
	P95MaxMs               *float64 `json:"p95MaxMs"`
	P99MaxMs               *float64 `json:"p99MaxMs"`
	ThroughputMinPerSecond *float64 `json:"throughputMinPerSecond"`
	ErrorRateMaxPercent    *float64 `json:"errorRateMaxPercent"`
}
type PerformanceDetails struct {
	Subtype              string                `json:"subtype"`
	Scenario             string                `json:"scenario"`
	LoadConfiguration    string                `json:"loadConfiguration"`
	DataConfiguration    string                `json:"dataConfiguration"`
	Resources            string                `json:"resources"`
	DurationSeconds      *float64              `json:"durationSeconds"`
	WindowSeconds        *float64              `json:"windowSeconds"`
	P95Ms                *float64              `json:"p95Ms"`
	P99Ms                *float64              `json:"p99Ms"`
	ThroughputPerSecond  *float64              `json:"throughputPerSecond"`
	ErrorRatePercent     *float64              `json:"errorRatePercent"`
	Thresholds           PerformanceThresholds `json:"thresholds"`
	ThresholdsRecordedAt *time.Time            `json:"thresholdsRecordedAt"`
}
type ReportFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Location string `json:"location"`
	Summary  string `json:"summary"`
	Evidence string `json:"evidence"`
	Status   string `json:"status"`
	Retested bool   `json:"retested"`
}
type FindingsDetails struct {
	Standard           string          `json:"standard"`
	StandardVersion    string          `json:"standardVersion"`
	Method             string          `json:"method"`
	Coverage           []string        `json:"coverage"`
	AssessmentComplete bool            `json:"assessmentComplete"`
	Findings           []ReportFinding `json:"findings"`
}
type CompatibilityCell struct {
	Browser        string `json:"browser"`
	BrowserVersion string `json:"browserVersion"`
	OS             string `json:"os"`
	OSVersion      string `json:"osVersion"`
	Device         string `json:"device"`
	Status         string `json:"status"`
	Evidence       string `json:"evidence"`
}
type CompatibilityDetails struct {
	Matrix []CompatibilityCell `json:"matrix"`
}
type RecoveryDetails struct {
	Fault                 string   `json:"fault"`
	InitialConditions     string   `json:"initialConditions"`
	RecoveryObjective     string   `json:"recoveryObjective"`
	TargetRecoverySeconds *float64 `json:"targetRecoverySeconds"`
	ActualRecoverySeconds *float64 `json:"actualRecoverySeconds"`
	AllowedDataLossBytes  *float64 `json:"allowedDataLossBytes"`
	ActualDataLossBytes   *float64 `json:"actualDataLossBytes"`
	Evidence              string   `json:"evidence"`
}
type UsabilityTask struct {
	Task             string `json:"task"`
	SuccessCriterion string `json:"successCriterion"`
	Observation      string `json:"observation"`
	Conclusion       string `json:"conclusion"`
	Status           string `json:"status"`
}
type UsabilityDetails struct {
	Method       string          `json:"method"`
	Participants *float64        `json:"participants"`
	Tasks        []UsabilityTask `json:"tasks"`
	Conclusion   string          `json:"conclusion"`
}

func reportNumbers(values ...*float64) error {
	for _, v := range values {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return fmt.Errorf("%w: measurements must be finite and nonnegative", ErrInvalid)
		}
	}
	return nil
}
func missingNumber(values ...*float64) bool {
	for _, v := range values {
		if v == nil {
			return true
		}
	}
	return false
}
func emptyReportText(values ...string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return true
		}
	}
	return false
}
func reportShape(value any, depth int) bool {
	if depth > 12 {
		return false
	}
	switch x := value.(type) {
	case string:
		return len(x) <= 16000
	case []any:
		if len(x) > 200 {
			return false
		}
		for _, v := range x {
			if !reportShape(v, depth+1) {
				return false
			}
		}
	case map[string]any:
		if len(x) > 64 {
			return false
		}
		for _, v := range x {
			if !reportShape(v, depth+1) {
				return false
			}
		}
	}
	return true
}
func assessSpecialized(in SpecializedInput) (string, []string, json.RawMessage, error) {
	raw := in.Details
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	var shape any
	if json.Unmarshal(raw, &shape) != nil || !reportShape(shape, 0) {
		return "", nil, nil, fmt.Errorf("%w: invalid or oversized details", ErrInvalid)
	}
	if _, ok := shape.(map[string]any); !ok {
		return "", nil, nil, fmt.Errorf("%w: details must be an object", ErrInvalid)
	}
	reasons := []string{}
	failed := false
	var data any
	switch in.Kind {
	case "performance":
		var d PerformanceDetails
		if err := strictReportJSON(raw, &d); err != nil {
			return "", nil, nil, err
		}
		data = d
		if d.Subtype != "" && !slices.Contains(taxonomyPerformance, d.Subtype) {
			return "", nil, nil, fmt.Errorf("%w: performance subtype", ErrInvalid)
		}
		t := d.Thresholds
		if err := reportNumbers(d.DurationSeconds, d.WindowSeconds, d.P95Ms, d.P99Ms, d.ThroughputPerSecond, d.ErrorRatePercent, t.P95MaxMs, t.P99MaxMs, t.ThroughputMinPerSecond, t.ErrorRateMaxPercent); err != nil {
			return "", nil, nil, err
		}
		if (d.ErrorRatePercent != nil && *d.ErrorRatePercent > 100) || (t.ErrorRateMaxPercent != nil && *t.ErrorRateMaxPercent > 100) || (d.P95Ms != nil && d.P99Ms != nil && *d.P99Ms < *d.P95Ms) {
			return "", nil, nil, fmt.Errorf("%w: inconsistent latency/error metrics", ErrInvalid)
		}
		if emptyReportText(d.Subtype, d.Scenario, d.LoadConfiguration, d.DataConfiguration) {
			reasons = append(reasons, "Scenario, subtype, load and data configuration are required")
		}
		if d.Subtype == "scalability" && strings.TrimSpace(d.Resources) == "" {
			reasons = append(reasons, "Scalability requires compared resource configurations")
		}
		if missingNumber(d.DurationSeconds, d.WindowSeconds, d.P95Ms, d.P99Ms, d.ThroughputPerSecond, d.ErrorRatePercent) || (d.DurationSeconds != nil && *d.DurationSeconds <= 0) || (d.WindowSeconds != nil && *d.WindowSeconds <= 0) {
			reasons = append(reasons, "Duration, measurement window and all four metrics are required")
		}
		if d.DurationSeconds != nil && d.WindowSeconds != nil && *d.WindowSeconds > *d.DurationSeconds {
			return "", nil, nil, fmt.Errorf("%w: measurement window exceeds duration", ErrInvalid)
		}
		if missingNumber(t.P95MaxMs) && missingNumber(t.P99MaxMs) && missingNumber(t.ThroughputMinPerSecond) && missingNumber(t.ErrorRateMaxPercent) {
			reasons = append(reasons, "No thresholds recorded")
		}
		if d.ThresholdsRecordedAt == nil || d.ThresholdsRecordedAt.After(in.StartedAt) {
			reasons = append(reasons, "Threshold declaration must precede execution")
		}
		for _, check := range []struct {
			actual, limit *float64
			minimum       bool
		}{{d.P95Ms, t.P95MaxMs, false}, {d.P99Ms, t.P99MaxMs, false}, {d.ThroughputPerSecond, t.ThroughputMinPerSecond, true}, {d.ErrorRatePercent, t.ErrorRateMaxPercent, false}} {
			if check.actual != nil && check.limit != nil && ((!check.minimum && *check.actual > *check.limit) || (check.minimum && *check.actual < *check.limit)) {
				failed = true
			}
		}
		if failed {
			reasons = append(reasons, "At least one recorded threshold was breached")
		}
	case "security", "accessibility":
		var d FindingsDetails
		if err := strictReportJSON(raw, &d); err != nil {
			return "", nil, nil, err
		}
		data = d
		if d.Method != "" && !slices.Contains([]string{"manual", "automated", "combined"}, d.Method) {
			return "", nil, nil, fmt.Errorf("%w: assessment method", ErrInvalid)
		}
		if !d.AssessmentComplete || len(d.Coverage) == 0 || d.Findings == nil || strings.TrimSpace(d.Standard) == "" {
			reasons = append(reasons, "Assessment scope/completeness and findings were not fully recorded")
		}
		for _, scope := range d.Coverage {
			if strings.TrimSpace(scope) == "" {
				return "", nil, nil, fmt.Errorf("%w: empty coverage item", ErrInvalid)
			}
		}
		if in.Kind == "accessibility" && emptyReportText(d.StandardVersion, d.Method) {
			reasons = append(reasons, "Accessibility requires standard version and assessment method")
		}
		seen := map[string]bool{}
		for _, finding := range d.Findings {
			if emptyReportText(finding.ID, finding.Severity, finding.Location, finding.Summary, finding.Evidence) || seen[finding.ID] {
				return "", nil, nil, fmt.Errorf("%w: finding identity, severity, location and evidence required", ErrInvalid)
			}
			seen[finding.ID] = true
			switch finding.Status {
			case "open":
				failed = true
			case "resolved":
				if !finding.Retested {
					reasons = append(reasons, "Resolved finding has not been retested")
				}
			case "accepted_risk", "not_retested":
				reasons = append(reasons, "Finding needs review or retest")
			default:
				return "", nil, nil, fmt.Errorf("%w: finding status", ErrInvalid)
			}
		}
		if failed {
			reasons = append(reasons, "Open findings remain")
		}
	case "compatibility":
		var d CompatibilityDetails
		if err := strictReportJSON(raw, &d); err != nil {
			return "", nil, nil, err
		}
		data = d
		if len(d.Matrix) == 0 {
			reasons = append(reasons, "No platform matrix recorded")
		}
		seen := map[string]bool{}
		for _, cell := range d.Matrix {
			key, _ := json.Marshal([]string{cell.Browser, cell.BrowserVersion, cell.OS, cell.OSVersion, cell.Device})
			if seen[string(key)] {
				return "", nil, nil, fmt.Errorf("%w: duplicate platform combination", ErrInvalid)
			}
			seen[string(key)] = true
			if emptyReportText(cell.Browser, cell.BrowserVersion, cell.OS, cell.OSVersion, cell.Device) {
				reasons = append(reasons, "A platform combination is incomplete")
			}
			switch cell.Status {
			case "failed":
				failed = true
			case "passed":
				if strings.TrimSpace(cell.Evidence) == "" {
					reasons = append(reasons, "Passed platform has no evidence")
				}
			case "blocked", "skipped", "not_run", "":
				reasons = append(reasons, "A platform combination has not passed")
			default:
				return "", nil, nil, fmt.Errorf("%w: platform status", ErrInvalid)
			}
		}
		if failed {
			reasons = append(reasons, "A platform combination failed")
		}
	case "recovery":
		var d RecoveryDetails
		if err := strictReportJSON(raw, &d); err != nil {
			return "", nil, nil, err
		}
		data = d
		if err := reportNumbers(d.TargetRecoverySeconds, d.ActualRecoverySeconds, d.AllowedDataLossBytes, d.ActualDataLossBytes); err != nil {
			return "", nil, nil, err
		}
		for _, v := range []*float64{d.AllowedDataLossBytes, d.ActualDataLossBytes} {
			if v != nil && math.Trunc(*v) != *v {
				return "", nil, nil, fmt.Errorf("%w: data loss bytes must be integers", ErrInvalid)
			}
		}
		if emptyReportText(d.Fault, d.InitialConditions, d.RecoveryObjective, d.Evidence) || missingNumber(d.TargetRecoverySeconds, d.ActualRecoverySeconds, d.AllowedDataLossBytes, d.ActualDataLossBytes) {
			reasons = append(reasons, "Fault, initial state, objective, measured recovery/data loss and evidence are required")
		}
		if d.TargetRecoverySeconds != nil && d.ActualRecoverySeconds != nil && *d.ActualRecoverySeconds > *d.TargetRecoverySeconds {
			failed = true
		}
		if d.AllowedDataLossBytes != nil && d.ActualDataLossBytes != nil && *d.ActualDataLossBytes > *d.AllowedDataLossBytes {
			failed = true
		}
		if failed {
			reasons = append(reasons, "Recovery or data-loss objective exceeded")
		}
	case "usability":
		var d UsabilityDetails
		if err := strictReportJSON(raw, &d); err != nil {
			return "", nil, nil, err
		}
		data = d
		if err := reportNumbers(d.Participants); err != nil {
			return "", nil, nil, err
		}
		if d.Participants != nil && math.Trunc(*d.Participants) != *d.Participants {
			return "", nil, nil, fmt.Errorf("%w: participants must be integer", ErrInvalid)
		}
		if emptyReportText(d.Method, d.Conclusion) || len(d.Tasks) == 0 {
			reasons = append(reasons, "Method, task observations and conclusion are required")
		}
		for _, task := range d.Tasks {
			if emptyReportText(task.Task, task.SuccessCriterion, task.Observation, task.Conclusion) {
				reasons = append(reasons, "Task criteria, observation or conclusion missing")
			}
			switch task.Status {
			case "passed":
			case "failed":
				failed = true
			case "inconclusive", "":
				reasons = append(reasons, "Task has no conclusive assessment")
			default:
				return "", nil, nil, fmt.Errorf("%w: usability task status", ErrInvalid)
			}
		}
		if failed {
			reasons = append(reasons, "A usability task failed its recorded criteria")
		}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", nil, nil, err
	}
	if in.State != "completed" {
		return in.State, []string{in.Notes}, encoded, nil
	}
	slices.Sort(reasons)
	reasons = slices.Compact(reasons)
	if failed {
		return "failed", reasons, encoded, nil
	}
	if len(reasons) > 0 {
		return "inconclusive", reasons, encoded, nil
	}
	return "passed", reasons, encoded, nil
}
