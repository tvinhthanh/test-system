// Package metric is the process-wide metrics registry behind every number in
// SPEC §6.4 (mandatory from P1: ingest error rate, resolver latency, artifact
// failures, identity churn, ...). It exposes them in the Prometheus text
// format so any scraper can read them; nothing here knows what a run or a
// project is (§6.9.3).
//
// Like platform/log it wraps no third-party library. Three metric kinds with
// labels and a text exposition are a few hundred lines; a client library
// brings a dependency tree that dwarfs the rest of backend/ and a global
// default registry that every module would quietly reach for. Modules take a
// *Registry explicitly instead, so a test can hand them a fresh one.
//
// Registration is a boot-time act and panics on misuse (duplicate name,
// bad name, wrong label count): those are programming errors that should
// fail the process before it serves a request, not be logged and forgotten.
package metric

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// DefaultLatencyBuckets are the Prometheus defaults, in seconds. They cover
// the §7.6 inline resolver budget (p50 < 1 s), the §6.4 p99 > 5 s alert and
// the §14.2 dashboard p95 < 500 ms line.
var DefaultLatencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

var (
	metricNameRe = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNameRe  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// Registry owns a set of named metrics. Zero value is not usable; use New.
type Registry struct {
	mu      sync.Mutex
	metrics map[string]collector
}

// collector is what the registry stores: anything that can write itself in
// exposition format.
type collector interface {
	write(sb *strings.Builder)
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{metrics: map[string]collector{}}
}

func (r *Registry) register(name string, c collector) {
	if !metricNameRe.MatchString(name) {
		panic(fmt.Sprintf("metric: invalid metric name %q", name))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.metrics[name]; dup {
		panic(fmt.Sprintf("metric: %q registered twice", name))
	}
	r.metrics[name] = c
}

func checkLabels(name string, labels []string) {
	for _, l := range labels {
		if !labelNameRe.MatchString(l) || strings.HasPrefix(l, "__") {
			panic(fmt.Sprintf("metric: %s: invalid label name %q", name, l))
		}
	}
}

// Counter registers a monotonically increasing counter with the given label
// names. Counter names end in _total by convention.
func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	checkLabels(name, labels)
	c := &Counter{vec: newVec(name, help, "counter", labels, newFloatCell)}
	r.register(name, c)
	return c
}

// Gauge registers a value that can go up and down.
func (r *Registry) Gauge(name, help string, labels ...string) *Gauge {
	checkLabels(name, labels)
	g := &Gauge{vec: newVec(name, help, "gauge", labels, newFloatCell)}
	r.register(name, g)
	return g
}

// GaugeFunc registers an unlabelled gauge whose value is computed at scrape
// time — for things the runtime already tracks (goroutines, heap).
func (r *Registry) GaugeFunc(name, help string, fn func() float64) {
	r.register(name, gaugeFunc{name: name, help: help, fn: fn})
}

// Histogram registers a histogram with cumulative buckets. Buckets must be
// strictly increasing; +Inf is appended automatically.
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *Histogram {
	checkLabels(name, labels)
	if len(buckets) == 0 {
		panic(fmt.Sprintf("metric: %s: histogram needs at least one bucket", name))
	}
	for i := 1; i < len(buckets); i++ {
		if buckets[i] <= buckets[i-1] {
			panic(fmt.Sprintf("metric: %s: buckets must be strictly increasing", name))
		}
	}
	n := len(buckets)
	h := &Histogram{
		vec:     newVec(name, help, "histogram", labels, func() *histSeries { return &histSeries{counts: make([]atomic.Uint64, n)} }),
		buckets: append([]float64(nil), buckets...),
	}
	r.register(name, h)
	return h
}

// Handler serves the registry in Prometheus text exposition format 0.0.4.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(r.Expose()))
	})
}

