package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// ---- Rule-based investigation (no AI key) ---------------------------------
//
// When an organization has no AI key, an investigation still runs: the same
// read-only evidence the model would see (failure_context, stable_history,
// triage_history) goes through fixed rules instead. The result is an ordinary
// hypothesis — category, confidence, cited facts, suggested action — that a
// member accepts or rejects, so TTRC and triage coverage (§8.3, which count
// only HYPOTHESIS_ACCEPTED) work without any AI provider.
//
// Every fact cites the evidence it came from. Beyond patterns, the rules
// learn from the team's own verdicts: the same error explained for another
// test is proposed again (similar_failures), and confidence is calibrated
// per category against how often people accepted the rules' suggestions
// (up to 0.95 once earned; at most 0.85 before). The human decision is still
// what counts — TTRC is the time until the team accepts a cause (§8.3).

// RulesModel names the rule set in ai_hypotheses.model and on the
// investigation, so the UI can say "rule-based" and a rule change is visible
// in history.
const RulesModel = "rules-v1"

type ruleEvidence struct {
	FailureContext struct {
		Result struct {
			FinalStatus string `json:"finalStatus"`
			DisplayName string `json:"displayName"`
		} `json:"result"`
		Environment string `json:"environment"`
		Attempts    []struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Stack   string `json:"stack"`
		} `json:"attempts"`
	} `json:"failureContext"`
	StableHistory []struct {
		Status      string    `json:"status"`
		Environment string    `json:"environment"`
		CommitSHA   string    `json:"commitSha"`
		StartedAt   time.Time `json:"startedAt"`
	} `json:"stableHistory"`
	TriageHistory []struct {
		Type       string    `json:"type"`
		Category   *string   `json:"category"`
		OccurredAt time.Time `json:"occurredAt"`
	} `json:"triageHistory"`
	// Causes accepted for other tests that failed with the same error.
	SimilarFailures []struct {
		Category       string    `json:"category"`
		Tests          int       `json:"tests"`
		Example        string    `json:"example"`
		LastAcceptedAt time.Time `json:"lastAcceptedAt"`
	} `json:"similarFailures"`
	// How the team answered this rule set's hypotheses, per category.
	RulesTrackRecord []struct {
		Category string `json:"category"`
		Accepted int    `json:"accepted"`
		Rejected int    `json:"rejected"`
	} `json:"rulesTrackRecord"`
}

// Confidence bounds. Below calibrationMinimum decisions a category keeps its
// rule's own confidence (at most maxUncalibrated); with a track record it may
// rise to maxCalibrated — earned by the team accepting it, never assumed.
const (
	maxUncalibrated    = 0.85
	maxCalibrated      = 0.95
	minConfidence      = 0.1
	calibrationMinimum = 5
)

// ruleVerdict is what the rules conclude.
type ruleVerdict struct {
	Category        Category
	Confidence      float64
	Evidence        []EvidenceFact
	SuggestedAction string
}

type errorPattern struct {
	category   Category
	confidence float64
	label      string
	re         *regexp.Regexp
}

// Ordered: the first pattern that matches the error text wins. Infra and data
// before locator and timing, because a refused connection also times out.
var errorPatterns = []errorPattern{
	{CategoryEnvInfra, 0.7, "a network or service failure", regexp.MustCompile(`(?i)ECONNREFUSED|ECONNRESET|ENOTFOUND|EAI_AGAIN|EHOSTUNREACH|socket hang up|connection refused|net::ERR_|\b50[234]\b|bad gateway|service unavailable|no route to host|certificate|TLS handshake|page did not open|page could not be read`)},
	{CategoryDependencyExternal, 0.55, "a rate limit or third-party refusal", regexp.MustCompile(`(?i)\b429\b|rate.?limit|too many requests|quota exceeded`)},
	{CategoryEnvData, 0.6, "a data or fixture problem", regexp.MustCompile(`(?i)duplicate key|unique constraint|already exists|foreign key|violates .*constraint|fixture|seed data|no such (row|record)|record not found`)},
	{CategoryTestBug, 0.55, "a test that could not reach its element or site", regexp.MustCompile(`(?i)resolved to 0 elements|strict mode violation|no node found|waiting for (locator|selector)|element is not attached|unable to find (element|selector)|NoSuchElement|is not a valid selector|is not the site under test`)},
	{CategoryFlakyTiming, 0.5, "a timeout", regexp.MustCompile(`(?i)timeout|timed out|exceeded while|deadline exceeded`)},
	// Playwright/Jest/pytest assertions, and the checker's own wording for a
	// failed auto-test check (checker/src/steps.mjs).
	{CategoryAppBug, 0.45, "an assertion on the application's behaviour", regexp.MustCompile(`(?i)expect\(|assertion|assert(ed)?\b|expected .* (received|but was|to (be|equal|contain))|toHave|toBe|toEqual|toContain|is not on (the|an?) |does not (contain|hold)|is (still|not) visible|the (page )?title is|the address is`)},
}

func failing(status string) bool {
	return status == "failed" || status == "timed_out" || status == "interrupted" || status == "error"
}
func passing(status string) bool { return status == "passed" || status == "flaky_pass" }

