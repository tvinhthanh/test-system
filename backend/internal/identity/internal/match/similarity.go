package match

import (
	"strings"
	"unicode"
)

// levenshtein counts edits in runes, not bytes — byte-wise counting is wrong
// for non-ASCII test names.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// LevenshteinSimilarity normalises to [0,1]; 1 means identical. It has no
// length cap: fuzzystrmatch's 255 limit is exactly why step 3 runs here and
// not in SQL (N50).
func LevenshteinSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	maxLen := max(len([]rune(a)), len([]rune(b)))
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(levenshtein(a, b))/float64(maxLen)
}

// JaccardSimilarity over two string sets — the enclosing describe blocks.
func JaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1
	}
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	setA := make(map[string]struct{}, len(a))
	for _, s := range a {
		setA[s] = struct{}{}
	}
	setB := make(map[string]struct{}, len(b))
	for _, s := range b {
		setB[s] = struct{}{}
	}
	inter := 0
	for s := range setA {
		if _, ok := setB[s]; ok {
			inter++
		}
	}
	union := len(setA) + len(setB) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}

// notWordChar mirrors pg_trgm's ISWORDCHR: anything that is not a letter or a
// digit separates words. "/", ".", "-", "_" and spaces all cut here.
func notWordChar(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// trigrams reproduces PostgreSQL's generate_trgm(): lower-case, cut into
// maximal alphanumeric words, pad EACH WORD with two leading and one trailing
// space, then slice 3-grams (LPADDING=2, RPADDING=1).
//
// Splitting into words FIRST is the part that is easy to get wrong (N49).
// Padding the whole string invents trigrams that straddle a separator and
// scores a mean 0.013 below pg_trgm (max 0.054) — and a candidate threshold
// validated against a stricter function admits MORE candidates against the
// real one, never fewer. The identity package compares this against
// similarity() on a live Postgres.
func trigrams(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, word := range strings.FieldsFunc(strings.ToLower(s), notWordChar) {
		r := []rune("  " + word + " ")
		for i := 0; i+3 <= len(r); i++ {
			out[string(r[i:i+3])] = struct{}{}
		}
	}
	return out
}

// TrigramSimilarity is pg_trgm.similarity(): shared trigrams over the size
// of their union. §9.3 uses it for S_file. Two strings with no trigrams at
// all score 0, as in Postgres — not 1 as the prototype had it (found by the
// live parity test in the identity package).
//
// Parity holds only when the database runs a UTF-8 locale: under LC_CTYPE=C
// pg_trgm classifies bytes, so every non-ASCII letter becomes a separator
// and Vietnamese titles fragment. That is a deployment requirement, checked
// by the same test.
func TrigramSimilarity(a, b string) float64 {
	ta, tb := trigrams(a), trigrams(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for g := range ta {
		if _, ok := tb[g]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 1
	}
	return float64(inter) / float64(union)
}
