package match

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// Test-only port of prototype/identity/{synth,eval,visibility}.go: the
// synthetic Playwright-shaped corpus and the §9.6 measurement, so the locking
// tests below run the same pipeline on the same numbers the prototype
// reported. SpecRenameSetSize is the §9.6 item-2 denominator (N56): 50 tests
// × 5 mutations = 250 trials.

const specRenameSetSize = 50

type mutation string

const (
	mutateLeafTitle mutation = "rename leaf title"
	mutateDescribe  mutation = "rename describe"
	mutateMoveFile  mutation = "move file"
	mutateBoth      mutation = "rename title and move file"
	mutateAddParam  mutation = "add parameter"
)

var allMutations = []mutation{mutateLeafTitle, mutateDescribe, mutateMoveFile, mutateBoth, mutateAddParam}

type renameCase struct {
	ID     string
	Before Signals
	After  Signals
	Kind   mutation
}

func baseSuite(n int, rng *rand.Rand) []Signals {
	areas := []struct{ dir, file, suite, feature string }{
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
	out := make([]Signals, 0, n)
	for i := 0; i < n; i++ {
		a := areas[rng.Intn(len(areas))]
		v := verbs[rng.Intn(len(verbs))]
		out = append(out, Signals{
			Framework: "playwright", FilePath: a.dir + "/" + a.file,
			TitlePath:      []string{a.suite, a.feature, fmt.Sprintf("%s #%d", v, i)},
			ParamSignature: browsers[rng.Intn(len(browsers))],
		})
	}
	return out
}

func mutate(s Signals, kind mutation, rng *rand.Rand) Signals {
	out := Signals{Framework: s.Framework, FilePath: s.FilePath, TitlePath: append([]string{}, s.TitlePath...), ParamSignature: s.ParamSignature}
	last := len(out.TitlePath) - 1
	switch kind {
	case mutateLeafTitle:
		out.TitlePath[last] = rewordLeaf(out.TitlePath[last])
	case mutateDescribe:
		if last >= 1 {
			out.TitlePath[last-1] += " flow"
		} else {
			out.TitlePath = append([]string{"Suite"}, out.TitlePath...)
		}
	case mutateMoveFile:
		out.FilePath = moveFile(out.FilePath, rng)
	case mutateBoth:
		out.TitlePath[last] = rewordLeaf(out.TitlePath[last])
		out.FilePath = moveFile(out.FilePath, rng)
	case mutateAddParam:
		if out.ParamSignature == "" {
			out.ParamSignature = "browser=webkit"
		} else {
			out.ParamSignature += "&viewport=mobile"
		}
	}
	return out
}

func rewordLeaf(s string) string {
	for _, r := range []struct{ from, to string }{
		{"declines", "rejects"}, {"accepts", "allows"}, {"shows", "displays"}, {"redirects", "navigates"},
		{"keeps", "preserves"}, {"handles", "tolerates"}, {"rejects", "refuses"}, {"applies", "uses"}, {"blocks", "prevents"},
	} {
		if strings.Contains(s, r.from) {
			return strings.Replace(s, r.from, r.to, 1)
		}
	}
	return s + " correctly"
}

func moveFile(p string, rng *rand.Rand) string {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "specs/" + p
	}
	dir, file := p[:i], p[i+1:]
	if rng.Intn(2) == 0 {
		return dir + "/e2e/" + file
	}
	return dir + "/" + strings.TrimSuffix(file, ".spec.ts") + ".e2e.spec.ts"
}

func buildRenameCases(suite []Signals, count int, rng *rand.Rand) []renameCase {
	if count > len(suite) {
		count = len(suite)
	}
	idx := rng.Perm(len(suite))[:count]
	cases := make([]renameCase, 0, count*len(allMutations))
	for _, i := range idx {
		for _, k := range allMutations {
			cases = append(cases, renameCase{ID: fmt.Sprintf("st_%04d", i), Before: suite[i], After: mutate(suite[i], k, rng), Kind: k})
		}
	}
	return cases
}

type evalResult struct {
	Total, AutoMatched, AutoCorrect, AutoWrong, Review, New int
	Precision, Recall                                       float64
}

func timeRef() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) }

// evaluate runs the resolver over every rename case. visible < 1 models a
// partial view of the run (shard, --grep, early batch): a candidate can only
// be marked present if it was observed, and anything not yet reported looks
// absent — the N39 ambiguity. visible = 1 is resolution deferred to
// run.finish. present == nil means the prototype's Evaluate model (everything
// but the renamed test is present).
func evaluate(cfg Config, suite []Signals, cases []renameCase, visible float64, rng *rand.Rand) evalResult {
	now := timeRef()
	aliases := map[string]string{}
	cands := make([]Candidate, 0, len(suite))
	for i, s := range suite {
		id := fmt.Sprintf("st_%04d", i)
		aliases[s.Fingerprint()] = id
		cands = append(cands, Candidate{ID: id, Signals: s, LastSeenAt: now.Add(-24 * time.Hour)})
	}
	idx := map[string]int{}
	for i, c := range cands {
		idx[c.ID] = i
	}
	res := evalResult{Total: len(cases)}
	for _, c := range cases {
		for i := range cands {
			cands[i].PresentInRun = rng == nil || rng.Float64() < visible
		}
		if i, ok := idx[c.ID]; ok {
			cands[i].PresentInRun = false // genuinely renamed, genuinely gone
		}
		fp := c.After.Fingerprint()
		var decision Decision
		var matched string
		if id, ok := aliases[fp]; ok {
			decision, matched = DecisionAutoMatch, id // exact: fingerprint unchanged by this mutation
		} else {
			v := cfg.Decide(c.After, cfg.RoughFilter(c.After, cands), now)
			decision, matched = v.Decision, v.MatchedID
		}
		switch decision {
		case DecisionAutoMatch:
			res.AutoMatched++
			if matched == c.ID {
				res.AutoCorrect++
			} else {
				res.AutoWrong++
			}
		case DecisionReview:
			res.Review++
		case DecisionNew:
			res.New++
		}
	}
	if res.AutoMatched > 0 {
		res.Precision = float64(res.AutoCorrect) / float64(res.AutoMatched)
	}
	if res.Total > 0 {
		res.Recall = float64(res.AutoCorrect) / float64(res.Total)
	}
	return res
}
