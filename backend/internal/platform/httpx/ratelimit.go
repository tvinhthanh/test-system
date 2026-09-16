package httpx

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimit is a token bucket per key (§7.4: 60 req/min, burst 120, per
// token). keyFn returns "" to exempt a request. Buckets live in this
// process; with more than one api replica each replica enforces its own
// share, which is fine until §6.9.8 step 2 and must move to Postgres or
// Redis at that point — noted here so it is not forgotten.
func RateLimit(perMinute, burst int, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	l := &limiter{
		rate:    float64(perMinute) / 60.0,
		burst:   float64(burst),
		buckets: map[string]*bucket{},
		now:     time.Now,
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}
			if wait := l.take(key); wait > 0 {
				secs := int(wait.Seconds()) + 1
				w.Header().Set("Retry-After", strconv.Itoa(secs))
				WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests", map[string]any{"retryAfterSeconds": secs})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type bucket struct {
	tokens float64
	last   time.Time
}

type limiter struct {
	rate, burst float64
	mu          sync.Mutex
	buckets     map[string]*bucket
	calls       int
	now         func() time.Time
}

// take consumes one token for key and returns 0, or how long until one is
// available.
func (l *limiter) take(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		l.maybePrune(now)
		return 0
	}
	return time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// maybePrune drops buckets idle long enough to be full again, so a burst
// of one-off tokens does not grow the map forever.
func (l *limiter) maybePrune(now time.Time) {
	l.calls++
	if l.calls%1000 != 0 {
		return
	}
	idle := time.Duration(l.burst/l.rate*float64(time.Second)) + time.Minute
	for k, b := range l.buckets {
		if now.Sub(b.last) > idle {
			delete(l.buckets, k)
		}
	}
}
