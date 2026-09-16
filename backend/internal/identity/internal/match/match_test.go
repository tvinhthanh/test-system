package match

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

// ---- N22: the separator ---------------------------------------------------

func TestSeparatorPreventsCollision(t *testing.T) {
	base := Signals{Framework: "playwright", FilePath: "tests/a.spec.ts"}
	for _, c := range []struct {
		name string
		a, b []string
	}{
		{"empty separator would merge", []string{"Check", "outPayment"}, []string{"Checkout", "Payment"}},
		{"space separator would merge", []string{"a b", "c"}, []string{"a", "b c"}},
		{"three-element join", []string{"x", "y", "z"}, []string{"xy", "z"}},
	} {
		x, y := base, base
		x.TitlePath, y.TitlePath = c.a, c.b
		if fx, fy := x.Fingerprint(), y.Fingerprint(); fx == fy {
			t.Errorf("%s: COLLISION %v and %v share %s", c.name, c.a, c.b, fx)
		}
	}
}

func TestFingerprintShapeAndStability(t *testing.T) {
	id := Signals{Framework: "playwright", FilePath: "tests/checkout/payment.spec.ts", TitlePath: []string{"Checkout", "Payment", "declines expired card"}, ParamSignature: "browser=firefox"}
	fp := id.Fingerprint()
	if len(fp) != len(FingerprintVersion)+64 || fp[:4] != "fp1:" {
		t.Errorf("fingerprint shape: %s", fp)
	}
	if ct := id.ClientTestID(); len(ct) != 35 || ct[:3] != "ct_" {
		t.Errorf("clientTestId shape: %s", ct)
	}
	// Meaningless differences do not move the fingerprint.
	a := Signals{Framework: "Playwright ", FilePath: "tests\\auth\\login.spec.TS", TitlePath: []string{"  Auth  ", "Login\tflow"}}
	b := Signals{Framework: "playwright", FilePath: "./tests/auth/login.spec.ts", TitlePath: []string{"Auth", "Login flow"}}
	if a.Fingerprint() != b.Fingerprint() {
		t.Error("normalisation unstable")
	}
	if NormalizeParams(map[string]string{"browser": "firefox", "viewport": "mobile"}) != NormalizeParams(map[string]string{"viewport": "mobile", "browser": "firefox"}) {
		t.Error("parameter order leaked into the signature")
	}
	// Meaningful differences do.
	c := b
	c.ParamSignature = "browser=webkit"
	if c.Fingerprint() == b.Fingerprint() {
		t.Error("parameter ignored")
	}
}

// ---- N35/N38: template key groups loop variants, fingerprint does not ---

