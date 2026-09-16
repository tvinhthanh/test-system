package match

import (
	"path"
	"sort"
	"strings"
	"time"
)

// Config holds every tunable of §9.3. All values are [ASSUMPTION] starting
// points; the prototype confirmed them on synthetic data (§9.6) and partner
// data may move them — record the final numbers in the schema changelog.
type Config struct {
	WeightTitle, WeightFile, WeightParam, WeightRecency float64
	WeightLeaf, WeightAncestors                         float64

	// Reinforcement signals. The penalty multiplier is load-bearing (N34).
	BonusCandidateAbsent    float64
	PenaltyCandidatePresent float64

	RecencyFullDays, RecencyZeroDays float64

	ThresholdAutoMatch, ThresholdReview float64
	// MinGap is the safety net (N40): auto-match needs top-1 to lead top-2
	// by at least this much.
	MinGap float64

	// RecencyGateDays optionally blocks auto-match for stale candidates (N45).
	// Zero disables it, which is what the SPEC says today.
	RecencyGateDays float64

	// MaxCandidates caps step 2 (§9.3: 50).
	MaxCandidates int
}

// DefaultConfig is §9.3 as written.
func DefaultConfig() Config {
	return Config{
		WeightTitle: 0.45, WeightFile: 0.25, WeightParam: 0.20, WeightRecency: 0.10,
		WeightLeaf: 0.7, WeightAncestors: 0.3,
		BonusCandidateAbsent: 0.05, PenaltyCandidatePresent: 0.3,
		RecencyFullDays: 7, RecencyZeroDays: 90,
		ThresholdAutoMatch: 0.85, ThresholdReview: 0.60, MinGap: 0.10,
		RecencyGateDays: 0,
		MaxCandidates:   50,
	}
}

// Candidate is an active TestIdentity being compared against.
type Candidate struct {
	ID         string
	Signals    Signals
	LastSeenAt time.Time
	// PresentInRun: this identity still appears in the run being processed
	// under its own fingerprint, which rules out a rename.
	PresentInRun bool
}

// Breakdown keeps each component so the Review Queue can explain a decision.
type Breakdown struct {
	Title    float64 `json:"title"`
	File     float64 `json:"file"`
	Param    float64 `json:"param"`
	Recency  float64 `json:"recency"`
	Base     float64 `json:"base"`     // weighted total before reinforcement
	Adjusted float64 `json:"adjusted"` // after reinforcement
}

// ScoreTitle: identical leaf = 1.0; otherwise Levenshtein on the leaf (0.7)
// plus Jaccard on the ancestors (0.3).
func (c Config) ScoreTitle(a, b Signals) float64 {
	if a.Leaf() == b.Leaf() {
		return 1
	}
	return c.WeightLeaf*LevenshteinSimilarity(a.Leaf(), b.Leaf()) + c.WeightAncestors*JaccardSimilarity(a.Ancestors(), b.Ancestors())
}

// ScoreFile per §9.3: identical 1.0; same directory different name 0.6; same
// name different directory (a moved file) 0.7; otherwise trigram similarity.
func ScoreFile(a, b string) float64 {
	if a == b {
		return 1
	}
	dirA, fileA := path.Split(a)
	dirB, fileB := path.Split(b)
	switch {
	case dirA == dirB && fileA != fileB:
		return 0.6
	case fileA == fileB && dirA != dirB:
		return 0.7
	default:
		return TrigramSimilarity(a, b)
	}
}

// ScoreParam: identical 1.0; same key set with different values 0.5;
// different key set 0.
func ScoreParam(a, b string) float64 {
	if a == b {
		return 1
	}
	ka, kb := paramKeys(a), paramKeys(b)
	if len(ka) == 0 && len(kb) == 0 {
		return 1
	}
	if sameKeySet(ka, kb) {
		return 0.5
	}
	return 0
}

func paramKeys(sig string) []string {
	if sig == "" {
		return nil
	}
	parts := strings.Split(sig, "&")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		k, _, _ := strings.Cut(p, "=")
		out = append(out, k)
	}
	return out
}

func sameKeySet(a, b []string) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, k := range a {
		set[k] = struct{}{}
	}
	for _, k := range b {
		if _, ok := set[k]; !ok {
			return false
		}
	}
	return true
}

// ScoreRecency: 1.0 within 7 days, decaying linearly to 0 at 90 days. A
// rename shows up on the very next run, so an identity disappearing exactly
// when an unknown fingerprint appears is strong evidence.
func (c Config) ScoreRecency(lastSeen, now time.Time) float64 {
	if lastSeen.IsZero() {
		return 0
	}
	days := now.Sub(lastSeen).Hours() / 24
	switch {
	case days <= c.RecencyFullDays:
		return 1
	case days >= c.RecencyZeroDays:
		return 0
	default:
		return 1 - (days-c.RecencyFullDays)/(c.RecencyZeroDays-c.RecencyFullDays)
	}
}

