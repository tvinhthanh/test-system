package testmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// The §6.8.4 quality gate. Declarative rules, evaluated on the server,
// answered in a shape CI can act on (#2). Each rule declares its own level
// (#3: warn is never inferred into blocked), and the human decision lives
// on the release, apart from this result (#1: override with a name).
//
// Everything judged here is read from the Intelligence Plane as decided
// facts (§6.8.6): which results failed, which failures a person decided on,
// how many results were flaky passes. The gate adds only the link to test
// cases and requirements — its own tables.

const (
	RuleCriticalRegression = "critical_regression"
	RuleUntriagedFailures  = "untriaged_failures"
	RuleFlakyRate          = "flaky_rate"
	RuleP0Coverage         = "p0_coverage"
	RuleProdSmoke          = "prod_smoke_approved"
)

func failing(status string) bool {
	return status == "failed" || status == "timed_out" || status == "interrupted"
}

// Evaluate reads the newest finished run (on the release's branch when set),
// applies the policy and stores the result on the release.
func (s *service) Evaluate(ctx context.Context, projectID, id string) (Release, error) {
	rel, err := s.GetRelease(ctx, projectID, id)
	if err != nil {
		return Release{}, err
	}
	if s.evidence == nil {
		return Release{}, fmt.Errorf("testmgmt: no evidence source wired")
	}
	run, ok, err := s.evidence.LatestFinishedRun(ctx, projectID, rel.Branch)
	if err != nil {
		return Release{}, fmt.Errorf("gate evidence: %w", err)
	}
	now := s.now().UTC()
	result := GateResult{EvaluatedAt: now, Rules: []GateRule{}}

	// Inputs from this module's own tables.
	crit, err := s.criticalLinksSet(ctx, projectID)
	if err != nil {
		return Release{}, err
	}
	cov, hasP0, err := s.p0Coverage(ctx, projectID)
	if err != nil {
		return Release{}, err
	}
	prev, err := s.previousP0Coverage(ctx, projectID, rel.CreatedAt)
	if err != nil {
		return Release{}, err
	}
	if hasP0 {
		c := cov
		result.P0Coverage = &c
	}

	if !ok {
		result.Verdict = VerdictNoEvidence
		result.Rules = append(result.Rules, GateRule{ID: RuleCriticalRegression, Level: LevelBlocked, Passed: true, Applies: false, Summary: "no finished run to judge"})
		result.Rules = append(result.Rules, GateRule{ID: RuleUntriagedFailures, Level: LevelBlocked, Passed: true, Applies: false, Summary: "no finished run to judge"})
		result.Rules = append(result.Rules, GateRule{ID: RuleFlakyRate, Level: LevelWarn, Passed: true, Applies: false, Summary: "no finished run to judge"})
	} else {
		result.RunID, result.Branch = run.RunID, run.Branch
		result.Rules = append(result.Rules, criticalRegression(run, crit))
		result.Rules = append(result.Rules, untriagedFailures(run))
		result.Rules = append(result.Rules, flakyRate(run, s.policy))
	}
	result.Rules = append(result.Rules, p0CoverageRule(hasP0, cov, prev))
	result.Rules = append(result.Rules, GateRule{ID: RuleProdSmoke, Level: LevelBlocked, Passed: true, Applies: false,
		Summary: "production smoke approval is an Execution Plane rule (P3, QĐ-8)"})
	if ok {
		result.Verdict = verdict(result.Rules)
	}

	raw, _ := json.Marshal(result)
	tag, err := s.db.Q(ctx).Exec(ctx, `UPDATE releases SET last_gate = $3, evaluated_at = $4, updated_at = now() WHERE id = $1 AND project_id = $2`, id, projectID, raw, now)
	if err != nil {
		return Release{}, err
	}
	if tag.RowsAffected() == 0 {
		return Release{}, ErrNotFound
	}
	return s.GetRelease(ctx, projectID, id)
}

// verdict folds rule outcomes: any failed blocked rule → blocked; else any
// failed warn rule → warn; else approved. Rules that do not apply are
// silent — reported, never counted.
func verdict(rules []GateRule) GateVerdict {
	out := VerdictApproved
	for _, r := range rules {
		if !r.Applies || r.Passed {
			continue
		}
		if r.Level == LevelBlocked {
			return VerdictBlocked
		}
		out = VerdictWarn
	}
	return out
}

