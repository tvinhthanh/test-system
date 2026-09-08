package identity

import (
	"sort"
	"time"
)

// Thresholds and weights live in Config (config.go), not in package globals.
// Decision is the outcome of resolving an unknown fingerprint.
type Decision string

const (
	DecisionExactMatch Decision = "exact_match"  // alias hit, the hot path
	DecisionAutoMatch  Decision = "auto_match"   // merged automatically into an existing identity
	DecisionReview     Decision = "review_queue" // new identity, pair queued for a human to review
	DecisionNew        Decision = "new_identity" // new identity, no question asked
)

// Resolution carries everything the Review Queue needs to display a decision.
type Resolution struct {
	Decision    Decision
	MatchedID   string // stableTestId when matched, empty when a new identity was created
	Score       float64
	RunnerUpID  string
	RunnerUp    float64
	Gap         float64
	Breakdown   ScoreBreakdown
	Fingerprint string
}

// AliasStore maps fingerprint to the currently valid stableTestId (validTo IS NULL).
type AliasStore map[string]string

// Resolver runs the SPEC §9.3 pipeline.
type Resolver struct {
	Aliases    AliasStore
	Candidates []Candidate
	RepoRoot   string
	Config     Config // zero value falls back to DefaultConfig
}

// Resolve runs the four steps of §9.3 for one incoming identity.
// cfg returns the resolver's config, falling back to defaults when unset.
func (r *Resolver) cfg() Config {
	if r.Config.ThresholdAutoMatch == 0 {
		return DefaultConfig()
	}
	return r.Config
}

func (r *Resolver) Resolve(in Identity, now time.Time) Resolution {
	cfg := r.cfg()
	fp := in.Fingerprint(r.RepoRoot)

	// Step 1 — exact match. The hot path, over 99% of events, O(1) via index.
	if id, ok := r.Aliases[fp]; ok {
		return Resolution{Decision: DecisionExactMatch, MatchedID: id, Score: 1, Fingerprint: fp}
	}

	// Step 2 — candidate set.
	cands := r.candidateSet(in)
	if len(cands) == 0 {
		return Resolution{Decision: DecisionNew, Fingerprint: fp}
	}

	// Step 3 — score every candidate.
	type scored struct {
		c Candidate
		b ScoreBreakdown
	}
	all := make([]scored, 0, len(cands))
	for _, c := range cands {
		all = append(all, scored{c, cfg.Score(in, c, now, r.RepoRoot)})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].b.Adjusted > all[j].b.Adjusted })

	top := all[0]
	res := Resolution{
		Score:       top.b.Adjusted,
		Breakdown:   top.b,
		Fingerprint: fp,
	}
	gap := top.b.Adjusted
	if len(all) > 1 {
		res.RunnerUpID = all[1].c.StableID
		res.RunnerUp = all[1].b.Adjusted
		if cfg.GapOver == GapTitle {
			gap = top.b.Title - all[1].b.Title
		} else {
			gap = top.b.Adjusted - all[1].b.Adjusted
		}
	}
	res.Gap = gap

	// Step 4 — apply thresholds.
	switch {
	case top.b.Adjusted >= cfg.ThresholdAutoMatch && gap >= cfg.MinGap && !cfg.staleBeyondGate(top.c, now):
		res.Decision = DecisionAutoMatch
		res.MatchedID = top.c.StableID
	case top.b.Adjusted >= cfg.ThresholdReview:
		// Create the new identity immediately so ingest is never blocked, and queue the pair for review.
		res.Decision = DecisionReview
		res.MatchedID = top.c.StableID // suggested candidate for the reviewer
	default:
		res.Decision = DecisionNew
	}
	return res
}

// candidateSet — SPEC §9.3 step 2: same project, satisfying at least one of
// three conditions, capped at the 50 highest rough scores.
func (r *Resolver) candidateSet(in Identity) []Candidate {
	n := in.Normalize(r.RepoRoot)
	leaf := n.Leaf()

	type rough struct {
		c Candidate
		s float64
	}
	var picked []rough
	for _, c := range r.Candidates {
		cn := c.Identity.Normalize(r.RepoRoot)
		sameFile := cn.FilePath == n.FilePath
		sameLeaf := cn.Leaf() == leaf
		trg := TrigramSimilarity(cn.Leaf(), leaf)
		if sameFile || sameLeaf || trg >= 0.4 {
			s := trg
			if sameFile {
				s += 1
			}
			if sameLeaf {
				s += 1
			}
			picked = append(picked, rough{c, s})
		}
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].s > picked[j].s })
	if len(picked) > 50 {
		picked = picked[:50]
	}
	out := make([]Candidate, 0, len(picked))
	for _, p := range picked {
		out = append(out, p.c)
	}
	return out
}

// staleBeyondGate reports whether a candidate is too old for the rename
// rationale to apply — see RecencyGateDays and finding N45.
func (cfg Config) staleBeyondGate(c Candidate, now time.Time) bool {
	if cfg.RecencyGateDays <= 0 || c.LastSeenAt.IsZero() {
		return false
	}
	return now.Sub(c.LastSeenAt).Hours()/24 > cfg.RecencyGateDays
}
