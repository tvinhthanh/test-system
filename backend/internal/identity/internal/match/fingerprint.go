// Package match is the pure core of SPEC §9: fingerprinting (§9.2), the
// similarity functions and score of §9.3 step 3, and the threshold decision of
// step 4. It knows nothing about Postgres or runs; the identity package feeds
// it candidates and acts on its decisions.
//
// It is a port of prototype/identity with the invariants that prototype paid
// for kept as tests (§6.9.10): the U+0000 separator (N22), the presence
// penalty (N34), MinGap under partial visibility (N39/N40), and pg_trgm parity
// (N49). The prototype itself stays untouched as the §9.6 evidence.
package match

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

// Signals is the raw identity a reporter sends with every event (§7.3).
type Signals struct {
	Framework      string
	FilePath       string
	TitlePath      []string
	ParamSignature string
}

// FingerprintVersion prefixes every fingerprint. Any change to normalisation
// bumps it; the server keeps matching older prefixes through the raw signals
// (§9.5).
const FingerprintVersion = "fp1:"

// titleSep separates TitlePath elements inside the hash.
//
// U+0000 (NUL) — NOT a space, and never the empty string. A space is
// ambiguous: ["a b","c"] and ["a","b c"] hash identically. The empty string
// is worse: ["Check","outPayment"] == ["Checkout","Payment"], which merges two
// distinct tests — the failure §9.6 ranks as more damaging than splitting one
// test in two (N22).
const titleSep = "\x00"

// NormalizeFilePath per §9.2: forward slashes, no "./", lower-case extension.
// Repo-root stripping is the reporter's job; the server only sees the result.
func NormalizeFilePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	if ext := path.Ext(p); ext != "" {
		p = strings.TrimSuffix(p, ext) + strings.ToLower(ext)
	}
	return p
}

// NormalizeTitle trims and collapses every whitespace run to a single space.
func NormalizeTitle(t string) string { return strings.Join(strings.Fields(t), " ") }

// NormalizeParams renders key=value pairs sorted by key, joined with '&'.
func NormalizeParams(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	return strings.Join(parts, "&")
}

// Normalize returns a fully normalised copy.
func (s Signals) Normalize() Signals {
	titles := make([]string, len(s.TitlePath))
	for i, t := range s.TitlePath {
		titles[i] = NormalizeTitle(t)
	}
	return Signals{
		Framework:      strings.ToLower(strings.TrimSpace(s.Framework)),
		FilePath:       NormalizeFilePath(s.FilePath),
		TitlePath:      titles,
		ParamSignature: strings.TrimSpace(s.ParamSignature),
	}
}

// Fingerprint is the deterministic hash of §9.2. Never feed test index, line
// number or timestamps into it: they churn constantly and would shatter
// identity for no reason.
func (s Signals) Fingerprint() string {
	n := s.Normalize()
	h := sha256.New()
	h.Write([]byte(n.Framework))
	h.Write([]byte("\n"))
	h.Write([]byte(n.FilePath))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.Join(n.TitlePath, titleSep)))
	h.Write([]byte("\n"))
	h.Write([]byte(n.ParamSignature))
	return FingerprintVersion + hex.EncodeToString(h.Sum(nil))
}

// ClientTestID is "ct_" + the first 16 digest bytes, hex (§9.2) — what a
// reporter that implements §9.2 exactly would send.
func (s Signals) ClientTestID() string {
	raw := strings.TrimPrefix(s.Fingerprint(), FingerprintVersion)
	return "ct_" + raw[:32]
}

// Leaf returns the normalised test name (last TitlePath element), "" when empty.
func (s Signals) Leaf() string {
	if len(s.TitlePath) == 0 {
		return ""
	}
	return NormalizeTitle(s.TitlePath[len(s.TitlePath)-1])
}

// Ancestors returns the normalised enclosing describe blocks.
func (s Signals) Ancestors() []string {
	if len(s.TitlePath) <= 1 {
		return nil
	}
	out := make([]string, 0, len(s.TitlePath)-1)
	for _, t := range s.TitlePath[:len(s.TitlePath)-1] {
		out = append(out, NormalizeTitle(t))
	}
	return out
}

// TemplateKey groups identities that share (filePath, titlePath) but differ
// by parameter (§9.5), with dynamic title runs folded to placeholders (N35/
// N38). It is for dashboard grouping only — NEVER an identity: two loop
// variants are two real tests with separate flaky histories.
func (s Signals) TemplateKey() string {
	n := s.Normalize()
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
