package identity

import "testing"

// TestSeparatorPreventsCollision locks down SPEC finding N22.
//
// The separator between titlePath elements MUST be U+0000. If anyone swaps it
// for a space or the empty string this test must fail immediately: two distinct
// tests would hash to the same fingerprint and be merged into one, which §9.6
// ranks as more damaging than splitting a test in two.
func TestSeparatorPreventsCollision(t *testing.T) {
	base := Identity{Framework: "playwright", FilePath: "tests/a.spec.ts"}

	cases := []struct {
		name string
		a, b []string
	}{
		{"empty separator would merge", []string{"Check", "outPayment"}, []string{"Checkout", "Payment"}},
		{"space separator would merge", []string{"a b", "c"}, []string{"a", "b c"}},
		{"three-element join", []string{"x", "y", "z"}, []string{"xy", "z"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y := base, base
			x.TitlePath, y.TitlePath = c.a, c.b
			if fx, fy := x.Fingerprint(""), y.Fingerprint(""); fx == fy {
				t.Fatalf("COLLISION: %v and %v share fingerprint %s", c.a, c.b, fx)
			}
		})
	}
}

func TestFingerprintDeterministic(t *testing.T) {
	id := Identity{
		Framework:      "playwright",
		FilePath:       "tests/checkout/payment.spec.ts",
		TitlePath:      []string{"Checkout", "Payment", "declines expired card"},
		ParamSignature: "browser=firefox",
	}
	first := id.Fingerprint("")
	for i := 0; i < 100; i++ {
		if got := id.Fingerprint(""); got != first {
			t.Fatalf("not deterministic: run %d gave %s, first run gave %s", i, got, first)
		}
	}
	if len(id.ClientTestID("")) != 35 { // "ct_" + 32 hex chars
		t.Errorf("ClientTestID has wrong length: %s", id.ClientTestID(""))
	}
}

// TestNormalizationStability: differences that carry no meaning must not change
// the fingerprint.
func TestNormalizationStability(t *testing.T) {
	a := Identity{Framework: "playwright", FilePath: "tests\\auth\\login.spec.TS",
		TitlePath: []string{"  Auth  ", "Login\tflow"}}
	b := Identity{Framework: "playwright", FilePath: "tests/auth/login.spec.ts",
		TitlePath: []string{"Auth", "Login flow"}}
	if a.Fingerprint("") != b.Fingerprint("") {
		t.Errorf("normalisation unstable:\n  a=%s\n  b=%s", a.Fingerprint(""), b.Fingerprint(""))
	}
}

// TestParamOrderIrrelevant: declaration order of parameters must not matter.
func TestParamOrderIrrelevant(t *testing.T) {
	p1 := NormalizeParams(map[string]string{"browser": "firefox", "viewport": "mobile"})
	p2 := NormalizeParams(map[string]string{"viewport": "mobile", "browser": "firefox"})
	if p1 != p2 {
		t.Errorf("parameter sort unstable: %q vs %q", p1, p2)
	}
}

// TestRepoRootStripped: the same test on a dev machine and on CI must produce
// the same fingerprint.
func TestRepoRootStripped(t *testing.T) {
	dev := Identity{Framework: "playwright", FilePath: "C:/work/myapp/tests/a.spec.ts", TitlePath: []string{"T"}}
	ci := Identity{Framework: "playwright", FilePath: "/home/runner/myapp/tests/a.spec.ts", TitlePath: []string{"T"}}
	if dev.Fingerprint("C:/work/myapp") != ci.Fingerprint("/home/runner/myapp") {
		t.Error("repo root not stripped: the same test on dev and CI produced different fingerprints")
	}
}

func TestScoreFileRules(t *testing.T) {
	tests := []struct {
		name, a, b string
		want       float64
	}{
		{"identical", "tests/a/x.spec.ts", "tests/a/x.spec.ts", 1.0},
		{"same dir, different name", "tests/a/x.spec.ts", "tests/a/y.spec.ts", 0.6},
		{"same name, different dir", "tests/a/x.spec.ts", "tests/b/x.spec.ts", 0.7},
	}
	for _, tt := range tests {
		if got := ScoreFile(tt.a, tt.b); got != tt.want {
			t.Errorf("%s: ScoreFile = %.2f, want %.2f", tt.name, got, tt.want)
		}
	}
}

// TestPenaltyBlocksWrongMerge covers the anti-collision signal from §9.3: if a
// candidate still ran in this run, the incoming test is new, not a rename.
func TestPenaltyBlocksWrongMerge(t *testing.T) {
	in := Identity{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "new test"}}
	cand := Candidate{
		StableID: "st_1",
		Identity: Identity{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "new tests"}},
	}
	cand.PresentInRun = false
	absent := DefaultConfig().Score(in, cand, timeNow(), "").Adjusted
	cand.PresentInRun = true
	present := DefaultConfig().Score(in, cand, timeNow(), "").Adjusted

	if present >= absent {
		t.Errorf("penalty had no effect: present=%.4f >= absent=%.4f", present, absent)
	}
	if present >= DefaultConfig().ThresholdAutoMatch {
		t.Errorf("candidate still in the run still reached auto-match: %.4f", present)
	}
}
