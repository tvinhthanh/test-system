package identity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Diagnose explains WHY rename cases fail to auto-match.
// It separates "score too low" (the algorithm missed it) from "blocked by the
// gap rule" (it found the match but would not commit) — different fixes.
type Diagnosis struct {
	Total            int
	ScoreAboveAuto   int // score ≥ cfg.ThresholdAutoMatch
	BlockedByGap     int // ...nhưng gap < cfg.MinGap → bị đẩy sang review
	ScoreInReview    int // cfg.ThresholdReview ≤ score < cfg.ThresholdAutoMatch
	ScoreBelowReview int
	TopScoreCorrect  int // top candidate really is the original identity
	Gaps             []float64
	Scores           []float64
}

func Diagnose(cfg Config, suite []Identity, cases []RenameCase, repoRoot string, now time.Time) Diagnosis {
	aliases := AliasStore{}
	cands := make([]Candidate, 0, len(suite))
	for i, id := range suite {
		sid := fmt.Sprintf("st_%04d", i)
		aliases[id.Fingerprint(repoRoot)] = sid
		cands = append(cands, Candidate{StableID: sid, Identity: id, LastSeenAt: now.Add(-24 * time.Hour)})
	}
	r := &Resolver{Aliases: aliases, Candidates: cands, RepoRoot: repoRoot, Config: cfg}

	// Index candidates by stableID so PresentInRun can be toggled per case,
	// exactly as Evaluate does. This function used to leave PresentInRun at its
	// zero value for every candidate — the N32/N33 harness bug, fixed in
	// Evaluate on 2026-08-26 and never carried over here. A diagnosis of a
	// world where all 500 tests vanished at once explains nothing about the
	// world Evaluate measures, and the two printed side by side did not agree
	// (N59).
	idxByID := make(map[string]int, len(cands))
	for i, c := range cands {
		idxByID[c.StableID] = i
	}

	d := Diagnosis{Total: len(cases)}
	for _, c := range cases {
		// Model reality: only the renamed test is absent from this run.
		for i := range r.Candidates {
			r.Candidates[i].PresentInRun = true
		}
		if i, ok := idxByID[c.StableID]; ok {
			r.Candidates[i].PresentInRun = false
		}

		got := r.Resolve(c.After, now)
		if got.Decision == DecisionExactMatch {
			d.ScoreAboveAuto++
			d.TopScoreCorrect++
			continue
		}
		d.Scores = append(d.Scores, got.Score)
		d.Gaps = append(d.Gaps, got.Gap)
		if got.MatchedID == c.StableID {
			d.TopScoreCorrect++
		}
		switch {
		case got.Score >= cfg.ThresholdAutoMatch:
			d.ScoreAboveAuto++
			if got.Gap < cfg.MinGap {
				d.BlockedByGap++
			}
		case got.Score >= cfg.ThresholdReview:
			d.ScoreInReview++
		default:
			d.ScoreBelowReview++
		}
	}
	return d
}

func (d Diagnosis) Report(cfg Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "DIAGNOSIS — why auto-match did not fire\n")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 50))
	fmt.Fprintf(&b, "Total cases                          : %d\n", d.Total)
	fmt.Fprintf(&b, "Top candidate is the RIGHT identity  : %d  (%.1f%%)\n",
		d.TopScoreCorrect, 100*float64(d.TopScoreCorrect)/float64(d.Total))
	fmt.Fprintf(&b, "\nDistribution across thresholds:\n")
	fmt.Fprintf(&b, "  score >= %.2f (auto-match eligible) : %d\n", cfg.ThresholdAutoMatch, d.ScoreAboveAuto)
	fmt.Fprintf(&b, "     of which gap < %.2f, blocked     : %d   <-- HERE\n", cfg.MinGap, d.BlockedByGap)
	fmt.Fprintf(&b, "  %.2f <= score < %.2f (review)       : %d\n", cfg.ThresholdReview, cfg.ThresholdAutoMatch, d.ScoreInReview)
	fmt.Fprintf(&b, "  score < %.2f (new identity)          : %d\n", cfg.ThresholdReview, d.ScoreBelowReview)

	if len(d.Gaps) > 0 {
		g := append([]float64{}, d.Gaps...)
		sort.Float64s(g)
		s := append([]float64{}, d.Scores...)
		sort.Float64s(s)
		fmt.Fprintf(&b, "\nGap top-1 vs top-2:  p50=%.4f  p90=%.4f  max=%.4f\n",
			g[len(g)/2], g[len(g)*9/10], g[len(g)-1])
		fmt.Fprintf(&b, "Top-1 score:         p50=%.4f  p90=%.4f  max=%.4f\n",
			s[len(s)/2], s[len(s)*9/10], s[len(s)-1])
	}
	return b.String()
}