// Score rates one candidate against the incoming signals — §9.3 step 3.
func (c Config) Score(incoming Signals, cand Candidate, now time.Time) Breakdown {
	in, cs := incoming.Normalize(), cand.Signals.Normalize()
	b := Breakdown{
		Title:   c.ScoreTitle(in, cs),
		File:    ScoreFile(in.FilePath, cs.FilePath),
		Param:   ScoreParam(in.ParamSignature, cs.ParamSignature),
		Recency: c.ScoreRecency(cand.LastSeenAt, now),
	}
	b.Base = c.WeightTitle*b.Title + c.WeightFile*b.File + c.WeightParam*b.Param + c.WeightRecency*b.Recency
	b.Adjusted = b.Base
	if cand.PresentInRun {
		// The candidate still ran in this run under its old fingerprint, so
		// the incoming test is genuinely new. Penalise hard (N34).
		b.Adjusted *= c.PenaltyCandidatePresent
	} else {
		b.Adjusted += c.BonusCandidateAbsent
	}
	if b.Adjusted > 1 {
		b.Adjusted = 1
	}
	return b
}

// Decision is the outcome of §9.3 step 4 for one unknown fingerprint.
type Decision string

const (
	DecisionAutoMatch Decision = "auto_match"   // alias the fingerprint to the top candidate
	DecisionReview    Decision = "review_queue" // new identity now, pair queued for a human
	DecisionNew       Decision = "new_identity" // new identity, no question asked
)

// Verdict carries everything the Review Queue needs to explain a decision.
type Verdict struct {
	Decision   Decision
	MatchedID  string // top candidate: the match for auto_match, the suggestion for review
	Score      float64
	RunnerUpID string
	RunnerUp   float64
	Gap        float64
	Breakdown  Breakdown
}

// Decide runs steps 3–4 over an already-filtered candidate set (step 2 is a
// Postgres query in the identity package).
func (c Config) Decide(in Signals, cands []Candidate, now time.Time) Verdict {
	if len(cands) == 0 {
		return Verdict{Decision: DecisionNew}
	}
	type scored struct {
		c Candidate
		b Breakdown
	}
	all := make([]scored, 0, len(cands))
	for _, cand := range cands {
		all = append(all, scored{cand, c.Score(in, cand, now)})
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].b.Adjusted > all[j].b.Adjusted })

	top := all[0]
	v := Verdict{Score: top.b.Adjusted, Breakdown: top.b, Gap: top.b.Adjusted}
	if len(all) > 1 {
		v.RunnerUpID = all[1].c.ID
		v.RunnerUp = all[1].b.Adjusted
		v.Gap = top.b.Adjusted - all[1].b.Adjusted
	}
	switch {
	case top.b.Adjusted >= c.ThresholdAutoMatch && v.Gap >= c.MinGap && !c.staleBeyondGate(top.c, now):
		v.Decision = DecisionAutoMatch
		v.MatchedID = top.c.ID
	case top.b.Adjusted >= c.ThresholdReview:
		v.Decision = DecisionReview
		v.MatchedID = top.c.ID
	default:
		v.Decision = DecisionNew
	}
	return v
}

// staleBeyondGate: the rename rationale stops applying to a candidate not
// seen for longer than RecencyGateDays (N45). Off by default.
func (c Config) staleBeyondGate(cand Candidate, now time.Time) bool {
	if c.RecencyGateDays <= 0 || cand.LastSeenAt.IsZero() {
		return false
	}
	return now.Sub(cand.LastSeenAt).Hours()/24 > c.RecencyGateDays
}

// RoughFilter is step 2 as the prototype computed it in memory: keep
// candidates that share the file, share the leaf, or have leaf trigram
// similarity ≥ 0.4, ranked by a rough score and capped. Production uses the
// equivalent Postgres query; this exists so the in-memory evaluation in tests
// runs the same pipeline the database does.
func (c Config) RoughFilter(in Signals, all []Candidate) []Candidate {
	n := in.Normalize()
	leaf := n.Leaf()
	type rough struct {
		c Candidate
		s float64
	}
	var picked []rough
	for _, cand := range all {
		cn := cand.Signals.Normalize()
		sameFile := cn.FilePath == n.FilePath
		sameLeaf := cn.Leaf() == leaf
		trg := TrigramSimilarity(cn.Leaf(), leaf)
		if sameFile || sameLeaf || trg >= 0.4 {
			s := trg
			if sameFile {
				s++
			}
			if sameLeaf {
				s++
			}
			picked = append(picked, rough{cand, s})
		}
	}
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].s > picked[j].s })
	if c.MaxCandidates > 0 && len(picked) > c.MaxCandidates {
		picked = picked[:c.MaxCandidates]
	}
	out := make([]Candidate, 0, len(picked))
	for _, p := range picked {
		out = append(out, p.c)
	}
	return out
}
