package identity

import (
	"strings"
	"testing"
)

// TestLoadRealPlaywrightReport runs the loader against a REAL Playwright report
// (testdata/playwright-report.json, produced by an actual Playwright run).
func TestLoadRealPlaywrightReport(t *testing.T) {
	tests, err := LoadPlaywrightReport("testdata/playwright-report.json")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(tests) != 14 {
		t.Fatalf("want 14 entries (7 specs x 2 projects), got %d", len(tests))
	}

	// Two projects of the same spec must not share a fingerprint.
	byFP := map[string]int{}
	for _, x := range tests {
		byFP[x.Identity.Fingerprint("")]++
	}
	if len(byFP) != 14 {
		t.Errorf("want 14 distinct fingerprints, got %d — collision", len(byFP))
	}

	// titlePath must never contain the file path.
	for _, x := range tests {
		for _, seg := range x.Identity.TitlePath {
			if isFileTitle(seg) {
				t.Errorf("file name leaked into titlePath: %v", x.Identity.TitlePath)
			}
		}
	}

	// Status mapping must reproduce the report's own stats: 10 expected, 2 skipped, 2 unexpected.
	count := map[string]int{}
	for _, x := range tests {
		count[x.FinalStatus]++
	}
	if count["passed"] != 10 || count["skipped"] != 2 || count["failed"] != 2 {
		t.Errorf("status mapping wrong: %v", count)
	}
}

// TestForLoopParamsAreInTitle locks down finding N35.
//
// Playwright's most common parameterisation idiom — generating tests in a loop —
// puts the parameter in the TITLE, not in projectName. §9.2 assumes
// paramSignature captures parameterisation; for this idiom it does not.
func TestForLoopParamsAreInTitle(t *testing.T) {
	tests, err := LoadPlaywrightReport("testdata/playwright-report.json")
	if err != nil {
		t.Fatal(err)
	}
	var discount []LoadedTest
	for _, x := range tests {
		if len(x.Identity.TitlePath) == 1 && strings.HasPrefix(x.Identity.TitlePath[0], "applies ") {
			discount = append(discount, x)
		}
	}
	if len(discount) != 4 { // 2 loop values x 2 projects
		t.Fatalf("want 4 loop-generated entries, got %d", len(discount))
	}
	// Same project, different loop value: paramSignature is IDENTICAL, only the
	// title differs. That is N35.
	same := 0
	for i := range discount {
		for j := i + 1; j < len(discount); j++ {
			if discount[i].Identity.ParamSignature == discount[j].Identity.ParamSignature {
				same++
			}
		}
	}
	if same == 0 {
		t.Error("N35 no longer holds — re-verify the assumption before editing the spec")
	}
}

// TestNormalizedTemplateKeyGroupsLoopTests measures the N35 fix on real data.
func TestNormalizedTemplateKeyGroupsLoopTests(t *testing.T) {
	tests, err := LoadPlaywrightReport("testdata/playwright-report.json")
	if err != nil {
		t.Fatal(err)
	}

	plain := map[string]int{}
	norm := map[string]int{}
	fps := map[string]int{}
	for _, x := range tests {
		plain[x.Identity.TemplateKey("")]++
		norm[x.Identity.TemplateKeyNormalized("")]++
		fps[x.Identity.Fingerprint("")]++
	}

	t.Logf("distinct TemplateKey           : %d", len(plain))
	t.Logf("distinct TemplateKeyNormalized : %d", len(norm))
	t.Logf("distinct Fingerprint           : %d", len(fps))

	if len(norm) >= len(plain) {
		t.Errorf("normalisation grouped nothing: plain=%d normalized=%d", len(plain), len(norm))
	}

	// The boundary that must never move: identities stay distinct even when
	// their template key merges. Grouping is a dashboard concern, not identity.
	if len(fps) != 14 {
		t.Errorf("normalisation leaked into identity: %d distinct fingerprints, want 14", len(fps))
	}
}

func TestNormalizeDynamicPatterns(t *testing.T) {
	cases := []struct{ in, want string }{
		{"applies discount for 10", "applies discount for <num>"},
		{"order #8412 succeeds", "order #<num> succeeds"},
		{"user 3f2b1a9c8d7e4f60 logs in", "user <hex> logs in"},
		{"expires on 2026-08-26", "expires on <date>"},
		{"emails a@b.com", "emails <email>"},
		{"charges 19.99", "charges <num>"},
		{"declines expired card", "declines expired card"}, // untouched
	}
	for _, c := range cases {
		if got := NormalizeDynamic(c.in); got != c.want {
			t.Errorf("NormalizeDynamic(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