func TestTemplateKeyGroupsDynamicTitlesButFingerprintDoesNot(t *testing.T) {
	a := Signals{Framework: "playwright", FilePath: "t.spec.ts", TitlePath: []string{"Discounts", "applies discount for 10"}}
	b := Signals{Framework: "playwright", FilePath: "t.spec.ts", TitlePath: []string{"Discounts", "applies discount for 20"}}
	if a.TemplateKey() != b.TemplateKey() {
		t.Error("loop variants should share a template key")
	}
	if a.Fingerprint() == b.Fingerprint() {
		t.Error("loop variants must NOT share a fingerprint — two real tests, two histories")
	}
	for in, want := range map[string]string{
		"order 8412 succeeds":                               "order <num> succeeds",
		"user a1b2c3d4-e5f6-7890-abcd-ef1234567890 logs in": "user <uuid> logs in",
		"invoice 2026-09-14 emailed to bob@example.com":     "invoice <date> emailed to <email>",
		"commit deadbeefcafe rolled back, total 12.50":      "commit <hex> rolled back, total <num>",
		"declines expired card":                             "declines expired card",
	} {
		if got := NormalizeDynamic(in); got != want {
			t.Errorf("NormalizeDynamic(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- N49: trigrams are pg_trgm's -----------------------------------------

func TestTrigramsMatchPgTrgm(t *testing.T) {
	list := func(s string) []string {
		out := []string{}
		for k := range trigrams(s) {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"a/b", []string{"  a", "  b", " a ", " b "}},
		{"two words", []string{"  t", "  w", " tw", " wo", "ds ", "ord", "rds", "two", "wo ", "wor"}},
		{"Tests/Login.spec", []string{"  l", "  s", "  t", " lo", " sp", " te", "ec ", "est", "gin", "in ", "log", "ogi", "pec", "spe", "sts", "tes", "ts "}},
		{"", []string{}},
		{"///", []string{}},
	} {
		got := list(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q: %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: %v, want %v", c.in, got, c.want)
				break
			}
		}
	}
	if s := TrigramSimilarity("declines expired card", "rejects expired card"); s <= 0.4 || s >= 1 {
		t.Errorf("similarity of a reworded leaf = %.3f; expected a partial score above the 0.4 filter", s)
	}
}

// ---- score rules (§9.3 step 3) -------------------------------------------

func TestScoreComponents(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"tests/a/x.spec.ts", "tests/a/x.spec.ts", 1}, {"tests/a/x.spec.ts", "tests/a/y.spec.ts", 0.6}, {"tests/a/x.spec.ts", "tests/b/x.spec.ts", 0.7},
	} {
		if got := ScoreFile(c.a, c.b); got != c.want {
			t.Errorf("ScoreFile(%s,%s)=%.2f want %.2f", c.a, c.b, got, c.want)
		}
	}
	for _, c := range []struct {
		a, b string
		want float64
	}{
		{"", "", 1}, {"browser=firefox", "browser=firefox", 1}, {"browser=firefox", "browser=webkit", 0.5}, {"browser=firefox", "viewport=mobile", 0}, {"", "browser=webkit", 0},
	} {
		if got := ScoreParam(c.a, c.b); got != c.want {
			t.Errorf("ScoreParam(%q,%q)=%.2f want %.2f", c.a, c.b, got, c.want)
		}
	}
	cfg := DefaultConfig()
	now := timeRef()
	for days, want := range map[float64]float64{0: 1, 7: 1, 90: 0, 200: 0, 48.5: 0.5} {
		if got := cfg.ScoreRecency(now.Add(-time.Duration(days*24)*time.Hour), now); math.Abs(got-want) > 1e-9 {
			t.Errorf("recency at %v days = %.3f want %.3f", days, got, want)
		}
	}
	if cfg.ScoreRecency(time.Time{}, now) != 0 {
		t.Error("never-seen candidate must score 0 recency")
	}
}

// ---- N34: presence penalty ------------------------------------------------

func TestPenaltyBlocksWrongMerge(t *testing.T) {
	cfg := DefaultConfig()
	now := timeRef()
	in := Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "new test"}}
	cand := Candidate{ID: "st_1", Signals: Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "new tests"}}, LastSeenAt: now}
	absent := cfg.Score(in, cand, now).Adjusted
	cand.PresentInRun = true
	present := cfg.Score(in, cand, now).Adjusted
	if present >= absent || present >= cfg.ThresholdAutoMatch {
		t.Errorf("penalty too weak: present=%.4f absent=%.4f", present, absent)
	}
	// Even a perfect copy that is still present in the run stays below review.
	same := Candidate{ID: "st_2", Signals: in, LastSeenAt: now, PresentInRun: true}
	if got := cfg.Score(in, same, now).Adjusted; got >= cfg.ThresholdReview {
		t.Errorf("identical candidate still in the run scored %.4f >= review %.2f", got, cfg.ThresholdReview)
	}
}

func TestPresenceSignalIsLoadBearing(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	suite := baseSuite(300, rng)
	cases := buildRenameCases(suite, 30, rng)
	res := evaluate(DefaultConfig(), suite, cases, 1, nil)
	if res.Precision < 0.98 || res.AutoWrong != 0 {
		t.Errorf("precision %.4f, %d wrong merges — the worst failure mode", res.Precision, res.AutoWrong)
	}
	if res.Recall < 0.80 {
		t.Errorf("recall %.4f < 0.80 — check PresentInRun (N34) before touching thresholds", res.Recall)
	}
	// Disable the penalty: recall must collapse. This is the N34 measurement
	// itself, kept so nobody can weaken the multiplier "for performance".
	weak := DefaultConfig()
	weak.PenaltyCandidatePresent = 1
	if r := evaluate(weak, suite, cases, 1, nil); r.Recall >= res.Recall*0.5 {
		t.Errorf("without the penalty recall should collapse; with=%.4f without=%.4f", res.Recall, r.Recall)
	}
}

// ---- §9.6 numbers at the SPEC denominator ---------------------------------

