package metric

import (
	"fmt"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestExpositionFormat(t *testing.T) {
	r := New()
	c := r.Counter("ingest_requests_total", "Ingest requests.", "endpoint", "status")
	g := r.Gauge("review_queue_length", "Pairs waiting for a human.")
	h := r.Histogram("resolve_seconds", "Resolver latency.", []float64{0.1, 1}, "tier")
	r.GaugeFunc("build_info", "Always 1.", func() float64 { return 1 })

	c.Inc("runs", "201")
	c.Add(2, "runs", "500")
	c.Inc("events", "200")
	g.Set(42)
	h.Observe(0.05, "inline")
	h.Observe(0.5, "inline")
	h.Observe(7, "inline")

	want := strings.Join([]string{
		"# HELP build_info Always 1.",
		"# TYPE build_info gauge",
		"build_info 1",
		"# HELP ingest_requests_total Ingest requests.",
		"# TYPE ingest_requests_total counter",
		`ingest_requests_total{endpoint="events",status="200"} 1`,
		`ingest_requests_total{endpoint="runs",status="201"} 1`,
		`ingest_requests_total{endpoint="runs",status="500"} 2`,
		"# HELP resolve_seconds Resolver latency.",
		"# TYPE resolve_seconds histogram",
		`resolve_seconds_bucket{tier="inline",le="0.1"} 1`,
		`resolve_seconds_bucket{tier="inline",le="1"} 2`,
		`resolve_seconds_bucket{tier="inline",le="+Inf"} 3`,
		`resolve_seconds_sum{tier="inline"} 7.55`,
		`resolve_seconds_count{tier="inline"} 3`,
		"# HELP review_queue_length Pairs waiting for a human.",
		"# TYPE review_queue_length gauge",
		"review_queue_length 42",
		"",
	}, "\n")
	if got := r.Expose(); got != want {
		t.Errorf("exposition mismatch\n--- got ---\n%s--- want ---\n%s", got, want)
	}

	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Body.String() != want {
		t.Error("handler body differs from Expose()")
	}
}

func TestLabelValueEscaping(t *testing.T) {
	r := New()
	c := r.Counter("errors_total", `Help with "quotes" and \ backslash`, "msg")
	c.Inc("say \"hi\"\nnew\\line")
	got := r.Expose()
	for _, want := range []string{
		`errors_total{msg="say \"hi\"\nnew\\line"} 1`,
		`# HELP errors_total Help with "quotes" and \\ backslash`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestBootTimePanics(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		fn()
	}
	r := New()
	r.Counter("ok_total", "")
	mustPanic("duplicate name", func() { r.Counter("ok_total", "") })
	mustPanic("bad metric name", func() { r.Gauge("has-dash", "") })
	mustPanic("bad label name", func() { r.Gauge("g", "", "0bad") })
	mustPanic("reserved label", func() { r.Gauge("g2", "", "__name__") })
	mustPanic("empty buckets", func() { r.Histogram("h", "", nil) })
	mustPanic("unsorted buckets", func() { r.Histogram("h2", "", []float64{1, 1}) })
	c := r.Counter("labelled_total", "", "a")
	mustPanic("label count", func() { c.Inc() })
	mustPanic("negative counter", func() { c.Add(-1, "x") })
}

func TestConcurrentUpdates(t *testing.T) {
	r := New()
	c := r.Counter("hits_total", "", "k")
	h := r.Histogram("lat", "", DefaultLatencyBuckets, "k")
	const goroutines, per = 16, 1000
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				c.Inc("a")
				h.Observe(0.01, "a")
			}
		}()
	}
	wg.Wait()
	if got := c.Value("a"); got != goroutines*per {
		t.Errorf("counter = %v, want %d", got, goroutines*per)
	}
	if got := h.Count("a"); got != goroutines*per {
		t.Errorf("histogram count = %d, want %d", got, goroutines*per)
	}
}

// TestProcessGaugesArePresentAndSane: the soak (T5) reads these by name;
// RSS and heap must be real numbers on this OS, not the 0 of "unsupported".
func TestProcessGaugesArePresentAndSane(t *testing.T) {
	r := New()
	Process(r)
	text := r.Expose()
	for _, name := range []string{"go_goroutines", "go_memstats_heap_alloc_bytes", "go_memstats_heap_inuse_bytes", "go_memstats_heap_live_bytes", "go_memstats_sys_bytes", "go_gc_cycles_total", "process_resident_memory_bytes"} {
		if !strings.Contains(text, "\n"+name+" ") && !strings.HasPrefix(text, name+" ") {
			t.Errorf("%s missing from exposition:\n%s", name, text)
		}
	}
	val := func(name string) float64 {
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, name+" ") {
				var v float64
				fmt.Sscanf(strings.TrimPrefix(line, name+" "), "%g", &v)
				return v
			}
		}
		return -1
	}
	if v := val("go_goroutines"); v < 1 {
		t.Errorf("go_goroutines = %v", v)
	}
	if heap, sys := val("go_memstats_heap_inuse_bytes"), val("go_memstats_sys_bytes"); heap <= 0 || sys < heap {
		t.Errorf("heap_inuse %v, sys %v", heap, sys)
	}
	if rss := val("process_resident_memory_bytes"); rss < 1<<20 {
		t.Errorf("process_resident_memory_bytes = %v: expected a real RSS on %s", rss, runtime.GOOS)
	}
}
