package identity

import (
	"math"
	"sort"
	"testing"
)

func trigramList(s string) []string {
	m := trigrams(s)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTrigramsMatchPgTrgm locks the generator to PostgreSQL's generate_trgm().
//
// §9.3 tunes a 0.4 candidate threshold against these numbers, but the threshold
// is enforced in production by pg_trgm inside Postgres. If the two functions
// drift apart, every number this prototype produces is measuring something the
// database will never compute.
func TestTrigramsMatchPgTrgm(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// A separator must cut words, not merge them. Each word is padded
		// independently: "  a " yields "  a" and " a ".
		{"a/b", []string{"  a", "  b", " a ", " b "}},

		// The canonical pg_trgm example. Note "  w" is present (words is padded
		// on its own) and no trigram straddles the space.
		{"two words", []string{"  t", "  w", " tw", " wo", "ds ", "ord", "rds", "two", "wo ", "wor"}},

		// Case folding, and a path where "/" and "." are both separators.
		{"Tests/Login.spec", []string{
			"  t", " te", "tes", "est", "sts", "ts ", // tests
			"  l", " lo", "log", "ogi", "gin", "in ", // login
			"  s", " sp", "spe", "pec", "ec ", // spec
		}},

		{"", nil},
		{"///", nil},
	}
	for _, c := range cases {
		got := trigramList(c.in)
		want := append([]string(nil), c.want...)
		sort.Strings(want)
		if !equalStrings(got, want) {
			t.Errorf("trigrams(%q)\n  got  %v\n  want %v", c.in, got, want)
		}
	}
}

// TestNoStraddlingTrigrams is the specific bug this generator used to have.
// Padding the whole string produced "o w" — a trigram spanning the space — and
// omitted "  w", so a candidate whose title differed only by an extra word was
// scored lower than Postgres would score it.
func TestNoStraddlingTrigrams(t *testing.T) {
	got := trigrams("two words")
	if _, bad := got["o w"]; bad {
		t.Error(`"o w" straddles the separator: the string is being padded as a whole, not word by word`)
	}
	if _, ok := got["  w"]; !ok {
		t.Error(`"  w" missing: the second word is not getting its own leading padding`)
	}
}

// TestSimilarityIsPgTrgmRatio checks the whole ratio against a set computed by
// hand, so a change in either the generator or the ratio is caught.
//
//	"renders the dashboard" -> renders(8) + the(4) + dashboard(10) = 22 trigrams
//	"renders dashboard"     -> renders(8) +          dashboard(10) = 18 trigrams
//
// The second set is a strict subset of the first, so intersection = 18 and
// union = 22 + 18 - 18 = 22.
func TestSimilarityIsPgTrgmRatio(t *testing.T) {
	got := TrigramSimilarity("renders the dashboard", "renders dashboard")
	want := 18.0 / 22.0
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("similarity = %.6f, want %.6f (18/22)", got, want)
	}
	if n := len(trigrams("renders the dashboard")); n != 22 {
		t.Errorf("trigram count = %d, want 22", n)
	}
}

func TestSimilarityBoundsAndSymmetry(t *testing.T) {
	pairs := [][2]string{
		{"", ""},
		{"", "abc"},
		{"logs in", "signs in"},
		{"tests/a/x.spec.ts", "tests/b/y.spec.ts"},
		{"đăng nhập thành công", "đăng nhập thất bại"},
	}
	for _, p := range pairs {
		ab, ba := TrigramSimilarity(p[0], p[1]), TrigramSimilarity(p[1], p[0])
		if ab != ba {
			t.Errorf("asymmetric for %q/%q: %.6f vs %.6f", p[0], p[1], ab, ba)
		}
		if ab < 0 || ab > 1 {
			t.Errorf("out of range for %q/%q: %.6f", p[0], p[1], ab)
		}
	}
	if s := TrigramSimilarity("checkout flow", "checkout flow"); s != 1 {
		t.Errorf("identical strings scored %.6f, want 1", s)
	}
}
