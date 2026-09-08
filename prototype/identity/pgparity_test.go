package identity

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// maxLevenshteinStrlen is MAX_LEVENSHTEIN_STRLEN from PostgreSQL's
// fuzzystrmatch. Past it, levenshtein() raises an error rather than degrading —
// the statement fails, and with it every candidate being scored in it.
//
// UNVERIFIED HERE: the documentation says 255 characters, the error message in
// the source says bytes, and the check is applied to the byte length. Which one
// governs decides whether a Vietnamese title gets 255 characters of headroom or
// roughly 85. This cannot be settled without a real Postgres — see N50.
const maxLevenshteinStrlen = 255

// TestTitleLengthHeadroom measures how close realistic Playwright titles come to
// that cap. The point is not that the cap is hit today; it is that the margin is
// thin enough to depend on an unverified detail.
func TestTitleLengthHeadroom(t *testing.T) {
	// A data-driven title — the idiom N35 established is common, not rare:
	//   for (const row of fixtures) test(`rejects order ${JSON.stringify(row)}`)
	row := `{"sku":"ABC-12345","qty":3,"currency":"USD","coupon":"SUMMER-2026","address":{"line1":"221B Baker Street","city":"London","postcode":"NW1 6XE","country":"GB"},"payment":"visa-4242"}`
	dataDriven := "rejects order " + row

	// The same test named in Vietnamese: every accented rune costs 2-3 bytes, so
	// the byte count runs far ahead of what the author sees on screen.
	viet := "kiểm tra đơn hàng bị từ chối khi mã giảm giá đã hết hạn và địa chỉ giao hàng nằm ngoài vùng phục vụ của đơn vị vận chuyển được chọn"

	// Two fixture rows in one title. Nothing exotic — a matrix test over two
	// payloads produces this shape on its own.
	twoRows := dataDriven + " with fallback " + row

	for _, c := range []struct{ name, title string }{
		{"typical short title", "declines expired card"},
		{"long Vietnamese title", viet},
		{"data-driven JSON title", dataDriven},
		{"two fixture rows", twoRows},
	} {
		runes, bytes := utf8.RuneCountInString(c.title), len(c.title)
		note := ""
		if bytes > maxLevenshteinStrlen {
			note = fmt.Sprintf("  over 255 BYTES by %d", bytes-maxLevenshteinStrlen)
		}
		if runes > maxLevenshteinStrlen {
			note += "  over 255 CHARACTERS too"
		}
		t.Logf("%-24s %4d runes %4d bytes%s", c.name, runes, bytes, note)
	}

	// Whatever the unit turns out to be, the prototype has no cap: it scores
	// every pair. Any limit that exists in production exists only there.
	other := strings.Replace(twoRows, "SUMMER-2026", "WINTER-2026", 1)
	if s := LevenshteinSimilarity(twoRows, other); s <= 0 || s >= 1 {
		t.Fatalf("expected a partial score for the long pair, got %.4f", s)
	} else {
		t.Logf("prototype scores the %d-byte pair at %.4f, with no length check at all",
			len(twoRows), s)
	}
}

// TestScoringCostInGo answers the question the cap raises: does §9.3 step 3 need
// to run in Postgres at all? Step 3 scores at most 50 candidates (§9.3), and
// §9.7 expects fewer than 10 unknown fingerprints in a normal run.
func TestScoringCostInGo(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	suite := BaseSuite(50, rng)
	cfg := DefaultConfig()
	now := time.Now()
	cands := make([]Candidate, len(suite))
	for i, id := range suite {
		cands[i] = Candidate{StableID: fmt.Sprintf("st_%d", i), Identity: id,
			LastSeenAt: now.Add(-24 * time.Hour)}
	}
	in := Mutate(suite[0], MutateLeafTitle, rng)

	const rounds = 200
	start := time.Now()
	for i := 0; i < rounds; i++ {
		for _, c := range cands {
			cfg.Score(in, c, now, "")
		}
	}
	per := time.Since(start) / rounds

	t.Logf("scoring the full 50-candidate cap in Go: %v", per.Round(time.Microsecond))
	t.Logf("  per candidate            : %.1f µs", float64(per.Nanoseconds())/50/1000)
	t.Logf("  10 unknown fps in a run  : %v", (per * 10).Round(time.Microsecond))

	// §7.6 budgets p50 < 1s for resolving a whole run. If scoring every unknown
	// fingerprint costs a low single-digit millisecond total, the choice of
	// language for step 3 is not a performance decision.
	if budget := 50 * time.Millisecond; per*10 > budget {
		t.Errorf("scoring 10 unknown fingerprints took %v, over the %v this claim rests on", per*10, budget)
	}
}

// BenchmarkScoreCandidateSet is the same measurement under `go test -bench`.
func BenchmarkScoreCandidateSet(b *testing.B) {
	rng := rand.New(rand.NewSource(7))
	suite := BaseSuite(50, rng)
	cfg := DefaultConfig()
	now := time.Now()
	cands := make([]Candidate, len(suite))
	for i, id := range suite {
		cands[i] = Candidate{StableID: fmt.Sprintf("st_%d", i), Identity: id,
			LastSeenAt: now.Add(-24 * time.Hour)}
	}
	in := Mutate(suite[0], MutateLeafTitle, rng)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cands {
			cfg.Score(in, c, now, "")
		}
	}
}
