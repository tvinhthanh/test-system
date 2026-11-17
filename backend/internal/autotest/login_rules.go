package autotest

import (
	"context"
	"regexp"
	"strings"
)

// loginStepsByRules finds the sign-in steps on the page sess shows, reading a
// second screen when the password comes after "continue". Typing the user
// name there needs the account's value, so this session gets it — the same
// value the proof session gets; it never leaves the checker.
func (s *service) loginStepsByRules(ctx context.Context, sess Session, a Account, view View) ([]Step, error) {
	steps, second, note := loginByRules(view.Tree, false)
	if steps == nil {
		return nil, tell("no sign-in steps found on %s: %s", view.URL, note)
	}
	if !second {
		return steps, nil
	}
	o, err := sess.Run(ctx, steps, map[string]string{VarUsername: a.Username})
	if err != nil {
		return nil, err
	}
	if !o.OK {
		return nil, tell("continuing past the user name did not work on %s: %s", view.URL, o.Results[len(o.Results)-1].Error)
	}
	next, err := sess.Look(ctx, false)
	if err != nil {
		return nil, err
	}
	rest, _, note := loginByRules(next.Tree, true)
	if rest == nil {
		return nil, tell("no sign-in steps found on %s: %s", next.URL, note)
	}
	return append(steps, rest...), nil
}

// ---- Finding sign-in steps without AI ----------------------------------
//
// With no AI key, the sign-in page's accessibility tree (Playwright's
// ariaSnapshot: one "- role \"name\"" per line) is read by fixed rules: the
// user-name or email field, the password field, the button that signs in.
// Buttons for another identity provider, "forgot password" and "sign up" are
// never chosen. A page that shows the password only on a second screen gets
// its first half (fill the user name, continue) and is read again. Pages
// that need a captcha or a code from a mail are left to a person, as the
// model is told to. Whatever is found is proven exactly like the model's
// answer: a session must sign in with it before it is kept.

var (
	treeLine = regexp.MustCompile(`^\s*-\s+(textbox|button|searchbox|combobox)(?:\s+"((?:[^"\\]|\\.)*)")?`)

	passwordName = regexp.MustCompile(`(?i)pass(word|wort|e)?|mật khẩu|mat khau|\bpwd\b|contraseña|mot de passe|senha`)
	userName     = regexp.MustCompile(`(?i)e-?mail|user|login|account|phone|mobile|tên đăng nhập|tài khoản|số điện thoại|benutzer|usuario|identifiant`)
	submitName   = regexp.MustCompile(`(?i)sign ?in|log ?in|log ?on|continue|next|submit|đăng nhập|tiếp tục|tiếp theo|anmelden|weiter|connexion|se connecter|iniciar sesión|entrar`)
	notSubmit    = regexp.MustCompile(`(?i)google|github|microsoft|apple|facebook|linkedin|gitlab|okta|\bsso\b|single sign|passkey|magic link|sign ?up|register|create|forgot|reset|quên|đăng ký|tạo tài khoản|show|hide|hiện|ẩn`)
	needsPerson  = regexp.MustCompile(`(?i)captcha|i'?m not a robot|tôi không phải là người máy|one-?time (code|password)|verification code|mã xác (thực|minh)|\bOTP\b|authenticator`)
)

type treeElement struct{ role, name string }

func readTree(tree string) []treeElement {
	var out []treeElement
	for _, line := range strings.Split(tree, "\n") {
		m := treeLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, treeElement{role: m[1], name: strings.ReplaceAll(m[2], `\"`, `"`)})
	}
	return out
}

// field is a fill step for an element, by its accessible name when it has
// one, else by CSS for the kind of input it must be.
func field(e treeElement, css, value, say string) Step {
	if e.name != "" {
		return Step{Do: "fill", By: "role", Role: "textbox", Name: e.name, Value: value, Say: say}
	}
	return Step{Do: "fill", By: "css", Name: css, Value: value, Say: say}
}

// loginByRules reads one screen of a sign-in page. It returns the steps for
// that screen; second is true when they end on "continue" and the password
// comes on the next screen. steps is nil when nothing usable was found, and
// note says why.
func loginByRules(tree string, wantPassword bool) (steps []Step, second bool, note string) {
	if needsPerson.MatchString(tree) {
		return nil, false, "the sign-in page asks for a captcha or a one-time code: write the sign-in steps on the account"
	}
	els := readTree(tree)
	var user, pass *treeElement
	var others []treeElement
	var submit *treeElement
	for i := range els {
		e := &els[i]
		switch {
		case (e.role == "textbox" || e.role == "searchbox" || e.role == "combobox") && passwordName.MatchString(e.name):
			if pass == nil {
				pass = e
			}
		case (e.role == "textbox" || e.role == "combobox") && userName.MatchString(e.name):
			if user == nil {
				user = e
			}
		case e.role == "textbox":
			others = append(others, *e)
		case e.role == "button" && submitName.MatchString(e.name) && !notSubmit.MatchString(e.name):
			if submit == nil {
				submit = e
			}
		}
	}
	// One unnamed or oddly named field beside a password field is the user
	// name. Without a password field it is just some field on some page.
	if user == nil && pass != nil && len(others) == 1 {
		user = &others[0]
	}
	click := func() Step {
		return Step{Do: "click", By: "role", Role: "button", Name: submit.name, Say: "Sign in"}
	}

	if wantPassword {
		// The second screen: only the password and the button are new.
		if pass == nil {
			return nil, false, "after continuing, no password field appeared: write the sign-in steps on the account"
		}
		steps = append(steps, field(*pass, `input[type="password"]`, "{{"+VarPassword+"}}", "Type the password"))
		if submit != nil {
			steps = append(steps, click())
		} else {
			steps = append(steps, Step{Do: "press", By: "role", Role: "textbox", Name: pass.name, Value: "Enter", Say: "Press Enter to sign in"})
		}
		return steps, false, ""
	}

	if user == nil {
		return nil, false, "no user name or email field on the sign-in page: check the account's sign-in address, or write the steps"
	}
	steps = append(steps, field(*user, `input[type="email"]`, "{{"+VarUsername+"}}", "Type the user name"))
	if pass == nil {
		// Password on a second screen, behind "continue".
		if submit == nil {
			return nil, false, "there is a user name field but no password field and no button to continue: write the sign-in steps on the account"
		}
		return append(steps, click()), true, ""
	}
	steps = append(steps, field(*pass, `input[type="password"]`, "{{"+VarPassword+"}}", "Type the password"))
	if submit != nil {
		steps = append(steps, click())
	} else if pass.name != "" {
		steps = append(steps, Step{Do: "press", By: "role", Role: "textbox", Name: pass.name, Value: "Enter", Say: "Press Enter to sign in"})
	} else {
		return nil, false, "no button that signs in and the password field has no name: write the sign-in steps on the account"
	}
	return steps, false, ""
}
