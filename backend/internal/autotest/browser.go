package autotest

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/outbound"
)

// httpBrowser is the checker service at URLCHECK_URL (QĐ-14): one host
// fixed by configuration; the address to open is data in the body and is
// opened only by the checker, behind its guard.
type httpBrowser struct {
	base   string
	client *outbound.Client
}

// NewHTTPBrowser talks to the checker's /sessions. A call waits up to
// two minutes: a step waits at most ten seconds for its element and thirty
// for a page, and a sign-in is a few of those.
func NewHTTPBrowser(base string) (Browser, error) {
	c, err := outbound.New(2*time.Minute, base)
	if err != nil {
		return nil, err
	}
	return &httpBrowser{base: strings.TrimSuffix(base, "/"), client: c.WithMaxBody(8 << 20)}, nil
}

type wireView struct {
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Tree       string   `json:"tree"`
	Screenshot string   `json:"screenshot"`
	Console    []string `json:"console"`
}

func (v wireView) view() View {
	shot, _ := base64.StdEncoding.DecodeString(v.Screenshot)
	return View{URL: v.URL, Title: v.Title, Tree: v.Tree, Screenshot: jpegOrNil(shot)}
}

func (h *httpBrowser) Open(ctx context.Context, base string, login *Login) (Session, LoginResult, error) {
	body := map[string]any{"base": base}
	if login != nil {
		body["login"] = login
	}
	var out struct {
		ID       string `json:"id"`
		LoggedIn *bool  `json:"loggedIn"`
		Error    string `json:"error"`
	}
	if err := h.client.PostJSON(ctx, h.base+"/sessions", body, &out); err != nil {
		return nil, LoginResult{}, err
	}
	if out.ID == "" {
		return nil, LoginResult{}, fmt.Errorf("checker: no session id")
	}
	s := &httpSession{b: h, id: out.ID}
	res := LoginResult{LoggedIn: login == nil || (out.LoggedIn != nil && *out.LoggedIn), Error: cut(out.Error, 500)}
	return s, res, nil
}

type httpSession struct {
	b  *httpBrowser
	id string
}

func (s *httpSession) path(p string) string { return s.b.base + "/sessions/" + s.id + p }

func (s *httpSession) Run(ctx context.Context, steps []Step, vars map[string]string) (Outcome, error) {
	var out struct {
		OK      bool `json:"ok"`
		Results []struct {
			Status string `json:"status"`
			Kind   string `json:"kind"`
			Error  string `json:"error"`
			Ms     int    `json:"ms"`
		} `json:"results"`
		wireView
	}
	if vars == nil {
		vars = map[string]string{}
	}
	if err := s.b.client.PostJSON(ctx, s.path("/steps"), map[string]any{"steps": steps, "vars": vars, "shot": "failure"}, &out); err != nil {
		return Outcome{}, err
	}
	o := Outcome{OK: out.OK, URL: out.URL, Console: out.Console}
	for _, r := range out.Results {
		o.Results = append(o.Results, StepOutcome{Status: r.Status, Kind: r.Kind, Error: cut(r.Error, 500), Ms: r.Ms})
	}
	if !out.OK {
		v := out.view()
		o.View = &v
	}
	return o, nil
}

func (s *httpSession) Look(ctx context.Context, shot bool) (View, error) {
	var out wireView
	if err := s.b.client.PostJSON(ctx, s.path("/look"), map[string]any{"shot": shot}, &out); err != nil {
		return View{}, err
	}
	return out.view(), nil
}

func (s *httpSession) Close(ctx context.Context) error {
	return s.b.client.Delete(ctx, s.path(""), nil)
}

// jpegOrNil keeps only what is a JPEG: the bytes are served back as
// image/jpeg, so nothing else may be stored under that name.
func jpegOrNil(b []byte) []byte {
	if len(b) < 3 || b[0] != 0xFF || b[1] != 0xD8 || b[2] != 0xFF || len(b) > 3<<20 {
		return nil
	}
	return b
}
