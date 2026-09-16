package httpx

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
)

func TestRequestIDHonoursCallerAndEchoes(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set(HeaderRequestID, "client-supplied")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != "client-supplied" || rec.Header().Get(HeaderRequestID) != "client-supplied" {
		t.Errorf("caller id not honoured: ctx=%q header=%q", seen, rec.Header().Get(HeaderRequestID))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Header().Get(HeaderRequestID); len(got) != 16 || got != seen {
		t.Errorf("generated id = %q (ctx %q), want a 16-hex id that reached the handler", got, seen)
	}
	if RequestIDFrom(context.Background()) != "" {
		t.Error("empty context should yield empty id")
	}
}

func TestLoggingRecordsStatusAndID(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	h := RequestID(Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})))
	req := httptest.NewRequest("POST", "/v1/runs", nil)
	req.Header.Set(HeaderRequestID, "abc")
	h.ServeHTTP(httptest.NewRecorder(), req)
	for _, want := range []string{"request_id=abc", "method=POST", "path=/v1/runs", "status=418"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log line missing %q: %s", want, buf.String())
		}
	}
}

func TestMetricsLabelsByPatternNotPath(t *testing.T) {
	reg := metric.New()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/projects/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := Metrics(reg)(mux)

	for _, p := range []string{"/v1/projects/p1", "/v1/projects/p2", "/v1/projects/p3"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", p, nil))
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/nope", nil))

	got := reg.Expose()
	for _, want := range []string{
		`http_requests_total{method="GET",route="GET /v1/projects/{id}",status="204"} 3`,
		`http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`http_request_duration_seconds_count{method="GET",route="GET /v1/projects/{id}"} 3`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "p1") {
		t.Error("raw path leaked into a label — cardinality would grow per project")
	}
}

func TestHealthIsJSONAndDependsOnNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	Health().ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 || rec.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body.String())
	}
}
