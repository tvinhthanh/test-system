package identity

import (
	"fmt"
	"math/rand"
	"time"
)

// buildStore creates a realistic project state of n tests.
func buildStore(n int) (*Resolver, []Identity) {
	rng := rand.New(rand.NewSource(7))
	suite := BaseSuite(n, rng)
	aliases := make(AliasStore, n)
	cands := make([]Candidate, 0, n)
	now := time.Now()
	for i, id := range suite {
		sid := fmt.Sprintf("st_%05d", i)
		aliases[id.Fingerprint("")] = sid
		cands = append(cands, Candidate{
			StableID: sid, Identity: id,
			LastSeenAt: now.Add(-24 * time.Hour), PresentInRun: true,
		})
	}
	return &Resolver{Aliases: aliases, Candidates: cands}, suite
}
