package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

// Identity is the raw signal a reporter sends with every event (SPEC §7.3).
type Identity struct {
	Framework      string
	FilePath       string
	TitlePath      []string
	ParamSignature string
}

// titleSep separates TitlePath elements when computing the fingerprint.
//
// U+0000 (NUL) — NOT a space, and never the empty string.
// A space is ambiguous: ["a b","c"] and ["a","b c"] hash identically.
// The empty string is worse: ["Check","outPayment"] == ["Checkout","Payment"],
// which merges two distinct tests — the failure SPEC §9.6 ranks as more
// damaging than splitting one test in two. See finding N22.
const titleSep = "\x00"

// NormalizeFilePath per SPEC §9.2.
func NormalizeFilePath(p, repoRoot string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if repoRoot != "" {
		root := strings.ReplaceAll(repoRoot, "\\", "/")
		root = strings.TrimSuffix(root, "/") + "/"
		p = strings.TrimPrefix(p, root)
	}
	p = strings.TrimPrefix(p, "./")
	if ext := path.Ext(p); ext != "" {
		p = strings.TrimSuffix(p, ext) + strings.ToLower(ext)
	}
	return p
}

// NormalizeTitle trims and collapses every whitespace run to a single space.
func NormalizeTitle(t string) string {
	return strings.Join(strings.Fields(t), " ")
}

// NormalizeParams renders key=value pairs sorted by key, joined with '&'.
// Empty when the test is not parameterised.
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

// Normalize returns a fully normalised copy of the Identity.
func (id Identity) Normalize(repoRoot string) Identity {
	titles := make([]string, len(id.TitlePath))
	for i, t := range id.TitlePath {
		titles[i] = NormalizeTitle(t)
	}
	return Identity{
		Framework:      id.Framework,
		FilePath:       NormalizeFilePath(id.FilePath, repoRoot),
		TitlePath:      titles,
		ParamSignature: id.ParamSignature,
	}
}

// Fingerprint computes the deterministic fingerprint from SPEC §9.2.
// The "fp1:" prefix versions the algorithm — any change to normalisation
// must bump it.
//
// Never feed test index, line number or timestamps into the fingerprint:
// they churn constantly and would shatter identity for no reason.
func (id Identity) Fingerprint(repoRoot string) string {
	n := id.Normalize(repoRoot)
	h := sha256.New()
	h.Write([]byte(n.Framework))
	h.Write([]byte("\n"))
	h.Write([]byte(n.FilePath))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.Join(n.TitlePath, titleSep)))
	h.Write([]byte("\n"))
	h.Write([]byte(n.ParamSignature))
	return "fp1:" + hex.EncodeToString(h.Sum(nil))
}

// ClientTestID is "ct_" + the first 16 digest bytes, hex encoded (SPEC §9.2).
func (id Identity) ClientTestID(repoRoot string) string {
	fp := id.Fingerprint(repoRoot)
	raw := strings.TrimPrefix(fp, "fp1:")
	if len(raw) >= 32 {
		return "ct_" + raw[:32]
	}
	return "ct_" + raw
}

// Leaf returns the last TitlePath element (the test name), "" when empty.
func (id Identity) Leaf() string {
	if len(id.TitlePath) == 0 {
		return ""
	}
	return NormalizeTitle(id.TitlePath[len(id.TitlePath)-1])
}

// Ancestors returns the enclosing describe blocks (everything but the leaf).
func (id Identity) Ancestors() []string {
	if len(id.TitlePath) <= 1 {
		return nil
	}
	out := make([]string, 0, len(id.TitlePath)-1)
	for _, t := range id.TitlePath[:len(id.TitlePath)-1] {
		out = append(out, NormalizeTitle(t))
	}
	return out
}

// TemplateKey groups identities that share (filePath, titlePath) but differ
// by parameter — SPEC §9.5.
//
// Caveat, see finding N35: Playwright's most common parameterisation idiom
// bakes the parameter into the TITLE, so those variants get different
// TitlePath values and this key does not group them. TemplateKeyNormalized
// is the experimental answer to that.
func (id Identity) TemplateKey(repoRoot string) string {
	n := id.Normalize(repoRoot)
	h := sha256.New()
	h.Write([]byte(n.FilePath))
	h.Write([]byte("\n"))
	h.Write([]byte(strings.Join(n.TitlePath, titleSep)))
	return hex.EncodeToString(h.Sum(nil))[:32]
}
