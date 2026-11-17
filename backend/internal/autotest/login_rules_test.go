package autotest

import (
	"context"
	"strings"
	"testing"
)

func TestLoginByRules(t *testing.T) {
	cases := []struct {
		name   string
		tree   string
		want   []Step // nil: nothing usable
		second bool
		note   string // a substring of the note when want is nil
	}{
		{
			name: "vietnamese form",
			tree: loginTree, // the fake site's own sign-in page
			want: []Step{
				{Do: "fill", By: "role", Role: "textbox", Name: "Email", Value: "{{username}}"},
				{Do: "fill", By: "role", Role: "textbox", Name: "Mật khẩu", Value: "{{password}}"},
				{Do: "click", By: "role", Role: "button", Name: "Đăng nhập"},
			},
		},
		{
			name: "provider buttons and links are never the submit",
			tree: `- heading "Welcome back" [level=1]
- button "Continue with Google"
- button "Sign in with GitHub"
- textbox "Username"
- textbox "Password"
- button "Show password"
- link "Forgot password?"
- button "Log in"
- button "Create account"`,
			want: []Step{
				{Do: "fill", By: "role", Role: "textbox", Name: "Username", Value: "{{username}}"},
				{Do: "fill", By: "role", Role: "textbox", Name: "Password", Value: "{{password}}"},
				{Do: "click", By: "role", Role: "button", Name: "Log in"},
			},
		},
		{
			name: "no button: press Enter in the password field",
			tree: `- textbox "Email address"
- textbox "Password"`,
			want: []Step{
				{Do: "fill", By: "role", Role: "textbox", Name: "Email address", Value: "{{username}}"},
				{Do: "fill", By: "role", Role: "textbox", Name: "Password", Value: "{{password}}"},
				{Do: "press", By: "role", Role: "textbox", Name: "Password", Value: "Enter"},
			},
		},
		{
			name: "the one other field beside the password is the user name",
			tree: `- textbox "Mã nhân viên"
- textbox "Mật khẩu"
- button "Đăng nhập"`,
			want: []Step{
				{Do: "fill", By: "role", Role: "textbox", Name: "Mã nhân viên", Value: "{{username}}"},
				{Do: "fill", By: "role", Role: "textbox", Name: "Mật khẩu", Value: "{{password}}"},
				{Do: "click", By: "role", Role: "button", Name: "Đăng nhập"},
			},
		},
		{
			name: "password on a second screen",
			tree: `- textbox "Email"
- button "Next"`,
			want: []Step{
				{Do: "fill", By: "role", Role: "textbox", Name: "Email", Value: "{{username}}"},
				{Do: "click", By: "role", Role: "button", Name: "Next"},
			},
			second: true,
		},
		{name: "captcha is left to a person", tree: loginTree + "\n- iframe \"reCAPTCHA\"", note: "captcha"},
		{name: "a one-time code is left to a person", tree: `- textbox "Verification code"
- button "Verify"`, note: "one-time code"},
		{name: "not a sign-in page", tree: chatTree, note: "no user name or email field"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, second, note := loginByRules(c.tree, false)
			if c.want == nil {
				if got != nil || !strings.Contains(note, c.note) {
					t.Fatalf("want nothing with a note about %q, got %+v / %q", c.note, got, note)
				}
				return
			}
			if second != c.second {
				t.Errorf("second screen = %v, want %v", second, c.second)
			}
			if len(got) != len(c.want) {
				t.Fatalf("steps = %+v, want %+v", got, c.want)
			}
			for i := range got {
				g, w := got[i], c.want[i]
				if g.Do != w.Do || g.By != w.By || g.Role != w.Role || g.Name != w.Name || g.Value != w.Value {
					t.Errorf("step %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

// A sign-in that asks for the email first and shows the password after
// "Continue".
type twoScreenSession struct {
	onPassword bool
	ran        [][]Step
	vars       []map[string]string
}

func (s *twoScreenSession) Run(_ context.Context, steps []Step, vars map[string]string) (Outcome, error) {
	s.ran = append(s.ran, steps)
	s.vars = append(s.vars, vars)
	for _, st := range steps {
		if st.Do == "click" && st.Name == "Tiếp tục" {
			s.onPassword = true
		}
	}
	return Outcome{OK: true}, nil
}

func (s *twoScreenSession) Look(context.Context, bool) (View, error) {
	if s.onPassword {
		return View{URL: "https://shop.example/login/password", Tree: `- textbox "Mật khẩu"
- button "Đăng nhập"`}, nil
	}
	return View{URL: "https://shop.example/login", Tree: `- textbox "Email hoặc số điện thoại"
- button "Tiếp tục"`}, nil
}

func (s *twoScreenSession) Close(context.Context) error { return nil }

func TestLoginStepsByRulesReadsTheSecondScreen(t *testing.T) {
	sess := &twoScreenSession{}
	first, _ := sess.Look(context.Background(), false)
	svc := &service{}
	steps, err := svc.loginStepsByRules(context.Background(), sess, Account{Username: "tester@example.com"}, first)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"fill Email hoặc số điện thoại {{username}}", "click Tiếp tục ", "fill Mật khẩu {{password}}", "click Đăng nhập "}
	if len(steps) != len(want) {
		t.Fatalf("steps = %+v", steps)
	}
	for i, s := range steps {
		if got := s.Do + " " + s.Name + " " + s.Value; got != want[i] {
			t.Errorf("step %d = %q, want %q", i, got, want[i])
		}
	}
	// Continuing needed the user name, and only the user name.
	if len(sess.vars) != 1 || sess.vars[0][VarUsername] != "tester@example.com" || sess.vars[0][VarPassword] != "" {
		t.Errorf("first screen ran with %+v", sess.vars)
	}
}

// End to end without any AI key: the account's sign-in steps are found by
// rules, proven by signing in, and kept; no model is asked.
func TestFindLoginWithoutAI(t *testing.T) {
	f := setup(t)
	f.svc.Keys = fakeKeys{ok: false}
	ctx := context.Background()
	if _, err := f.svc.SaveAccount(ctx, f.project, f.env, AccountInput{Key: "default", Username: "tester@example.com", Password: "s3cret", LoginURL: "/login", Actor: "tia"}); err != nil {
		t.Fatal(err)
	}
	a, err := f.svc.FindLogin(ctx, f.project, f.org, f.env, "default")
	if err != nil || a.LoginStatus != LoginFinding {
		t.Fatalf("queue without AI: %+v %v", a, err)
	}
	if _, err := f.svc.LoginWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	acct, _ := f.svc.account(ctx, f.project, f.env, "default")
	if acct.LoginStatus != LoginOK || len(acct.LoginSteps) != 3 || acct.LoginSteps[2].Name != "Đăng nhập" {
		t.Fatalf("sign-in steps by rules: %+v", acct)
	}
	if len(f.model.calls) != 0 {
		t.Errorf("a model was asked %d time(s) without an AI key", len(f.model.calls))
	}

	// A wrong password: the rules find the same steps, the proof fails, and
	// nothing is kept.
	if _, err := f.svc.SaveAccount(ctx, f.project, f.env, AccountInput{Key: "wrong", Username: "tester@example.com", Password: "nope", LoginURL: "/login", Actor: "tia"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.FindLogin(ctx, f.project, f.org, f.env, "wrong"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.LoginWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	bad, _ := f.svc.account(ctx, f.project, f.env, "wrong")
	if bad.LoginStatus != LoginFailed || len(bad.LoginSteps) != 0 || !strings.Contains(bad.LoginError, "did not sign in") {
		t.Fatalf("unproven steps kept: %+v", bad)
	}
}
