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
