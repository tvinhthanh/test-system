package httpx

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
)

// Metrics records the §6.4 "infrastructure" row — 5xx rate and p99 latency —
// for every request, labelled by route pattern rather than raw path so a
// project id in the URL does not become its own time series.
//
// It must wrap the *http.ServeMux directly: the mux fills in r.Pattern on the
// request it is handed, and that is the request this middleware reads back
// after next.ServeHTTP returns. Unmatched requests (404) share one label.
func Metrics(reg *metric.Registry) func(http.Handler) http.Handler {
	requests := reg.Counter("http_requests_total", "HTTP requests by route and status.", "method", "route", "status")
	latency := reg.Histogram("http_request_duration_seconds", "HTTP request latency by route.", metric.DefaultLatencyBuckets, "method", "route")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			route := r.Pattern
			if route == "" {
				route = "unmatched"
			}
			requests.Inc(r.Method, route, strconv.Itoa(rec.status))
			latency.Observe(time.Since(start).Seconds(), r.Method, route)
		})
	}
}
