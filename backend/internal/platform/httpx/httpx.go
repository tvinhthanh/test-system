// Package httpx holds HTTP middleware shared by every handler: request id,
// access logging, and — later — auth, rate limit and the Origin/CSRF guard
// from N18 (§6.9.2). It is platform code and knows no business types.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = iota

// reqInfo travels with the request: its id, and the underlying error behind
// a 5xx so the access line can say why without the handler needing a
// logger of its own (middleware such as authz has none).
type reqInfo struct {
	id    string
	cause error
}

// HeaderRequestID is echoed on every response so a client can quote it in a
// support request and we can find the exact log lines (§6.4 correlation id).
const HeaderRequestID = "X-Request-Id"

// RequestID attaches a correlation id to the request context and response.
// A caller-supplied id is honoured so ids survive a proxy or a retry (§7.1).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = newID()
		}
		w.Header().Set(HeaderRequestID, id)
		ctx := context.WithValue(r.Context(), requestIDKey, &reqInfo{id: id})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom returns the correlation id set by RequestID, or "".
func RequestIDFrom(ctx context.Context) string {
	if info, ok := ctx.Value(requestIDKey).(*reqInfo); ok {
		return info.id
	}
	return ""
}

// Cause records the error behind a failure the request is about to answer
// with a 5xx, for the access log (§6.4: an operator must be able to tell a
// database outage from a bug from one line, by request id). The client
// still gets only the generic message (§11.5).
func Cause(ctx context.Context, err error) {
	if info, ok := ctx.Value(requestIDKey).(*reqInfo); ok && err != nil {
		info.cause = err
	}
}

func causeFrom(ctx context.Context) error {
	if info, ok := ctx.Value(requestIDKey).(*reqInfo); ok {
		return info.cause
	}
	return nil
}

// Logging writes one structured line per request with method, path, status
// and latency — at ERROR with the recorded Cause when the answer is a 5xx,
// so "we are broken" is one grep away, at INFO otherwise. It must run
// inside RequestID so the id is available.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			attrs := []any{
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if rec.status >= 500 {
				if cause := causeFrom(r.Context()); cause != nil {
					attrs = append(attrs, "cause", cause.Error())
				}
				logger.Error("http", attrs...)
				return
			}
			logger.Info("http", attrs...)
		})
	}
}

// Health answers liveness probes. It deliberately checks nothing: a probe that
// depends on Postgres turns a database blip into a fleet restart.
func Health() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
