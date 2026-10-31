package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOnlyAllowedHostsNoRedirectsBoundedJSON(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"leaked":true}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"sub":"1"}`))
		case "/form":
			_ = r.ParseForm()
			_, _ = w.Write([]byte(`{"code":"` + r.Form.Get("code") + `"}`))
		case "/redirect":
			http.Redirect(w, r, other.URL, http.StatusFound)
		case "/huge":
			_, _ = w.Write([]byte(`{"x":"` + strings.Repeat("a", maxBody) + `"}`))
		case "/html":
			_, _ = w.Write([]byte(`<html>`))
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	c, err := New(time.Second, srv.URL+"/any/base")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var ok struct{ Sub string }
	if err := c.GetJSON(ctx, srv.URL+"/ok", "tok", &ok); err != nil || ok.Sub != "1" {
		t.Errorf("allowed get: %v %+v", err, ok)
	}
	var form struct{ Code string }
	if err := c.PostForm(ctx, srv.URL+"/form", url.Values{"code": {"abc"}}, &form); err != nil || form.Code != "abc" {
		t.Errorf("post form: %v %+v", err, form)
	}
	var v map[string]any
	if err := c.GetJSON(ctx, other.URL+"/", "tok", &v); !errors.Is(err, ErrHost) {
		t.Errorf("host not allowed: %v", err)
	}
	for _, path := range []string{"/redirect", "/huge", "/html", "/500"} {
		if err := c.GetJSON(ctx, srv.URL+path, "tok", &v); !errors.Is(err, ErrAnswer) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if _, err := New(time.Second, "ftp://x"); err == nil {
		t.Error("non-http base accepted")
	}
}

// Send hands back what came — a 4xx with its body, a 3xx unfollowed, a
// stream read as it arrives — but still only from an allowed host and never
// past the answer cap.
func TestSendReturnsTheAnswerAsItCame(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			b := make([]byte, 64)
			n, _ := r.Body.Read(b)
			w.Header().Set("X-Seen", r.Header.Get("Authorization"))
			_, _ = w.Write(b[:n])
		case "/denied":
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"bad key"}}`))
		case "/redirect":
			http.Redirect(w, r, other.URL, http.StatusFound)
		case "/huge":
			_, _ = w.Write([]byte(strings.Repeat("a", 100)))
		}
	}))
	defer srv.Close()
	c, err := New(time.Second, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.WithMaxBody(10)
	ctx := context.Background()
	read := func(resp *http.Response) string {
		defer resp.Body.Close()
		var b strings.Builder
		buf := make([]byte, 8)
		for {
			n, err := resp.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				return b.String()
			}
		}
	}
	resp, err := c.Send(ctx, http.MethodPost, srv.URL+"/echo", http.Header{"Authorization": {"Bearer k"}}, []byte("hi"))
	if err != nil || resp.Header.Get("X-Seen") != "Bearer k" || read(resp) != "hi" {
		t.Errorf("echo: %v", err)
	}
	if resp, err := c.Send(ctx, http.MethodGet, srv.URL+"/denied", nil, nil); err != nil || resp.StatusCode != 401 {
		t.Errorf("a 4xx is an answer, not an error: %v", err)
	} else if got := read(resp); got != `{"error":{` {
		t.Errorf("body past the cap: %q", got)
	}
	if resp, err := c.Send(ctx, http.MethodGet, srv.URL+"/redirect", nil, nil); err != nil || resp.StatusCode != http.StatusFound {
		t.Errorf("redirect followed: %v", err)
	} else {
		read(resp)
	}
	if resp, err := c.Send(ctx, http.MethodGet, srv.URL+"/huge", nil, nil); err != nil || len(read(resp)) != 10 {
		t.Errorf("huge: %v", err)
	}
	if _, err := c.Send(ctx, http.MethodGet, other.URL+"/", nil, nil); !errors.Is(err, ErrHost) {
		t.Errorf("host not allowed: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Send(cancelled, http.MethodGet, srv.URL+"/echo", nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled call says so: %v", err)
	}
}