type criticalLink struct {
	TestCaseID   string `json:"testCaseId"`
	Title        string `json:"title"`
	StableTestID string `json:"stableTestId"`
}

// criticalLinks lists every (critical case, identity) pair of the project.
func (s *service) criticalLinks(ctx context.Context, projectID string) (links []criticalLink, criticalCases int, err error) {
	q := s.db.Q(ctx)
	if err := q.QueryRow(ctx, `SELECT count(*) FROM test_cases WHERE project_id = $1 AND is_critical AND automation_status <> 'obsolete'`, projectID).Scan(&criticalCases); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `
		SELECT c.id, c.title, l.stable_test_id
		  FROM test_cases c JOIN test_case_automation_links l ON l.test_case_id = c.id
		 WHERE c.project_id = $1 AND c.is_critical AND c.automation_status <> 'obsolete'`, projectID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var l criticalLink
		if err := rows.Scan(&l.TestCaseID, &l.Title, &l.StableTestID); err != nil {
			return nil, 0, err
		}
		links = append(links, l)
	}
	return links, criticalCases, rows.Err()
}

type criticalSet struct {
	links []criticalLink
	cases int
}

func (s *service) criticalLinksSet(ctx context.Context, projectID string) (criticalSet, error) {
	l, n, err := s.criticalLinks(ctx, projectID)
	return criticalSet{links: l, cases: n}, err
}

// Rule 1: a regression on a critical test case blocks the release.
func criticalRegression(run RunEvidence, crit criticalSet) GateRule {
	rule := GateRule{ID: RuleCriticalRegression, Level: LevelBlocked, Passed: true, Applies: true}
	if crit.cases == 0 {
		rule.Applies = false
		rule.Summary = "no test case is marked critical"
		return rule
	}
	if len(crit.links) == 0 {
		rule.Applies = false
		rule.Summary = fmt.Sprintf("%d critical case(s) but none is linked to automation — the gate cannot see them", crit.cases)
		rule.Details = map[string]any{"criticalCases": crit.cases}
		return rule
	}
	byIdentity := map[string]ResultEvidence{}
	for _, r := range run.Results {
		if r.StableTestID != "" {
			byIdentity[r.StableTestID] = r
		}
	}
	var regressions []map[string]any
	seen := 0
	for _, l := range crit.links {
		r, ok := byIdentity[l.StableTestID]
		if !ok {
			continue
		}
		seen++
		if failing(r.FinalStatus) {
			regressions = append(regressions, map[string]any{"testCaseId": l.TestCaseID, "title": l.Title, "stableTestId": l.StableTestID, "resultId": r.ResultID, "status": r.FinalStatus})
		}
	}
	rule.Details = map[string]any{"criticalCases": crit.cases, "linkedIdentities": len(crit.links), "seenInRun": seen, "regressions": regressions}
	if len(regressions) > 0 {
		rule.Passed = false
		rule.Summary = fmt.Sprintf("%d critical flow(s) failed in the judged run", len(regressions))
		return rule
	}
	rule.Summary = fmt.Sprintf("%d of %d linked critical identities ran, none failed", seen, len(crit.links))
	return rule
}

// Rule 3 of §6.8.4: a failure nobody decided on blocks the release.
func untriagedFailures(run RunEvidence) GateRule {
	rule := GateRule{ID: RuleUntriagedFailures, Level: LevelBlocked, Passed: true, Applies: run.Total > 0}
	if run.Total == 0 {
		rule.Summary = "the judged run has no results"
		return rule
	}
	failures, untriaged := 0, 0
	var ids []string
	for _, r := range run.Results {
		if !failing(r.FinalStatus) {
			continue
		}
		failures++
		if !r.Triaged {
			untriaged++
			if len(ids) < 50 {
				ids = append(ids, r.ResultID)
			}
		}
	}
	rule.Details = map[string]any{"failures": failures, "untriaged": untriaged, "untriagedResultIds": ids}
	if untriaged > 0 {
		rule.Passed = false
		rule.Summary = fmt.Sprintf("%d of %d failure(s) have no triage decision", untriaged, failures)
		return rule
	}
	rule.Summary = fmt.Sprintf("%d failure(s), every one decided", failures)
	return rule
}

