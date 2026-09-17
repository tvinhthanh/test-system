package metric

import (
	"runtime"
	runtimemetrics "runtime/metrics"
)

// Process registers the process-level gauges every binary exposes: the
// numbers a soak test (§14 T5: "không rò bộ nhớ") and an on-call reader
// look at first. Names follow the Prometheus Go client so dashboards and
// alert rules written for it apply unchanged.
//
//	go_goroutines                    — a leak here is a goroutine per request that never returns
//	go_memstats_heap_alloc_bytes     — live + not-yet-collected heap objects
//	go_memstats_heap_inuse_bytes     — heap spans in use (what the GC actually holds on to)
//	go_memstats_sys_bytes            — everything the Go runtime obtained from the OS
//	go_memstats_heap_live_bytes      — live heap at the last GC: the leak signal, free of GC sawtooth
//	go_gc_cycles_total               — completed GC cycles
//	process_resident_memory_bytes    — RSS as the OS sees it (Linux, Windows; 0 elsewhere)
//
// Heap numbers come from runtime/metrics, which does not stop the world;
// a scrape must never be the thing that makes the process hiccup.
func Process(r *Registry) {
	r.GaugeFunc("go_goroutines", "Number of goroutines.", func() float64 { return float64(runtime.NumGoroutine()) })
	names := map[string]string{
		"go_memstats_heap_alloc_bytes": "/memory/classes/heap/objects:bytes",
		"go_memstats_heap_inuse_bytes": "/memory/classes/heap/objects:bytes+/memory/classes/heap/unused:bytes",
		"go_memstats_sys_bytes":        "/memory/classes/total:bytes",
		// Live heap as of the last GC — the number that must stay flat under
		// steady load. heap_inuse and RSS sawtooth with GC pacing (target =
		// 2× live) and mislead a leak hunt; this one does not.
		"go_memstats_heap_live_bytes": "/gc/heap/live:bytes",
	}
	for metricName, sample := range names {
		sample := sample
		r.GaugeFunc(metricName, "Go runtime memory ("+sample+").", func() float64 { return sampleSum(sample) })
	}
	r.GaugeFunc("go_gc_cycles_total", "Completed GC cycles.", func() float64 { return sampleSum("/gc/cycles/total:gc-cycles") })
	r.GaugeFunc("process_resident_memory_bytes", "Resident set size as reported by the OS; 0 where unsupported.", func() float64 { return float64(residentBytes()) })
}

// sampleSum reads one or more runtime/metrics samples joined by '+' and
// returns their sum; an unknown name counts as 0 rather than panicking,
// because a Go release may rename a metric and /metrics must still serve.
func sampleSum(spec string) float64 {
	var names []string
	for start := 0; start <= len(spec); {
		end := start
		for end < len(spec) && spec[end] != '+' {
			end++
		}
		names = append(names, spec[start:end])
		start = end + 1
	}
	samples := make([]runtimemetrics.Sample, len(names))
	for i, n := range names {
		samples[i].Name = n
	}
	runtimemetrics.Read(samples)
	var total float64
	for _, s := range samples {
		switch s.Value.Kind() {
		case runtimemetrics.KindUint64:
			total += float64(s.Value.Uint64())
		case runtimemetrics.KindFloat64:
			total += s.Value.Float64()
		}
	}
	return total
}
