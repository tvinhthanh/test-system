package identity

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"
)

// Experiment for the load-bearing assumption behind N34.
//
// §9.3's decisive signal asks "is this candidate still present in the run?".
// Answering it requires knowing which tests ran — but §7.6 resolves identity
// INLINE with ingest at p50 < 1s, long before the run finishes. At that moment
// the resolver has only seen a fraction of the run.
//
// Three real situations produce a partial view:
//   - sharding: `--shard=1/4` means each shard reports a quarter of the suite
//   - filtered runs: `--grep`, `--project`, changed-files-only runs
//   - batching: events arrive in batches of 500 (§7.2) while resolution runs inline
//
// A test that has not been reported YET is indistinguishable from a test that
// has DISAPPEARED. Both look absent. This measures what that costs.

// VisibilityResult reports accuracy at one level of run visibility.
type VisibilityResult struct {
	VisibleFraction float64
	AutoMatched     int
	AutoCorrect     int
	AutoWrong       int
	Review          int
	NewIdentity     int
	Precision       float64
	Recall          float64
}

// EvaluateWithVisibility repeats the §9.6 measurement while the resolver can
// only observe `visible` of the run when deciding presence.
//
// visible = 1.0 models resolution deferred until the run is complete.
// visible = 0.5 models a 2-way shard, 0.25 a 4-way shard, and so on.
func EvaluateWithVisibility(cfg Config, suite []Identity, cases []RenameCase, visible float64, repoRoot string, now time.Time, rng *rand.Rand) VisibilityResult {
	aliases := AliasStore{}
	cands := make([]Candidate, 0, len(suite))
	for i, id := range suite {
		sid := fmt.Sprintf("st_%04d", i)
		aliases[id.Fingerprint(repoRoot)] = sid
		cands = append(cands, Candidate{StableID: sid, Identity: id, LastSeenAt: now.Add(-24 * time.Hour)})
	}
	r := &Resolver{Aliases: aliases, Candidates: cands, RepoRoot: repoRoot, Config: cfg}

	idxByID := make(map[string]int, len(cands))
	for i, c := range cands {
		idxByID[c.StableID] = i
	}

	out := VisibilityResult{VisibleFraction: visible}
	for _, c := range cases {
		// Ground truth: every test except the renamed one is still in the suite.
		// But the resolver only KNOWS about the fraction reported so far.
		for i := range r.Candidates {
			observed := rng.Float64() < visible
			// PresentInRun can only be true for something actually observed.
			// Anything not yet reported looks absent — the ambiguity under test.
			r.Candidates[i].PresentInRun = observed
		}
		if i, ok := idxByID[c.StableID]; ok {
			r.Candidates[i].PresentInRun = false // genuinely renamed, genuinely gone
		}

		got := r.Resolve(c.After, now)
		switch got.Decision {
		case DecisionExactMatch, DecisionAutoMatch:
			out.AutoMatched++
			if got.MatchedID == c.StableID {
				out.AutoCorrect++
			} else {
				out.AutoWrong++
			}
		case DecisionReview:
			out.Review++
		case DecisionNew:
			out.NewIdentity++
		}
	}
	if out.AutoMatched > 0 {
		out.Precision = float64(out.AutoCorrect) / float64(out.AutoMatched)
	}
	if len(cases) > 0 {
		out.Recall = float64(out.AutoCorrect) / float64(len(cases))
	}
	return out
}

// VisibilitySweep runs the experiment across visibility levels.
func VisibilitySweep(suiteSize, mutate int, seed int64, repoRoot string) []VisibilityResult {
	levels := []float64{1.00, 0.90, 0.75, 0.50, 0.25, 0.10}
	out := make([]VisibilityResult, 0, len(levels))
	for _, v := range levels {
		rng := rand.New(rand.NewSource(seed))
		suite := BaseSuite(suiteSize, rng)
		cases := BuildRenameCases(suite, mutate, rng)
		out = append(out, EvaluateWithVisibility(DefaultConfig(), suite, cases, v, repoRoot, timeRef(), rand.New(rand.NewSource(seed+1))))
	}
	return out
}

func timeRef() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) }

// ReportVisibility renders the sweep, highest visibility first.
func ReportVisibility(rs []VisibilityResult) string {
	// Sort a copy: a report function has no business reordering its caller's
	// slice, and the sort has to happen BEFORE the rows are written to have any
	// effect at all.
	rows := append([]VisibilityResult(nil), rs...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].VisibleFraction > rows[j].VisibleFraction })

	var b strings.Builder
	fmt.Fprintf(&b, "RUN VISIBILITY vs IDENTITY ACCURACY\n")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 68))
	fmt.Fprintf(&b, "How much of the run the resolver can observe when judging presence.\n")
	fmt.Fprintf(&b, "1.00 = resolution deferred to run completion; 0.25 = a 4-way shard.\n\n")
	fmt.Fprintf(&b, "  %-9s %11s %9s %8s %8s %8s\n", "visible", "precision", "recall", "auto", "WRONG", "review")
	for _, r := range rows {
		flag := ""
		if r.Precision < 0.98 {
			flag = "  <-- below §9.6 floor"
		}
		fmt.Fprintf(&b, "  %-9.2f %11.4f %9.4f %8d %8d %8d%s\n",
			r.VisibleFraction, r.Precision, r.Recall, r.AutoMatched, r.AutoWrong, r.Review, flag)
	}
	return b.String()
}
