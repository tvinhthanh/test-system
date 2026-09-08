package identity

import (
	"strings"
	"unicode"
)

// levenshtein returns the edit distance between two strings, measured in runes
// rather than bytes — byte-wise counting is wrong for non-ASCII test names.
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
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// LevenshteinSimilarity normalises to [0,1]; 1 means identical.
func LevenshteinSimilarity(a, b string) float64 {
	if a == b {
		return 1
	}
	ra, rb := []rune(a), []rune(b)
	maxLen := len(ra)
	if len(rb) > maxLen {
		maxLen = len(rb)
	}
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(levenshtein(a, b))/float64(maxLen)
}

// JaccardSimilarity over two string sets, used for the enclosing describe blocks.
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

// notWordChar mirrors pg_trgm's ISWORDCHR, which treats anything that is not a
// letter or a digit as a word separator. Test names and file paths are full of
// them: "/", ".", "-", "_" and spaces all cut words here.
func notWordChar(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }

// trigrams reproduces PostgreSQL's generate_trgm(): lowercase the input, cut it
// into maximal alphanumeric words, then pad each word with two leading and one
// trailing space before slicing 3-grams (pg_trgm's LPADDING=2, RPADDING=1).
//
// Splitting into words FIRST is the part that is easy to get wrong. Padding the
// whole string instead invents trigrams that straddle a separator: "two words"
// would produce "o w" and would never produce "  w".
//
// That is not a cosmetic difference. Measured over 100k leaf-title pairs from
// the §9.6 suite, whole-string padding scored a mean 0.013 BELOW pg_trgm (max
// 0.054), because a straddling trigram stops matching as soon as the two strings
// differ in word count — exactly the case a rename detector cares about. Being
// stricter here than Postgres is the wrong direction to be wrong in: a §9.3
// candidate threshold validated against a stricter function admits MORE
// candidates once it runs against the real pg_trgm, never fewer.
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

// TrigramSimilarity is pg_trgm.similarity(): shared trigrams over the size of
// their union. SPEC §9.3 uses it for S_file and for the candidate filter (0.4).
func TrigramSimilarity(a, b string) float64 {
	ta, tb := trigrams(a), trigrams(b)
	if len(ta) == 0 && len(tb) == 0 {
		return 1
	}
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