func firstLine(s string, n int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return cut(s, n)
}

// investigateByRules reads the evidence JSON built by query.InvestigationEvidence.
func investigateByRules(raw json.RawMessage) (ruleVerdict, error) {
	var ev ruleEvidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		return ruleVerdict{}, err
	}
	type candidate struct {
		category   Category
		confidence float64
	}
	var cands []candidate
	var facts []EvidenceFact
	add := func(source, fact string) {
		if len(facts) < 10 {
			facts = append(facts, EvidenceFact{Source: source, Fact: cut(fact, 2000)})
		}
	}

	// 1. The error text of the attempts.
	msg := ""
	for _, a := range ev.FailureContext.Attempts {
		if strings.TrimSpace(a.Message) != "" {
			msg = a.Message + "\n" + a.Stack
		}
	}
	if line := firstLine(msg, 300); line != "" {
		add("failure_context", fmt.Sprintf("The last attempt failed with: %s", line))
	}
	for _, p := range errorPatterns {
		if msg != "" && p.re.MatchString(msg) {
			cands = append(cands, candidate{p.category, p.confidence})
			add("failure_context", fmt.Sprintf("The error text looks like %s (%s).", p.label, p.category))
			break
		}
	}

	// 2. The test's own history, newest first.
	h := ev.StableHistory
	if n := len(h); n > 0 {
		flips, fails, passes, flakies := 0, 0, 0, 0
		for i, x := range h {
			switch {
			case x.Status == "flaky_pass":
				flakies++
				passes++
			case passing(x.Status):
				passes++
			case failing(x.Status):
				fails++
			}
			if i > 0 && passing(h[i-1].Status) != passing(x.Status) {
				flips++
			}
		}
		add("stable_history", fmt.Sprintf("Over the previous %d runs: %d passed (%d only after a retry), %d failed, %d changes between pass and fail.", n, passes, flakies, fails, flips))
		switch {
		case flakies > 0 || flips >= 3:
			cands = append(cands, candidate{CategoryFlakyTiming, 0.65})
			add("stable_history", "The test alternates between passing and failing on the same code, which points at timing or order, not a fixed defect.")
		case passes == 0 && fails >= 3:
			cands = append(cands, candidate{CategoryTestBug, 0.5})
			add("stable_history", fmt.Sprintf("The test has not passed in any of the previous %d runs: it may never have worked as written.", n))
		default:
			// A clean streak of failures right after a pass, in the same
			// environment: a regression. A pass in another environment is the
			// environment rule's business, not a regression.
			same := h[:0:0]
			for _, x := range h {
				if ev.FailureContext.Environment == "" || x.Environment == ev.FailureContext.Environment {
					same = append(same, x)
				}
			}
			streak := 0
			for _, x := range same {
				if !failing(x.Status) {
					break
				}
				streak++
			}
			if streak < len(same) && passing(same[streak].Status) {
				last := same[streak]
				where := ""
				if last.CommitSHA != "" {
					where = " at commit " + cut(last.CommitSHA, 12)
				}
				cands = append(cands, candidate{CategoryAppBug, 0.55})
				add("stable_history", fmt.Sprintf("It last passed on %s%s and has failed in every run since (%d), which suggests a change introduced then.", last.StartedAt.Format("2006-01-02"), where, streak+1))
			}
		}
		// Fails only in this environment while passing elsewhere.
		env := ev.FailureContext.Environment
		passElsewhere := map[string]bool{}
		for _, x := range h {
			if passing(x.Status) && x.Environment != "" && x.Environment != env {
				passElsewhere[x.Environment] = true
			}
		}
		if env != "" && len(passElsewhere) > 0 {
			names := make([]string, 0, len(passElsewhere))
			for k := range passElsewhere {
				names = append(names, k)
			}
			sort.Strings(names)
			cands = append(cands, candidate{CategoryEnvInfra, 0.5})
			add("stable_history", fmt.Sprintf("It fails in %q but has passed in %s, so the environment differs.", env, strings.Join(names, ", ")))
		}
	}

	// 3. What people concluded about this test before.
	for _, d := range ev.TriageHistory {
		if d.Type == string(HypothesisAccepted) && d.Category != nil && ValidCategory(Category(*d.Category)) {
			cands = append(cands, candidate{Category(*d.Category), 0.7})
			add("triage_history", fmt.Sprintf("On %s the team accepted %q as the cause of an earlier failure of this test.", d.OccurredAt.Format("2006-01-02"), *d.Category))
			break
		}
	}

	// 4. The same error, explained for other tests: the strongest signal the
	// rules have, because a person already found that cause.
	for _, sf := range ev.SimilarFailures {
		if !ValidCategory(Category(sf.Category)) || sf.Tests < 1 {
			continue
		}
		conf := minFloat(0.9, 0.75+0.05*float64(sf.Tests-1))
		cands = append(cands, candidate{Category(sf.Category), conf})
		others := ""
		if sf.Tests > 1 {
			others = fmt.Sprintf(" and %d other test(s)", sf.Tests-1)
		}
		add("similar_failures", fmt.Sprintf("The same error was explained before for %q%s: the team accepted %q (last on %s).", cut(sf.Example, 120), others, sf.Category, sf.LastAcceptedAt.Format("2006-01-02")))
		break
	}

	v := ruleVerdict{Category: CategoryUnknown, Confidence: 0.2}
	for _, c := range cands {
		if c.confidence > v.Confidence {
			v.Category, v.Confidence = c.category, c.confidence
		}
	}
	// Agreement between independent signals raises confidence a little.
	agree := 0
	for _, c := range cands {
		if c.category == v.Category {
			agree++
		}
	}
	if agree > 1 {
		v.Confidence = minFloat(maxUncalibrated, v.Confidence+0.1*float64(agree-1))
	}
	v.Confidence = minFloat(v.Confidence, maxUncalibrated)
	if v.Confidence < 0.9 {
		// A similar failure's own confidence may exceed the generic cap: a
		// person explained that exact error.
		for _, c := range cands {
			if c.category == v.Category && c.confidence > v.Confidence {
				v.Confidence = c.confidence
			}
		}
	}

	// 5. Calibrate against how the team answered these rules before: a cause
	// the rules propose and people keep accepting earns confidence; one they
	// keep rejecting or correcting loses it.
	for _, tr := range ev.RulesTrackRecord {
		n := tr.Accepted + tr.Rejected
		if Category(tr.Category) != v.Category || n < calibrationMinimum {
			continue
		}
		rate := float64(tr.Accepted) / float64(n)
		v.Confidence = (v.Confidence + rate) / 2
		if v.Confidence > maxCalibrated {
			v.Confidence = maxCalibrated
		}
		if v.Confidence < minConfidence {
			v.Confidence = minConfidence
		}
		add("triage_history", fmt.Sprintf("In this project the team accepted %d of the %d rule suggestions of %q.", tr.Accepted, n, tr.Category))
		break
	}
	if len(facts) == 0 {
		add("failure_context", "The failure carries no error text and the test has no history yet.")
	}
	v.Evidence = facts
	v.SuggestedAction = suggestedActions[v.Category]
	return v, nil
}

