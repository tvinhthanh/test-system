package autotest

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Step is one action of a runnable case — the same shape in the API, the
// database, a model's answer and the checker (checker/src/steps.mjs, which
// checks it again before running it). Steps are data: the checker
// interprets each with Playwright's locators; nothing here is code.
type Step struct {
	Do       string `json:"do"`       // one of Actions
	By       string `json:"by"`       // one of Locators, or "" for a step without an element
	Role     string `json:"role"`     // the ARIA role when By is "role"
	Name     string `json:"name"`     // accessible name, label, placeholder, text, test id or CSS
	Value    string `json:"value"`    // what to type, pick or press; the address; the text expected; ms to wait
	Say      string `json:"say"`      // the step in words, for people
	Expected string `json:"expected"` // what should be seen, for people
}

var (
	// Actions a step may take.
	Actions = []string{"goto", "click", "fill", "select", "check", "uncheck", "press", "hover", "wait",
		"expect_visible", "expect_hidden", "expect_text", "expect_url", "expect_value", "expect_title"}
	// Locators: how a step names its element.
	Locators = []string{"role", "label", "placeholder", "text", "testid", "css"}
	// Roles getByRole is asked for.
	Roles = []string{"alert", "alertdialog", "article", "banner", "button", "cell", "checkbox", "columnheader", "combobox",
		"complementary", "contentinfo", "dialog", "form", "grid", "gridcell", "heading", "img", "link", "list",
		"listbox", "listitem", "log", "main", "menu", "menubar", "menuitem", "menuitemcheckbox", "menuitemradio",
		"navigation", "option", "paragraph", "progressbar", "radio", "radiogroup", "region", "row", "rowheader",
		"search", "searchbox", "separator", "slider", "spinbutton", "status", "switch", "tab", "table", "tablist",
		"tabpanel", "textbox", "toolbar", "tooltip", "tree", "treeitem"}
)

var (
	needsTarget    = map[string]bool{"click": true, "fill": true, "select": true, "check": true, "uncheck": true, "hover": true, "expect_visible": true, "expect_hidden": true, "expect_value": true}
	optionalTarget = map[string]bool{"press": true, "expect_text": true}
	needsValue     = map[string]bool{"goto": true, "select": true, "press": true, "wait": true, "expect_text": true, "expect_url": true, "expect_title": true}
	// Steps a model may put in place of one that lost its element: actions
	// only — a repair never changes what a case checks, and never navigates.
	healActions = map[string]bool{"click": true, "fill": true, "select": true, "check": true, "uncheck": true, "press": true, "hover": true, "wait": true}
)

const (
	// MaxSteps per case (and per sign-in).
	MaxSteps = 40
	// MaxWaitMs is the longest wait step.
	MaxWaitMs = 10_000
)

// Built-in variables: the account's sign-in values (sent to the checker
// only, never to a model) and two fresh values per case.
const (
	VarUsername  = "username"
	VarPassword  = "password"
	VarRandom    = "random"
	VarTimestamp = "timestamp"
)

var (
	varRe     = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]{0,39})\s*\}\}`)
	inputName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,39}$`)
)

// Validate reports the first problem with s, or nil.
func (s Step) Validate() error {
	if !slices.Contains(Actions, s.Do) {
		return fmt.Errorf("unknown action %q", s.Do)
	}
	if s.By != "" && !slices.Contains(Locators, s.By) {
		return fmt.Errorf("unknown locator %q", s.By)
	}
	switch {
	case needsTarget[s.Do] && s.By == "":
		return fmt.Errorf("%s needs an element", s.Do)
	case !needsTarget[s.Do] && !optionalTarget[s.Do] && s.By != "":
		return fmt.Errorf("%s takes no element", s.Do)
	case s.By == "role" && !slices.Contains(Roles, s.Role):
		return fmt.Errorf("unknown role %q", s.Role)
	case s.By != "" && s.By != "role" && strings.TrimSpace(s.Name) == "":
		return fmt.Errorf("%s needs a value", s.By)
	case needsValue[s.Do] && s.Value == "":
		return fmt.Errorf("%s needs a value", s.Do)
	}
	if s.Do == "wait" {
		if ms, err := strconv.Atoi(s.Value); err != nil || ms < 1 || ms > MaxWaitMs {
			return fmt.Errorf("wait is 1 to %d ms", MaxWaitMs)
		}
	}
	if len(s.Name) > 2000 || len(s.Value) > 2000 {
		return fmt.Errorf("name and value are at most 2000 bytes")
	}
	if len(s.Say) > 500 || len(s.Expected) > 1000 || !utf8.ValidString(s.Say+s.Expected+s.Name+s.Value) {
		return fmt.Errorf("say is at most 500 bytes and expected 1000, in UTF-8")
	}
	return nil
}

// clean trims the fields a person types and drops what does not apply, so
// equal steps compare equal.
func (s Step) clean() Step {
	s.Do, s.By, s.Role = strings.TrimSpace(s.Do), strings.TrimSpace(s.By), strings.TrimSpace(s.Role)
	s.Say, s.Expected = strings.TrimSpace(s.Say), strings.TrimSpace(s.Expected)
	if s.By != "role" {
		s.Role = ""
	}
	if s.By == "" {
		s.Name = ""
	}
	return s
}

