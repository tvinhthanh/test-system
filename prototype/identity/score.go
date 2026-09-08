package identity

import (
	"path"
	"strings"
	"time"
)

// Candidate is an active TestIdentity being compared against.
type Candidate struct {
	StableID   string
	Identity   Identity
	LastSeenAt time.Time
	// PresentInRun means this identity still appears in the run being processed
	// under its old fingerprint, which rules out a rename.
	PresentInRun bool
}

// ScoreBreakdown keeps each component so the Review Queue can explain a decision.
type ScoreBreakdown struct {
	Title    float64
	File     float64
	Param    float64
	Recency  float64
	Base     float64 // weighted total, before reinforcement signals
	Adjusted float64 // after reinforcement signals
}

// ScoreTitle: identical leaf scores 1.0; otherwise Levenshtein on the leaf (0.7) plus Jaccard on the ancestors (0.3).
func (c Config) ScoreTitle(a, b Identity) float64 {
	if a.Leaf() == b.Leaf() {
		return 1
	}
	leaf := LevenshteinSimilarity(a.Leaf(), b.Leaf())
	anc := JaccardSimilarity(a.Ancestors(), b.Ancestors())
	return c.WeightLeaf*leaf + c.WeightAncestors*anc
}

// ScoreFile per SPEC §9.3: identical 1.0; same directory different filename 0.6;
// same filename different directory (a moved file) 0.7; otherwise trigram similarity.
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

// ScoreParam: identical 1.0; same key set with different values 0.5; different key set 0.
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
		if i := strings.Index(p, "="); i >= 0 {
			out = append(out, p[:i])
		} else {
			out = append(out, p)
		}
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

// ScoreRecency: 1.0 within 7 days, decaying linearly to 0 at 90 days.
//
// Why this signal exists: a rename shows up on the very next run, so an identity
// disappearing at exactly the moment an unknown fingerprint appears is strong evidence.
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

// Score rates one candidate against the incoming identity — SPEC §9.3 step 3.
func (cfg Config) Score(incoming Identity, c Candidate, now time.Time, repoRoot string) ScoreBreakdown {
	in := incoming.Normalize(repoRoot)
	cand := c.Identity.Normalize(repoRoot)

	b := ScoreBreakdown{
		Title:   cfg.ScoreTitle(in, cand),
		File:    ScoreFile(in.FilePath, cand.FilePath),
		Param:   ScoreParam(in.ParamSignature, cand.ParamSignature),
		Recency: cfg.ScoreRecency(c.LastSeenAt, now),
	}
	b.Base = cfg.WeightTitle*b.Title + cfg.WeightFile*b.File + cfg.WeightParam*b.Param + cfg.WeightRecency*b.Recency

	// Reinforcement signals.
	b.Adjusted = b.Base
	if c.PresentInRun {
		// The candidate still ran in this run under its old fingerprint, so the
		// incoming test is genuinely new. Penalise hard to prevent a wrong merge.
		b.Adjusted *= cfg.PenaltyCandidatePresent
	} else {
		b.Adjusted += cfg.BonusCandidateAbsent
	}
	if b.Adjusted > 1 {
		b.Adjusted = 1
	}
	return b
}
