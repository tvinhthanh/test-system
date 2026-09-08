package identity

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// ScaleSweep measures how fuzzy-path cost grows with project size and with the
// number of unknown fingerprints in one run — the bulk-refactor case.
func ScaleSweep() string {
	var b strings.Builder
	fmt.Fprintf(&b, "FUZZY PATH COST\n")
	fmt.Fprintf(&b, "%s\n", strings.Repeat("=", 62))
	fmt.Fprintf(&b, "Candidate filter (SPEC 9.3 step 2) scans every active identity.\n")
	fmt.Fprintf(&b, "This prototype has no index; Postgres would need one (see 8.9).\n\n")
	fmt.Fprintf(&b, "  %-12s %14s %16s\n", "project size", "per unknown fp", "50 renames")
	for _, n := range []int{500, 2000, 10000, 25000} {
		r, suite := buildStore(n)
		rng := rand.New(rand.NewSource(11))
		now := time.Now()
		probes := make([]Identity, 20)
		for i := range probes {
			probes[i] = Mutate(suite[i*37%len(suite)], MutateLeafTitle, rng)
		}
		start := time.Now()
		for _, p := range probes {
			r.Resolve(p, now)
		}
		per := time.Since(start) / time.Duration(len(probes))
		fmt.Fprintf(&b, "  %-12d %14s %16s\n", n, per.Round(time.Microsecond), (per * 50).Round(time.Millisecond))
	}
	fmt.Fprintf(&b, "\nCost is linear in project size: every unknown fingerprint is compared\n")
	fmt.Fprintf(&b, "against every active identity before the 50-candidate cap applies.\n")
	return b.String()
}