// Expose renders every metric, sorted by name then label values, so two
// scrapes of the same state are byte-identical — handy in tests and diffs.
func (r *Registry) Expose() string {
	r.mu.Lock()
	names := make([]string, 0, len(r.metrics))
	for n := range r.metrics {
		names = append(names, n)
	}
	sort.Strings(names)
	cols := make([]collector, len(names))
	for i, n := range names {
		cols[i] = r.metrics[n]
	}
	r.mu.Unlock()

	var sb strings.Builder
	for _, c := range cols {
		c.write(&sb)
	}
	return sb.String()
}

// vec is the label-values → series map shared by the three kinds. The series
// key is the label values joined by NUL; a NUL inside a label value would
// alias two series, which is acceptable for a value no sane caller produces.
type vec[T any] struct {
	name, help, kind string
	labels           []string
	newSeries        func() *T
	mu               sync.RWMutex
	series           map[string]*T
}

func newVec[T any](name, help, kind string, labels []string, newSeries func() *T) vec[T] {
	return vec[T]{name: name, help: help, kind: kind, labels: labels, newSeries: newSeries, series: map[string]*T{}}
}

func (v *vec[T]) get(values []string) *T {
	if len(values) != len(v.labels) {
		panic(fmt.Sprintf("metric: %s: got %d label values, want %d (%s)", v.name, len(values), len(v.labels), strings.Join(v.labels, ",")))
	}
	key := strings.Join(values, "\x00")
	v.mu.RLock()
	s, ok := v.series[key]
	v.mu.RUnlock()
	if ok {
		return s
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok = v.series[key]; ok {
		return s
	}
	s = v.newSeries()
	v.series[key] = s
	return s
}

// snapshot returns (key, series) pairs in key order.
func (v *vec[T]) snapshot() ([]string, []*T) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	keys := make([]string, 0, len(v.series))
	for k := range v.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*T, len(keys))
	for i, k := range keys {
		out[i] = v.series[k]
	}
	return keys, out
}

func (v *vec[T]) header(sb *strings.Builder) {
	sb.WriteString("# HELP ")
	sb.WriteString(v.name)
	sb.WriteByte(' ')
	sb.WriteString(escapeHelp(v.help))
	sb.WriteString("\n# TYPE ")
	sb.WriteString(v.name)
	sb.WriteByte(' ')
	sb.WriteString(v.kind)
	sb.WriteByte('\n')
}

