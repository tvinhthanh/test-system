package pg

import "github.com/vinh/automation-testing-platform/backend/internal/platform/metric"

// Metrics registers the pool's gauges. A connection leak is invisible in
// heap numbers — pgx keeps a handful of small structs per connection — but
// shows up here as db_pool_conns_acquired that never comes back down, long
// before the pool is exhausted and requests start waiting.
//
//	db_pool_conns_acquired / _idle / _constructing  — snapshot at scrape
//	db_pool_max_conns                               — the pool's ceiling
//	db_pool_acquire_wait_seconds_total              — time callers spent waiting for a connection
//	db_pool_empty_acquires_total                    — acquires that had to wait
func (d *DB) Metrics(r *metric.Registry) {
	r.GaugeFunc("db_pool_conns_acquired", "Pooled connections checked out right now.", func() float64 { return float64(d.pool.Stat().AcquiredConns()) })
	r.GaugeFunc("db_pool_conns_idle", "Pooled connections open and idle.", func() float64 { return float64(d.pool.Stat().IdleConns()) })
	r.GaugeFunc("db_pool_conns_constructing", "Pooled connections being opened.", func() float64 { return float64(d.pool.Stat().ConstructingConns()) })
	r.GaugeFunc("db_pool_max_conns", "Maximum connections the pool will open.", func() float64 { return float64(d.pool.Stat().MaxConns()) })
	r.GaugeFunc("db_pool_acquire_wait_seconds_total", "Cumulative time spent waiting for a pooled connection.", func() float64 { return d.pool.Stat().AcquireDuration().Seconds() })
	r.GaugeFunc("db_pool_empty_acquires_total", "Acquires that found the pool empty and waited.", func() float64 { return float64(d.pool.Stat().EmptyAcquireCount()) })
}