// Rule 2: suite flaky rate above the threshold warns. Rate = flaky passes
// over all results of the run, straight from the run's own counters.
func flakyRate(run RunEvidence, p Policy) GateRule {
	rule := GateRule{ID: RuleFlakyRate, Level: LevelWarn, Passed: true, Applies: run.Total > 0}
	if run.Total == 0 {
		rule.Summary = "the judged run has no results"
		return rule
	}
	rate := float64(run.FlakyPass) / float64(run.Total)
	rule.Details = map[string]any{"flakyPass": run.FlakyPass, "total": run.Total, "rate": rate, "threshold": p.FlakyRateWarn}
	if rate > p.FlakyRateWarn {
		rule.Passed = false
		rule.Summary = fmt.Sprintf("flaky rate %.1f%% is above %.1f%%", rate*100, p.FlakyRateWarn*100)
		return rule
	}
	rule.Summary = fmt.Sprintf("flaky rate %.1f%% (threshold %.1f%%)", rate*100, p.FlakyRateWarn*100)
	return rule
}

// p0Coverage: share of open p0 requirements covered by at least one test
// case that is either automated or has a passed manual execution in an
// active plan — "covered" means someone or something checks it.
func (s *service) p0Coverage(ctx context.Context, projectID string) (coverage float64, hasP0 bool, err error) {
	var total, covered int
	err = s.db.Q(ctx).QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE EXISTS (
		           SELECT 1 FROM requirement_coverage rc JOIN test_cases c ON c.id = rc.test_case_id
		            WHERE rc.requirement_id = r.id AND c.automation_status <> 'obsolete'
		              AND (c.automation_status = 'automated'
		                   OR EXISTS (SELECT 1 FROM manual_executions e JOIN test_plans p ON p.id = e.test_plan_id
		                               WHERE e.test_case_id = c.id AND p.status = 'active' AND e.status = 'passed'))))
		  FROM requirements r
		 WHERE r.project_id = $1 AND r.priority = 'p0' AND r.status IN ('draft', 'active')`, projectID).Scan(&total, &covered)
	if err != nil || total == 0 {
		return 0, false, err
	}
	return float64(covered) / float64(total), true, nil
}

// previousP0Coverage is the figure stored by the newest earlier release
// that was evaluated; nil when this is the first.
func (s *service) previousP0Coverage(ctx context.Context, projectID string, before time.Time) (*float64, error) {
	var raw []byte
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT last_gate FROM releases
		 WHERE project_id = $1 AND created_at < $2 AND last_gate IS NOT NULL
		 ORDER BY created_at DESC LIMIT 1`, projectID, before).Scan(&raw)
	if err != nil {
		if mapErr(err) == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	var g GateResult
	if json.Unmarshal(raw, &g) != nil {
		return nil, nil
	}
	return g.P0Coverage, nil
}

// Rule 4: p0 requirement coverage below the previous release warns.
func p0CoverageRule(hasP0 bool, cov float64, prev *float64) GateRule {
	rule := GateRule{ID: RuleP0Coverage, Level: LevelWarn, Passed: true, Applies: hasP0 && prev != nil}
	switch {
	case !hasP0:
		rule.Summary = "no p0 requirement to cover"
	case prev == nil:
		rule.Summary = fmt.Sprintf("p0 coverage %.0f%% — first evaluated release, nothing to compare", cov*100)
		rule.Details = map[string]any{"coverage": cov}
	default:
		rule.Details = map[string]any{"coverage": cov, "previous": *prev}
		if cov < *prev {
			rule.Passed = false
			rule.Summary = fmt.Sprintf("p0 coverage fell from %.0f%% to %.0f%%", *prev*100, cov*100)
		} else {
			rule.Summary = fmt.Sprintf("p0 coverage %.0f%% (previous %.0f%%)", cov*100, *prev*100)
		}
	}
	return rule
}