func TestSpecRenameSetMeetsGate(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	suite := baseSuite(500, rng)
	cases := buildRenameCases(suite, specRenameSetSize, rng)
	res := evaluate(DefaultConfig(), suite, cases, 1, rand.New(rand.NewSource(43)))
	if res.Total != 250 {
		t.Fatalf("denominator = %d, want 250 (N56)", res.Total)
	}
	if res.Precision < 0.98 || res.AutoWrong != 0 {
		t.Errorf("precision %.4f (%d wrong) below the §9.6 floor", res.Precision, res.AutoWrong)
	}
	if res.Recall < 0.80 {
		t.Errorf("recall %.4f below the §9.6 floor", res.Recall)
	}
	t.Logf("§9.6 on synthetic data: precision %.4f recall %.4f (auto %d, review %d, new %d)", res.Precision, res.Recall, res.AutoMatched, res.Review, res.New)
}

// ---- N39/N40: partial visibility is safe but useless ---------------------

func TestPartialVisibilityIsSafeButUseless(t *testing.T) {
	results := map[float64]evalResult{}
	for _, v := range []float64{1, 0.5, 0.25, 0.1} {
		rng := rand.New(rand.NewSource(42))
		suite := baseSuite(500, rng)
		cases := buildRenameCases(suite, specRenameSetSize, rng)
		results[v] = evaluate(DefaultConfig(), suite, cases, v, rand.New(rand.NewSource(43)))
	}
	for v, r := range results {
		if r.AutoWrong != 0 || r.Precision < 0.98 {
			t.Errorf("visibility %.2f: %d wrong merges, precision %.4f — MinGap is no longer protecting us (N40)", v, r.AutoWrong, r.Precision)
		}
	}
	if results[0.5].Recall >= results[1].Recall*0.6 {
		t.Errorf("recall should collapse at half visibility: full=%.4f half=%.4f (N39) — this is why §7.6 resolves in two tiers", results[1].Recall, results[0.5].Recall)
	}
	t.Logf("recall by visibility: 1.0=%.4f 0.5=%.4f 0.1=%.4f", results[1].Recall, results[0.5].Recall, results[0.1].Recall)
}

// TestMinGapRefusesTies: two candidates scoring alike must not auto-match
// even when both clear the threshold.
func TestMinGapRefusesTies(t *testing.T) {
	cfg := DefaultConfig()
	now := timeRef()
	in := Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "rejects expired card"}}
	twins := []Candidate{
		{ID: "st_1", Signals: Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "declines expired card"}}, LastSeenAt: now},
		{ID: "st_2", Signals: Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "declines expired cards"}}, LastSeenAt: now},
	}
	v := cfg.Decide(in, twins, now)
	if v.Decision == DecisionAutoMatch {
		t.Errorf("auto-matched with gap %.3f between %.3f and %.3f", v.Gap, v.Score, v.RunnerUp)
	}
	if v.Decision != DecisionReview {
		t.Errorf("expected review, got %s (score %.3f)", v.Decision, v.Score)
	}
	// Mark the runner-up present: the gap opens and the match fires.
	twins[1].PresentInRun = true
	if v := cfg.Decide(in, twins, now); v.Decision != DecisionAutoMatch || v.MatchedID != "st_1" {
		t.Errorf("with the twin present: %s → %s (gap %.3f)", v.Decision, v.MatchedID, v.Gap)
	}
	if v := cfg.Decide(in, nil, now); v.Decision != DecisionNew {
		t.Error("no candidates must be a new identity")
	}
}

// ---- N45: recency gate is off by default --------------------------------

func TestRecencyGateOffByDefault(t *testing.T) {
	cfg := DefaultConfig()
	now := timeRef()
	in := Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "rejects expired card"}}
	old := Candidate{ID: "st_1", Signals: Signals{Framework: "playwright", FilePath: "tests/a.spec.ts", TitlePath: []string{"S", "declines expired card"}}, LastSeenAt: now.Add(-60 * 24 * time.Hour)}
	if v := cfg.Decide(in, []Candidate{old}, now); v.Decision != DecisionAutoMatch {
		t.Errorf("60-day-old candidate: %s at %.3f — SPEC says recency alone does not block (N45)", v.Decision, v.Score)
	}
	cfg.RecencyGateDays = 30
	if v := cfg.Decide(in, []Candidate{old}, now); v.Decision == DecisionAutoMatch {
		t.Error("gate enabled at 30 days should block the auto-match")
	}
}
