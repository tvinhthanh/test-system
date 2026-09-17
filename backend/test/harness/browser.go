package harness

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
)

// Browser is a signed-in dashboard user: cookie jar, CSRF token and the
// Origin header the §11.2 checks expect, walking frontend/API_CONTRACT.md
// the way the Vue app does.
type Browser struct {
	h      *Harness
	client *http.Client
	csrf   string
}

// Origin is the dashboard origin the harness binaries allow (config default).
const Origin = "http://localhost:5173"

// Login bootstraps a session (GET /v1/session for the CSRF token, then POST)
// and fails the test if the credentials are refused.
func (h *Harness) Login(email, password string) *Browser {
	h.T.Helper()
	jar, _ := cookiejar.New(nil)
	b := &Browser{h: h, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	boot := b.Get("/v1/session")
	if boot.Err != nil || boot.Code != 200 {
		h.T.Fatalf("session bootstrap: %v %d", boot.Err, boot.Code)
	}
	b.csrf, _ = boot.Body["csrfToken"].(string)
	r := b.Post("/v1/session", map[string]any{"email": email, "password": password})
	if r.Err != nil || r.Code != 200 {
		h.T.Fatalf("login %s: %v %d %v", email, r.Err, r.Code, r.Body)
	}
	b.csrf = r.Str("csrfToken") // rotated on login (§11.2)
	return b
}

// Get is a dashboard read.
func (b *Browser) Get(path string) Resp {
	req, _ := http.NewRequest("GET", "http://"+b.h.APIAddr+path, nil)
	req.Header.Set("Accept", "application/json")
	return b.do(req)
}

// Post is a dashboard write with the CSRF header and Origin set.
func (b *Browser) Post(path string, body any) Resp {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(body)
	req, _ := http.NewRequest("POST", "http://"+b.h.APIAddr+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", Origin)
	if b.csrf != "" {
		req.Header.Set(authz.CSRFHeader, b.csrf)
	}
	return b.do(req)
}

func (b *Browser) do(req *http.Request) Resp {
	start := time.Now()
	res, err := b.client.Do(req)
	if err != nil {
		return Resp{Err: err, Elapsed: time.Since(start)}
	}
	defer res.Body.Close()
	out := Resp{Code: res.StatusCode, Elapsed: time.Since(start)}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err == nil {
		out.Body = body
	}
	return out
}

// Items returns a list response's items.
func (r Resp) Items() []map[string]any {
	raw, _ := r.Body["items"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// Str reads a string field of the body ("" when absent).
func (r Resp) Str(key string) string {
	s, _ := r.Body[key].(string)
	return s
}

// Connections counts the database backends the harness's api and worker
// hold right now — the number a connection leak makes climb.
func (h *Harness) Connections() int {
	h.T.Helper()
	return h.Count(`SELECT count(*) FROM pg_stat_activity WHERE application_name = $1`, h.appName)
}

// Schema is the search_path schema this harness's world lives in.
func (h *Harness) Schema() string { return strings.TrimPrefix(h.appName, "atp-") }
