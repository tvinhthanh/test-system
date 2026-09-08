package identity

import "testing"

// TestPlaywrightIDIsNotStable locks down finding N37.
//
// Playwright's spec.id has the shape <file-hash>-<test-hash> and BREAKS on both
// a test rename and a file move. It is not usable as a durable identity — even
// though it is the first thing anyone reaches for ("Playwright already has an
// id, just use that").
//
// Both fixtures come from real Playwright runs: report-after-rename is the same
// suite after renaming one test and moving one file.
func TestPlaywrightIDIsNotStable(t *testing.T) {
	before, err := LoadPlaywrightReport("testdata/playwright-report.json")
	if err != nil {
		t.Fatal(err)
	}
	after, err := LoadPlaywrightReport("testdata/playwright-report-after-rename.json")
	if err != nil {
		t.Fatal(err)
	}

	beforeIDs := map[string]bool{}
	for _, x := range before {
		beforeIDs[x.PlaywrightID] = true
	}
	broken := 0
	for _, x := range after {
		if !beforeIDs[x.PlaywrightID] {
			broken++
		}
	}
	if broken == 0 {
		t.Fatal("spec.id survived rename+move — re-verify N37 before changing the spec")
	}
	t.Logf("spec.id broke on %d/%d entries after renaming one test and moving one file", broken, len(after))

	// Our fingerprint breaks at the exact-match step too — that is precisely why
	// §9.3 has steps 2-4. The difference: we have a recovery path, spec.id has none.
	beforeFP := map[string]bool{}
	for _, x := range before {
		beforeFP[x.Identity.Fingerprint("")] = true
	}
	fpBroken := 0
	for _, x := range after {
		if !beforeFP[x.Identity.Fingerprint("")] {
			fpBroken++
		}
	}
	t.Logf("fp1 fingerprint broke on %d/%d entries — recovered by §9.3 steps 2-4", fpBroken, len(after))
}
