package identity_test

import (
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/prototype/identity"
)

// ExampleConfig shows the tuning API documented in README.md.
//
// This lives as a compiled Example rather than a fenced block in the README so
// the documentation cannot drift away from the code: if the API changes, this
// stops building.
func ExampleConfig() {
	cfg := identity.DefaultConfig().WithRecencyGate(30)

	id := identity.Identity{
		Framework: "playwright",
		FilePath:  "tests/auth/login.spec.ts",
		TitlePath: []string{"Auth", "logs in"},
	}
	renamed := id
	renamed.TitlePath = []string{"Auth", "signs in"}

	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	r := &identity.Resolver{
		Config:  cfg,
		Aliases: identity.AliasStore{id.Fingerprint(""): "st_original"},
		Candidates: []identity.Candidate{{
			StableID:   "st_original",
			Identity:   id,
			LastSeenAt: now.Add(-60 * 24 * time.Hour), // stale by two months
		}},
	}

	fmt.Println(r.Resolve(renamed, now).Decision)
	// Output: review_queue
}

// ExampleIdentity_Fingerprint demonstrates the separator invariant from N22:
// two different title paths must never collide.
func ExampleIdentity_Fingerprint() {
	base := identity.Identity{Framework: "playwright", FilePath: "tests/a.spec.ts"}
	a, b := base, base
	a.TitlePath = []string{"Check", "outPayment"}
	b.TitlePath = []string{"Checkout", "Payment"}

	fmt.Println(a.Fingerprint("") == b.Fingerprint(""))
	// Output: false
}