// runRulesInvestigation finishes an investigation without a model: same
// evidence, same hypothesis row and HYPOTHESIS_PROPOSED event as the AI path,
// but the proposer is SYSTEM/rules-v1, so AI metrics never count it.
func (s *service) runRulesInvestigation(ctx context.Context, in Investigation, fail func(string) error) error {
	q := s.db.Q(ctx)
	raw, _, _, err := s.evidence.InvestigationEvidence(ctx, in.ProjectID, in.ResultID)
	if err != nil {
		return fail("The failure evidence is no longer available.")
	}
	if _, err := q.Exec(ctx, `UPDATE ai_investigations SET status='investigating',provider='rules',model=$2,key_source='',started_at=coalesce(started_at,now()) WHERE id=$1`, in.ID, RulesModel); err != nil {
		return err
	}
	v, err := investigateByRules(raw)
	if err != nil {
		return fail("The failure evidence is invalid.")
	}
	evidence, _ := json.Marshal(v.Evidence)
	var hypID string
	if err := q.QueryRow(ctx, `INSERT INTO ai_hypotheses(project_id,test_result_id,category,confidence,evidence,suggested_action,model,prompt_version,investigation_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8) RETURNING id`, in.ProjectID, in.ResultID, v.Category, v.Confidence, evidence, v.SuggestedAction, RulesModel, in.ID).Scan(&hypID); err != nil {
		return err
	}
	tr, err := s.results.GetResult(ctx, in.ProjectID, results.TestResultID(in.ResultID))
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"category": v.Category, "confidence": v.Confidence, "method": "rules"})
	if _, err := q.Exec(ctx, `INSERT INTO triage_events(project_id,test_result_id,run_id,type,actor_type,actor_id,hypothesis_id,payload,occurred_at)
		VALUES($1,$2,$3,'HYPOTHESIS_PROPOSED','SYSTEM',$4,$5,$6,now())`, in.ProjectID, in.ResultID, string(tr.RunID), RulesModel, hypID, payload); err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE ai_investigations SET status='done',hypothesis_id=$2,turn_count=1,tool_count=3,finished_at=now() WHERE id=$1`, in.ID, hypID)
	return err
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

var suggestedActions = map[Category]string{
	CategoryAppBug:             "Compare the changes since the last passing run and reproduce the failing assertion against the application.",
	CategoryTestBug:            "Check the test's locators and expectations against the current page; update the test if the application changed on purpose.",
	CategoryFlakyTiming:        "Replace fixed waits with waits on a condition, isolate shared state, and watch the next runs before marking it flaky.",
	CategoryEnvInfra:           "Check the environment's services, network and certificates at the time of the run; rerun once it is healthy.",
	CategoryEnvData:            "Reset or reseed the test data and make the test create what it needs.",
	CategoryDependencyExternal: "Check the third-party service's status and limits; stub it in tests that do not need the real one.",
	CategoryUnknown:            "Open the trace and screenshots of the last attempt; record the cause by hand once found.",
}
