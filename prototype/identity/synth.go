package identity

import (
	"fmt"
	"math/rand"
	"strings"
)

// MutationKind covers the five refactorings SPEC §9.6 item 2 requires simulating.
type MutationKind string

const (
	MutateLeafTitle MutationKind = "rename leaf title"
	MutateDescribe  MutationKind = "rename describe"
	MutateMoveFile  MutationKind = "move file"
	MutateBoth      MutationKind = "rename title and move file"
	MutateAddParam  MutationKind = "add parameter"
)

var AllMutations = []MutationKind{MutateLeafTitle, MutateDescribe, MutateMoveFile, MutateBoth, MutateAddParam}

// SpecRenameSetSize is the rename-set size SPEC §9.6 item 2 fixes: 50 tests, each
// put through all five mutations, so 250 trials.
//
// Any harness reporting recall as a §9.6 number must use it. A harness that
// quietly picks another count reports against a different denominator: at 40 the
// full-visibility recall is 0.9950 (199/200), at 50 it is 0.9920 (248/250). That
// is where the 0.9950 removed from SPEC in N51 was still being generated, long
// after every copy of it had been swept out of the document — see N56.
const SpecRenameSetSize = 50

// RenameCase is one trial: the identity before and after the change, plus ground truth.
type RenameCase struct {
	StableID string
	Before   Identity
	After    Identity
	Kind     MutationKind
}

// BaseSuite generates a Playwright-shaped corpus to test against.
// Used until partner data arrives; week 3 replaces it with real JSON exports (§9.6 item 1).
func BaseSuite(n int, rng *rand.Rand) []Identity {
	areas := []struct {
		dir     string
		file    string
		suite   string
		feature string
	}{
		{"tests/checkout", "payment.spec.ts", "Checkout", "Payment"},
		{"tests/checkout", "cart.spec.ts", "Checkout", "Cart"},
		{"tests/auth", "login.spec.ts", "Auth", "Login"},
		{"tests/auth", "signup.spec.ts", "Auth", "Signup"},
		{"tests/profile", "settings.spec.ts", "Profile", "Settings"},
		{"tests/search", "filters.spec.ts", "Search", "Filters"},
		{"tests/admin", "users.spec.ts", "Admin", "User management"},
	}
	verbs := []string{
		"declines expired card", "accepts valid card", "shows error on empty field",
		"redirects after success", "keeps state on reload", "handles slow network",
		"rejects duplicate email", "applies discount code", "paginates results",
		"sorts by relevance", "clears filters", "blocks unauthorized access",
	}
	browsers := []string{"", "", "browser=chromium", "browser=firefox"}

	out := make([]Identity, 0, n)
	for i := 0; i < n; i++ {
		a := areas[rng.Intn(len(areas))]
		v := verbs[rng.Intn(len(verbs))]
		out = append(out, Identity{
			Framework:      "playwright",
			FilePath:       a.dir + "/" + a.file,
			TitlePath:      []string{a.suite, a.feature, fmt.Sprintf("%s #%d", v, i)},
			ParamSignature: browsers[rng.Intn(len(browsers))],
		})
	}
	return out
}

// Mutate applies one refactoring to an identity, imitating what a developer would do.
func Mutate(id Identity, kind MutationKind, rng *rand.Rand) Identity {
	out := Identity{
		Framework:      id.Framework,
		FilePath:       id.FilePath,
		TitlePath:      append([]string{}, id.TitlePath...),
		ParamSignature: id.ParamSignature,
	}
	last := len(out.TitlePath) - 1

	switch kind {
	case MutateLeafTitle:
		out.TitlePath[last] = rewordLeaf(out.TitlePath[last], rng)

	case MutateDescribe:
		if last >= 1 {
			out.TitlePath[last-1] = out.TitlePath[last-1] + " flow"
		} else {
			out.TitlePath = append([]string{"Suite"}, out.TitlePath...)
		}

	case MutateMoveFile:
		out.FilePath = moveFile(out.FilePath, rng)

	case MutateBoth:
		out.TitlePath[last] = rewordLeaf(out.TitlePath[last], rng)
		out.FilePath = moveFile(out.FilePath, rng)

	case MutateAddParam:
		if out.ParamSignature == "" {
			out.ParamSignature = "browser=webkit"
		} else {
			out.ParamSignature = out.ParamSignature + "&viewport=mobile"
		}
	}
	return out
}

// rewordLeaf imitates a developer rewording a test name while keeping its meaning.
func rewordLeaf(s string, rng *rand.Rand) string {
	repl := []struct{ from, to string }{
		{"declines", "rejects"},
		{"accepts", "allows"},
		{"shows", "displays"},
		{"redirects", "navigates"},
		{"keeps", "preserves"},
		{"handles", "tolerates"},
		{"rejects", "refuses"},
		{"applies", "uses"},
		{"blocks", "prevents"},
	}
	for _, r := range repl {
		if strings.Contains(s, r.from) {
			return strings.Replace(s, r.from, r.to, 1)
		}
	}
	// No verb matched: append a suffix, as a developer adding context would.
	return s + " correctly"
}

// moveFile imitates a developer moving or renaming a spec file.
func moveFile(p string, rng *rand.Rand) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "specs/" + p
	}
	dir, file := p[:i], p[i+1:]
	if rng.Intn(2) == 0 {
		return dir + "/e2e/" + file // into a subdirectory: same filename
	}
	return dir + "/" + strings.TrimSuffix(file, ".spec.ts") + ".e2e.spec.ts" // renamed: same directory
}

// BuildRenameCases picks count tests from the suite and emits one case per mutation kind.
func BuildRenameCases(suite []Identity, count int, rng *rand.Rand) []RenameCase {
	if count > len(suite) {
		count = len(suite)
	}
	idx := rng.Perm(len(suite))[:count]
	cases := make([]RenameCase, 0, count*len(AllMutations))
	for _, i := range idx {
		for _, k := range AllMutations {
			cases = append(cases, RenameCase{
				StableID: fmt.Sprintf("st_%04d", i),
				Before:   suite[i],
				After:    Mutate(suite[i], k, rng),
				Kind:     k,
			})
		}
	}
	return cases
}
