package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Experimental answer to finding N35.
//
// Playwright's most common parameterisation idiom bakes the parameter into the
// test TITLE rather than into projectName:
//
//	for (const amount of [10, 20]) test(`applies discount for ${amount}`, ...)
//
// Those variants get different TitlePath values, so §9.5's TemplateKey does not
// group them. Normalising dynamic runs to placeholders fixes the grouping.
//
// CRITICAL BOUNDARY: this normalisation applies to the TEMPLATE KEY ONLY, never
// to the fingerprint. The two variants are genuinely different tests and must
// keep different identities and separate flaky histories. Folding them together
// at fingerprint level would be exactly the wrong-merge failure §9.6 ranks worst.

var dynamicPatterns = []struct {
	name string
	re   *regexp.Regexp
	repl string
}{
	// Order matters: match the most specific shapes before bare integers.
	{"uuid", regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<uuid>"},
	{"iso-date", regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2})?)?\b`), "<date>"},
	{"email", regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.]+\b`), "<email>"},
	{"hex", regexp.MustCompile(`(?i)\b(?:0x)?[0-9a-f]{8,}\b`), "<hex>"},
	{"decimal", regexp.MustCompile(`\b\d+\.\d+\b`), "<num>"},
	{"integer", regexp.MustCompile(`\b\d+\b`), "<num>"},
}

// NormalizeDynamic replaces dynamic-looking runs in a title with placeholders.
func NormalizeDynamic(s string) string {
	for _, p := range dynamicPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// TemplateKeyNormalized groups tests whose titles differ only by an embedded
// parameter. Use it for dashboard grouping and drill-down (§9.5), never as an
// identity.
func (id Identity) TemplateKeyNormalized(repoRoot string) string {
	n := id.Normalize(repoRoot)
	parts := make([]string, len(n.TitlePath))
	for i, t := range n.TitlePath {
		parts[i] = NormalizeDynamic(t)
	}
	h := sha256.New()
	h.Write([]byte(n.FilePath))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.Join(parts, titleSep)))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// TitleIsDynamic reports whether normalisation changed the leaf title, i.e.
// whether this test looks loop- or data-generated. Useful for surfacing "this
// suite uses title-embedded parameters" in the Review Queue.
func (id Identity) TitleIsDynamic() bool {
	leaf := id.Leaf()
	return NormalizeDynamic(leaf) != leaf
}