// Vars returns the variable names text uses, in order of first use.
func Vars(text string) []string {
	var out []string
	for _, m := range varRe.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

// Substitute replaces {{name}} from vars; an unknown name is an error.
func Substitute(text string, vars map[string]string) (string, error) {
	var missing string
	out := varRe.ReplaceAllStringFunc(text, func(m string) string {
		name := varRe.FindStringSubmatch(m)[1]
		v, ok := vars[name]
		if !ok && missing == "" {
			missing = name
		}
		return v
	})
	if missing != "" {
		return "", fmt.Errorf("unknown variable {{%s}}", missing)
	}
	return out, nil
}

// checkVars reports a variable that steps or target use and nothing defines.
func checkVars(target string, steps []Step, inputs map[string]string, account bool) error {
	known := func(name string) bool {
		if _, ok := inputs[name]; ok {
			return true
		}
		switch name {
		case VarRandom, VarTimestamp:
			return true
		case VarUsername, VarPassword:
			return account
		}
		return false
	}
	texts := []string{target}
	for _, s := range steps {
		texts = append(texts, s.Name, s.Value)
	}
	for _, t := range texts {
		for _, name := range Vars(t) {
			if !known(name) {
				if name == VarUsername || name == VarPassword {
					return fmt.Errorf("{{%s}} needs a test account on the case", name)
				}
				return fmt.Errorf("{{%s}} is not one of the case's inputs", name)
			}
		}
	}
	return nil
}

// validInputs checks an inputs map: names like variables, values short.
func validInputs(inputs map[string]string) error {
	if len(inputs) > 30 {
		return fmt.Errorf("at most 30 inputs")
	}
	for k, v := range inputs {
		if !inputName.MatchString(k) {
			return fmt.Errorf("input %q: a name is letters, digits and _ (not starting with a digit)", k)
		}
		switch k {
		case VarUsername, VarPassword, VarRandom, VarTimestamp:
			return fmt.Errorf("input %q is built in", k)
		}
		if len(v) > 2000 || !utf8.ValidString(v) {
			return fmt.Errorf("input %q is over 2000 bytes", k)
		}
	}
	return nil
}

// Describe is a step in words when no one wrote Say.
func Describe(s Step) string {
	el := element(s)
	switch s.Do {
	case "goto":
		return "Open " + s.Value
	case "click":
		return "Click " + el
	case "fill":
		if s.Value == "" {
			return "Clear " + el
		}
		return fmt.Sprintf("Type “%s” into %s", s.Value, el)
	case "select":
		return fmt.Sprintf("Choose “%s” in %s", s.Value, el)
	case "check":
		return "Tick " + el
	case "uncheck":
		return "Untick " + el
	case "press":
		if s.By == "" {
			return "Press " + s.Value
		}
		return fmt.Sprintf("Press %s in %s", s.Value, el)
	case "hover":
		return "Point at " + el
	case "wait":
		return "Wait " + s.Value + " ms"
	case "expect_visible":
		return "See " + el
	case "expect_hidden":
		return "No longer see " + el
	case "expect_text":
		if s.By == "" {
			return fmt.Sprintf("See the text “%s” on the page", s.Value)
		}
		return fmt.Sprintf("See “%s” in %s", s.Value, el)
	case "expect_url":
		return fmt.Sprintf("The address contains “%s”", s.Value)
	case "expect_value":
		return fmt.Sprintf("%s holds “%s”", capital(el), s.Value)
	case "expect_title":
		return fmt.Sprintf("The page title contains “%s”", s.Value)
	}
	return s.Do
}

func element(s Step) string {
	switch s.By {
	case "role":
		if s.Name == "" {
			return "a " + s.Role
		}
		return fmt.Sprintf("%s “%s”", s.Role, s.Name)
	case "label":
		return fmt.Sprintf("the field “%s”", s.Name)
	case "placeholder":
		return fmt.Sprintf("the field “%s”", s.Name)
	case "text":
		return fmt.Sprintf("“%s”", s.Name)
	case "testid":
		return fmt.Sprintf("the element [%s]", s.Name)
	case "css":
		return fmt.Sprintf("the element %s", s.Name)
	}
	return "the page"
}

func capital(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + s[n:]
}

// resolveTarget is the address a case opens: target (variables already
// substituted) relative to the environment's address, and on the same site.
func resolveTarget(base, target string) (string, error) {
	b, err := url.Parse(base)
	if err != nil || b.Hostname() == "" {
		return "", fmt.Errorf("the environment has no address")
	}
	t, err := b.Parse(strings.TrimSpace(target))
	if err != nil || (t.Scheme != "http" && t.Scheme != "https") || t.User != nil {
		return "", fmt.Errorf("the case's address is not an http(s) address")
	}
	if siteOf(t.Hostname()) != siteOf(b.Hostname()) {
		return "", fmt.Errorf("%s is not on the environment's site (%s)", t.Host, b.Host)
	}
	t.Fragment = ""
	return t.String(), nil
}

// siteOf mirrors checker/src/steps.mjs: a host and its siblings under the
// same registrable name are one site.
func siteOf(host string) string {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if strings.Contains(h, ":") || regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`).MatchString(h) {
		return h
	}
	labels := strings.Split(h, ".")
	if len(labels) <= 2 {
		return h
	}
	keep := 2
	second, last := labels[len(labels)-2], labels[len(labels)-1]
	if len(last) == 2 && slices.Contains([]string{"co", "com", "net", "org", "gov", "edu", "ac", "or", "ne", "go", "biz", "info"}, second) {
		keep = 3
	}
	return strings.Join(labels[len(labels)-keep:], ".")
}
