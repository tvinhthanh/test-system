package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitBucket(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	l := &limiter{rate: 60.0 / 60.0, burst: 3, buckets: map[string]*bucket{}, now: func() time.Time { return now }}

	for i := 0; i < 3; i++ {
		if w := l.take("a"); w != 0 {
			t.Fatalf("request %d within burst waited %v", i, w)
		}
	}
	if w := l.take("a"); w <= 0 || w > time.Second {
		t.Errorf("4th request: wait = %v, want (0, 1s]", w)
	}
	if w := l.take("b"); w != 0 {
		t.Errorf("other key is independent: %v", w)
	}
	now = now.Add(2 * time.Second) // 2 tokens refilled at 1/s
	if w := l.take("a"); w != 0 {
		t.Errorf("after refill: %v", w)
	}
	now = now.Add(time.Hour)
	for i := 0; i < 3; i++ {
		if w := l.take("a"); w != 0 {
			t.Errorf("refill caps at burst but never below it: %v", w)
		}
	}
	if w := l.take("a"); w == 0 {
		t.Error("refill must cap at burst")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	h := RateLimit(60, 2, func(r *http.Request) string { return r.Header.Get("X-Key") })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	codes := []int{}
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/", nil)
		req.Header.Set("X-Key", "tok")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes = append(codes, rec.Code)
		if rec.Code == 429 && rec.Header().Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
	}
	if codes[0] != 204 || codes[1] != 204 || codes[2] != 429 {
		t.Errorf("codes = %v", codes)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil)) // no key → exempt
	if rec.Code != 204 {
		t.Errorf("exempt request: %d", rec.Code)
	}
}
