package identity

// Config holds every tunable in SPEC §9.3.
//
// These used to be package-level variables. That was wrong in a way that went
// beyond taste: tests mutated them and restored with defer, so a failing test
// left the next one running under someone else's thresholds, and nothing could
// run in parallel. Sweeps that vary a parameter now build a Config instead of
// reaching into shared state.
//
// The zero value is not usable — call DefaultConfig and adjust.
type Config struct {
	// Component weights, §9.3 step 3.
	WeightTitle   float64
	WeightFile    float64
	WeightParam   float64
	WeightRecency float64

	// Within S_title.
	WeightLeaf      float64
	WeightAncestors float64

	// Reinforcement signals. The multiplier is load-bearing — see N34.
	BonusCandidateAbsent    float64
	PenaltyCandidatePresent float64

	// Recency decay bounds, in days.
	RecencyFullDays float64
	RecencyZeroDays float64

	// Decision thresholds, §9.3 step 4.
	ThresholdAutoMatch float64
	ThresholdReview    float64
	MinGap             float64

	// GapOver selects what the top-1/top-2 distance is measured on.
	// Largely moot once the presence signal is active (N34), kept because the
	// sweep that established that is worth being able to re-run.
	GapOver GapMode

	// RecencyGateDays optionally blocks auto-match for stale candidates (N45).
	// Zero disables the gate, which is current spec behaviour.
	RecencyGateDays float64
}

// GapMode selects what the top-1 vs top-2 distance is measured on.
type GapMode string

const (
	GapTotal GapMode = "total" // over the total score, as §9.3 reads today
	GapTitle GapMode = "title" // over S_title only
)

// DefaultConfig returns the values SPEC §9.3 specifies.
// All of them are [ASSUMPTION] starting points to be recalibrated on real
// partner data (§9.6); record the final numbers in the schema changelog.
func DefaultConfig() Config {
	return Config{
		WeightTitle:   0.45,
		WeightFile:    0.25,
		WeightParam:   0.20,
		WeightRecency: 0.10,

		WeightLeaf:      0.7,
		WeightAncestors: 0.3,

		BonusCandidateAbsent:    0.05,
		PenaltyCandidatePresent: 0.3,

		RecencyFullDays: 7,
		RecencyZeroDays: 90,

		ThresholdAutoMatch: 0.85,
		ThresholdReview:    0.60,
		MinGap:             0.10,

		GapOver:         GapTotal,
		RecencyGateDays: 0,
	}
}

// WithRecencyGate returns a copy with the staleness gate enabled (N45).
func (c Config) WithRecencyGate(days float64) Config { c.RecencyGateDays = days; return c }

// WithMinGap returns a copy with a different top-1/top-2 gap requirement.
func (c Config) WithMinGap(g float64) Config { c.MinGap = g; return c }

// WithGapOver returns a copy measuring the gap over a different component.
func (c Config) WithGapOver(m GapMode) Config { c.GapOver = m; return c }
