package identity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Result holds the precision/recall measurement — the pass criteria in SPEC §9.6 item 3.
type Result struct {
	Total int

	AutoMatched  int // merged automatically by the algorithm
	AutoCorrect  int // ...and merged correctly
	AutoWrong    int // ...merged WRONGLY — the most damaging failure mode
	SentToReview int // queued for review (an acceptable miss)
	NewIdentity  int // created fresh, no question asked

	Precision float64 // AutoCorrect / AutoMatched
	Recall    float64 // AutoCorrect / Total

	ByKind map[MutationKind]*KindResult
}

type KindResult struct {
	Total, AutoCorrect, AutoWrong, Review, New int
}

// Evaluate runs the resolver over every rename case and measures the outcome.
//
// Precision matters more than recall: a wrong merge CORRUPTS THE HISTORY of two
// tests and is hard to spot later, whereas a wrong split only costs review time.
// That is why SPEC §9.6 demands precision >= 0.98 but only recall >= 0.80.
func Evaluate(cfg Config, suite []Identity, cases []RenameCase, repoRoot string, now time.Time) Result {
	// Build the alias store and candidate set from the pre-change state.
	aliases := AliasStore{}
	cands := make([]Candidate, 0, len(suite))
	for i, id := range suite {
		sid := fmt.Sprintf("st_%04d", i)
		aliases[id.Fingerprint(repoRoot)] = sid
		cands = append(cands, Candidate{
			StableID:   sid,
			Identity:   id,
			LastSeenAt: now.Add(-24 * time.Hour), // last seen in yesterday's run
			// Rename scenario: the old test no longer appears in this run.
			PresentInRun: false,
		})
	}
	r := &Resolver{Aliases: aliases, Candidates: cands, RepoRoot: repoRoot, Config: cfg}

	// Index candidates by stableID so PresentInRun can be toggled per case.
	idxByID := make(map[string]int, len(cands))
	for i, c := range cands {
		idxByID[c.StableID] = i
	}

	res := Result{Total: len(cases), ByKind: map[MutationKind]*KindResult{}}
	for _, k := range AllMutations {
		res.ByKind[k] = &KindResult{}
	}

	for _, c := range cases {
		kr := res.ByKind[c.Kind]
		kr.Total++

		// Model reality: only the renamed test is absent from this run.
		// Every other test still ran under its old fingerprint and must take the x0.3 penalty (§9.3).
		for i := range r.Candidates {
			r.Candidates[i].PresentInRun = true
		}
		if i, ok := idxByID[c.StableID]; ok {
			r.Candidates[i].PresentInRun = false
		}

		got := r.Resolve(c.After, now)
		switch got.Decision {
		case DecisionExactMatch:
			// Fingerprint unchanged (e.g. a parameter that does not feed the fingerprint):
			// count it as a correct auto-match when it points at the right identity.
			res.AutoMatched++
			if got.MatchedID == c.StableID {
				res.AutoCorrect++
				kr.AutoCorrect++
			} else {
				res.AutoWrong++
				kr.AutoWrong++
			}
		case DecisionAutoMatch:
			res.AutoMatched++
			if got.MatchedID == c.StableID {
				res.AutoCorrect++
				kr.AutoCorrect++
			} else {
				res.AutoWrong++
				kr.AutoWrong++
			}
		case DecisionReview:
			res.SentToReview++
			kr.Review++
		case DecisionNew:
			res.NewIdentity++
			kr.New++
		}
	}

	if res.AutoMatched > 0 {
		res.Precision = float64(res.AutoCorrect) / float64(res.AutoMatched)
	}
	if res.Total > 0 {
		res.Recall = float64(res.AutoCorrect) / float64(res.Total)
	}
	return res
}

// Report renders the result against the §9.6 criteria.
func (r Result) Report() string {
	var b strings.Builder
	pass := r.Precision >= 0.98 && r.Recall >= 0.80

	fmt.Fprintf(&b, "Rename cases          : %d\n", r.Total)
	fmt.Fprintf(&b, "Auto-matched          : %d  (correct %d, WRONG %d)\n", r.AutoMatched, r.AutoCorrect, r.AutoWrong)
	fmt.Fprintf(&b, "Sent to Review Queue  : %d\n", r.SentToReview)
	fmt.Fprintf(&b, "New identities        : %d\n", r.NewIdentity)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "PRECISION             : %.4f   (§9.6 needs >= 0.98)  %s\n", r.Precision, mark(r.Precision >= 0.98))
	fmt.Fprintf(&b, "RECALL                : %.4f   (§9.6 needs >= 0.80)  %s\n", r.Recall, mark(r.Recall >= 0.80))
	fmt.Fprintf(&b, "\nBy mutation kind:\n")

	kinds := make([]MutationKind, 0, len(r.ByKind))
	for k := range r.ByKind {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return string(kinds[i]) < string(kinds[j]) })

	fmt.Fprintf(&b, "  %-30s %6s %8s %7s %8s %6s\n", "kind", "total", "correct", "WRONG", "review", "new")
	for _, k := range kinds {
		kr := r.ByKind[k]
		fmt.Fprintf(&b, "  %-30s %6d %8d %7d %8d %6d\n", k, kr.Total, kr.AutoCorrect, kr.AutoWrong, kr.Review, kr.New)
	}

	fmt.Fprintf(&b, "\nVERDICT: ")
	if pass {
		fmt.Fprintf(&b, "PASS — algorithm v1 is usable as configured.\n")
	} else {
		fmt.Fprintf(&b, "FAIL.\n")
		fmt.Fprintf(&b, "Per §9.6 item 4: drop auto-match to score >= 0.95 and route the\n")
		fmt.Fprintf(&b, "remainder to the Review Queue. The product still ships, with\n")
		fmt.Fprintf(&b, "more manual review.\n")
	}
	return b.String()
}

func mark(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}