// labelSet renders {a="x",b="y"} for a series key, with extra name/value
// pairs appended (histogram "le"). Empty when there are no labels at all.
func (v *vec[T]) labelSet(key string, extra ...string) string {
	if len(v.labels) == 0 && len(extra) == 0 {
		return ""
	}
	values := strings.Split(key, "\x00")
	var sb strings.Builder
	sb.WriteByte('{')
	for i, l := range v.labels {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(l)
		sb.WriteString(`="`)
		sb.WriteString(escapeLabel(values[i]))
		sb.WriteByte('"')
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if sb.Len() > 1 {
			sb.WriteByte(',')
		}
		sb.WriteString(extra[i])
		sb.WriteString(`="`)
		sb.WriteString(escapeLabel(extra[i+1]))
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

// line writes one sample: name[suffix]{labels} value.
func (v *vec[T]) line(sb *strings.Builder, suffix, labels, value string) {
	sb.WriteString(v.name)
	sb.WriteString(suffix)
	sb.WriteString(labels)
	sb.WriteByte(' ')
	sb.WriteString(value)
	sb.WriteByte('\n')
}

// floatCell is a float64 with atomic add/set, stored as bits.
type floatCell struct{ bits atomic.Uint64 }

func newFloatCell() *floatCell { return &floatCell{} }

func (f *floatCell) add(delta float64) {
	for {
		old := f.bits.Load()
		next := math.Float64bits(math.Float64frombits(old) + delta)
		if f.bits.CompareAndSwap(old, next) {
			return
		}
	}
}

func (f *floatCell) set(v float64) { f.bits.Store(math.Float64bits(v)) }
func (f *floatCell) get() float64  { return math.Float64frombits(f.bits.Load()) }

// Counter only goes up. Add with a negative delta panics.
type Counter struct{ vec vec[floatCell] }

// Inc adds 1 to the series identified by the label values.
func (c *Counter) Inc(values ...string) { c.Add(1, values...) }

// Add adds delta (≥ 0) to the series identified by the label values.
func (c *Counter) Add(delta float64, values ...string) {
	if delta < 0 {
		panic(fmt.Sprintf("metric: %s: counter cannot decrease", c.vec.name))
	}
	c.vec.get(values).add(delta)
}

// Value returns the current value of a series — for tests and alerts.
func (c *Counter) Value(values ...string) float64 { return c.vec.get(values).get() }

func (c *Counter) write(sb *strings.Builder) {
	c.vec.header(sb)
	keys, series := c.vec.snapshot()
	for i, k := range keys {
		c.vec.line(sb, "", c.vec.labelSet(k), formatFloat(series[i].get()))
	}
}

// Gauge is a value that can move in either direction.
type Gauge struct{ vec vec[floatCell] }

// Set overwrites the series value.
func (g *Gauge) Set(v float64, values ...string) { g.vec.get(values).set(v) }

// Add moves the series value by delta, which may be negative.
func (g *Gauge) Add(delta float64, values ...string) { g.vec.get(values).add(delta) }

// Value returns the current value of a series.
func (g *Gauge) Value(values ...string) float64 { return g.vec.get(values).get() }

func (g *Gauge) write(sb *strings.Builder) {
	g.vec.header(sb)
	keys, series := g.vec.snapshot()
	for i, k := range keys {
		g.vec.line(sb, "", g.vec.labelSet(k), formatFloat(series[i].get()))
	}
}

type gaugeFunc struct {
	name, help string
	fn         func() float64
}

func (g gaugeFunc) write(sb *strings.Builder) {
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", g.name, escapeHelp(g.help), g.name, g.name, formatFloat(g.fn()))
}

// histSeries holds one label combination of a Histogram. counts[i] is the
// number of observations ≤ buckets[i]; count is the +Inf bucket.
type histSeries struct {
	counts []atomic.Uint64
	count  atomic.Uint64
	sum    floatCell
}

// Histogram counts observations into cumulative buckets.
type Histogram struct {
	vec     vec[histSeries]
	buckets []float64
}

// Observe records one value.
func (h *Histogram) Observe(v float64, values ...string) {
	s := h.vec.get(values)
	// Cumulative: an observation lands in every bucket whose bound is ≥ v.
	for i, b := range h.buckets {
		if v <= b {
			s.counts[i].Add(1)
		}
	}
	s.count.Add(1)
	s.sum.add(v)
}

// Count returns the number of observations for a series.
func (h *Histogram) Count(values ...string) uint64 { return h.vec.get(values).count.Load() }

func (h *Histogram) write(sb *strings.Builder) {
	h.vec.header(sb)
	keys, series := h.vec.snapshot()
	for i, k := range keys {
		s := series[i]
		for j, b := range h.buckets {
			h.vec.line(sb, "_bucket", h.vec.labelSet(k, "le", formatFloat(b)), strconv.FormatUint(s.counts[j].Load(), 10))
		}
		h.vec.line(sb, "_bucket", h.vec.labelSet(k, "le", "+Inf"), strconv.FormatUint(s.count.Load(), 10))
		h.vec.line(sb, "_sum", h.vec.labelSet(k), formatFloat(s.sum.get()))
		h.vec.line(sb, "_count", h.vec.labelSet(k), strconv.FormatUint(s.count.Load(), 10))
	}
}

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

var (
	labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	helpEscaper  = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
func escapeHelp(s string) string  { return helpEscaper.Replace(s) }
