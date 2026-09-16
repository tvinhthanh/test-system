package match

import "regexp"

// N35/N38: Playwright's most common parameterisation idiom bakes the
// parameter into the test TITLE — a loop over amounts producing
// "applies discount for 10", "applies discount for 20". Folding dynamic runs
// to placeholders lets TemplateKey group those variants. Known trade-off:
// "handles 404" and "handles 500" also group under "handles <num>" — verify
// on partner data before relying on it.
var dynamicPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Most specific shapes first, bare integers last.
	{regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`), "<uuid>"},
	{regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2})?)?\b`), "<date>"},
	{regexp.MustCompile(`\b[\w.+-]+@[\w-]+\.[\w.]+\b`), "<email>"},
	{regexp.MustCompile(`(?i)\b(?:0x)?[0-9a-f]{8,}\b`), "<hex>"},
	{regexp.MustCompile(`\b\d+\.\d+\b`), "<num>"},
	{regexp.MustCompile(`\b\d+\b`), "<num>"},
}

// NormalizeDynamic replaces dynamic-looking runs in a title with placeholders.
func NormalizeDynamic(s string) string {
	for _, p := range dynamicPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}
