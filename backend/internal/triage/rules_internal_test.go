package triage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// evidenceJSON builds the shape query.InvestigationEvidence returns.
func evidenceJSON(t *testing.T, msg, env string, history []map[string]any, decisions []map[string]any) json.RawMessage {
	t.Helper()
	attempts := []map[string]any{{"attempt": 0, "status": "failed"}}
	if msg != "" {
		attempts[0]["message"] = msg
	}
	if history == nil {
		history = []map[string]any{}
	}
	if decisions == nil {
		decisions = []map[string]any{}
	}
	b, err := json.Marshal(map[string]any{
		"failureContext": map[string]any{"result": map[string]any{"finalStatus": "failed"}, "environment": env, "attempts": attempts},
		"stableHistory":  history,
		"triageHistory":  decisions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hist(env string, statuses ...string) []map[string]any {
	out := []map[string]any{}
	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
	for i, s := range statuses {
		out = append(out, map[string]any{"status": s, "environment": env, "commitSha": "abc123def4567890", "startedAt": at.Add(-time.Duration(i) * 24 * time.Hour)})
	}
	return out
}

func TestInvestigateByRules(t *testing.T) {
	accepted := "env_data"
	cases := []struct {
		name      string
		raw       json.RawMessage
		want      Category
		minConf   float64
		factWords string // a substring one fact must carry
	}{
		{"refused connection is infrastructure", evidenceJSON(t, "Error: connect ECONNREFUSED 10.0.0.5:5432", "ci", nil, nil), CategoryEnvInfra, 0.7, "network"},
		{"missing element is the test", evidenceJSON(t, "locator.click: waiting for locator('#pay') resolved to 0 elements", "ci", nil, nil), CategoryTestBug, 0.5, "element"},
		{"duplicate key is data", evidenceJSON(t, `ERROR: duplicate key value violates unique constraint "users_email_key"`, "ci", nil, nil), CategoryEnvData, 0.6, "data"},
		{"rate limit is a dependency", evidenceJSON(t, "HTTP 429 Too Many Requests from payments.example", "ci", nil, nil), CategoryDependencyExternal, 0.5, "rate limit"},
		{"alternating history is flaky", evidenceJSON(t, "expect(received).toBe(expected)", "ci", hist("ci", "passed", "failed", "passed", "failed", "passed"), nil), CategoryFlakyTiming, 0.65, "alternates"},
		{"failing since a pass is a regression", evidenceJSON(t, "expect(received).toContain(expected)", "ci", hist("ci", "failed", "failed", "passed", "passed"), nil), CategoryAppBug, 0.55, "last passed on"},
		{"never passed is the test", evidenceJSON(t, "", "ci", hist("ci", "failed", "failed", "failed"), nil), CategoryTestBug, 0.5, "has not passed"},
		{"passes elsewhere is the environment", evidenceJSON(t, "", "staging", hist("ci", "passed", "passed"), nil), CategoryEnvInfra, 0.5, "environment differs"},
		{"earlier accepted cause is reused", evidenceJSON(t, "", "ci", nil, []map[string]any{{"type": "HYPOTHESIS_ACCEPTED", "category": accepted, "occurredAt": time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}}), CategoryEnvData, 0.7, "accepted"},
		{"nothing to go on is unknown", evidenceJSON(t, "", "", nil, nil), CategoryUnknown, 0.2, "no error text"},
		// The checker's wording for failed auto-test steps.
		{"auto test text check is an assertion", evidenceJSON(t, "step: Step 2: the text “Trang không tồn tại” is not on the element h1", "ci", nil, nil), CategoryAppBug, 0.45, "assertion"},
		{"auto test title check is an assertion", evidenceJSON(t, "Step 2: the page title does not contain “Shop”", "ci", nil, nil), CategoryAppBug, 0.45, "assertion"},
		{"auto test sign-in page that never opened is infrastructure", evidenceJSON(t, "the sign-in page did not open: net timeout", "ci", nil, nil), CategoryEnvInfra, 0.7, "network"},
		{"auto test aimed at another site is the test", evidenceJSON(t, "Step 1: evil.example is not the site under test", "ci", nil, nil), CategoryTestBug, 0.55, "element"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, err := investigateByRules(c.raw)
			if err != nil {
				t.Fatal(err)
			}
			if v.Category != c.want {
				t.Fatalf("category = %s, want %s; facts %+v", v.Category, c.want, v.Evidence)
			}
			if v.Confidence < c.minConf || v.Confidence > 0.85 {
				t.Errorf("confidence = %.2f, want ≥ %.2f and ≤ 0.85", v.Confidence, c.minConf)
			}
			if len(v.Evidence) == 0 || len(v.Evidence) > 10 {
				t.Fatalf("evidence count %d", len(v.Evidence))
			}
			found := false
			for _, f := range v.Evidence {
				// The same contract as the model's hypotheses: every fact cites
				// one of the three observed sources and says something.
				if f.Source != "failure_context" && f.Source != "stable_history" && f.Source != "triage_history" {
					t.Errorf("fact cites unobserved source %q", f.Source)
				}
				if strings.TrimSpace(f.Fact) == "" {
					t.Error("empty fact")
				}
				if strings.Contains(strings.ToLower(f.Fact), c.factWords) {
					found = true
				}
			}
			if !found {
				t.Errorf("no fact mentions %q: %+v", c.factWords, v.Evidence)
			}
			if v.SuggestedAction == "" {
				t.Error("no suggested action")
			}
		})
	}
}

func TestInvestigateByRulesAgreementRaisesConfidence(t *testing.T) {
	// A refused connection, failing only here while passing elsewhere: two
	// independent signals for env_infra.
	v, err := investigateByRules(evidenceJSON(t, "connect ECONNREFUSED", "staging", hist("ci", "passed", "passed"), nil))
	if err != nil {
		t.Fatal(err)
	}
	if v.Category != CategoryEnvInfra || v.Confidence <= 0.7 {
		t.Fatalf("got %s %.2f, want env_infra above 0.7", v.Category, v.Confidence)
	}
}

func TestInvestigateByRulesRejectsBadEvidence(t *testing.T) {
	if _, err := investigateByRules(json.RawMessage(`not json`)); err == nil {
		t.Fatal("want an error for invalid evidence")
	}
}
