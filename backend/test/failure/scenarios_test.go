package failure

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// §14.3 "Kill API giữa batch ingest": the reporter retries the same
// (localRunId, sequence) batches → no event is doubled, none is lost.
func TestKillAPIMidBatchThenReplay(t *testing.T) {
	h := newHarness(t)
	api := h.startAPI()

	const batches, per = 40, 25
	local := localRun(1)
	if r := h.createRun(local); r.code != 201 {
		t.Fatalf("create run: %d %v %v", r.code, r.body, r.err)
	}
	batch := func(i int) map[string]any {
		var events []any
		for k := 1; k <= per; k++ {
			seq := (i-1)*per + k
			events = append(events, event(seq, "tests/api.spec.ts", fmt.Sprintf("t%04d", seq)))
		}
		return map[string]any{"events": events}
	}

	// Four reporter workers stream the batches; the API dies under them.
	var mu sync.Mutex
	acked := map[int]bool{}
	var firstErr error
	work := make(chan int, batches)
	for i := 1; i <= batches; i++ {
		work <- i
	}
	close(work)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				r := h.post("/v1/runs/"+local+"/events", batch(i))
				mu.Lock()
				switch {
				case r.err != nil || r.code >= 500:
					if firstErr == nil {
						firstErr = fmt.Errorf("batch %d: code %d err %v", i, r.code, r.err)
					}
				case r.code == 200:
					acked[i] = true
				default:
					t.Errorf("batch %d before the kill: %d %v", i, r.code, r.body)
				}
				mu.Unlock()
			}
		}()
	}
	time.Sleep(120 * time.Millisecond)
	api.kill()
	wg.Wait()
	committed := h.count(`SELECT count(*) FROM run_events e JOIN runs r ON r.id = e.run_id WHERE r.local_run_id = $1`, local)
	t.Logf("killed api: %d/%d batches acked, %d events committed, first failure: %v", len(acked), batches, committed, firstErr)
	if committed < len(acked)*per {
		t.Fatalf("acked %d batches but only %d events are committed — an ack without a commit", len(acked), committed)
	}

	// The reporter's spool replays everything it did not see acked; here we
	// replay all of it, the harsher case.
	api = h.startAPI()
	accepted, duplicates := 0, 0
	for i := 1; i <= batches; i++ {
		r := h.post("/v1/runs/"+local+"/events", batch(i))
		if r.code != 200 {
			t.Fatalf("replay batch %d: %d %v %v", i, r.code, r.body, r.err)
		}
		a, d := int(r.body["accepted"].(float64)), int(r.body["duplicates"].(float64))
		if a+d != per {
			t.Errorf("replay batch %d: accepted %d + duplicates %d != %d", i, a, d, per)
		}
		accepted, duplicates = accepted+a, duplicates+d
	}
	if accepted+committed != batches*per {
		t.Errorf("replay accepted %d + already committed %d != %d", accepted, committed, batches*per)
	}
	if duplicates != committed {
		t.Errorf("replay reported %d duplicates, %d events were committed before the kill", duplicates, committed)
	}
	if r := h.post("/v1/runs/"+local+"/finish", map[string]any{}); r.code != 200 {
		t.Fatalf("finish: %d %v", r.code, r.body)
	}

	// Invariants: every sequence exactly once, every test exactly one attempt.
	if n := h.count(`SELECT count(*) FROM run_events e JOIN runs r ON r.id = e.run_id WHERE r.local_run_id = $1`, local); n != batches*per {
		t.Errorf("run_events: %d, want %d", n, batches*per)
	}
	if n := h.count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1`, local); n != batches*per {
		t.Errorf("test_results: %d, want %d", n, batches*per)
	}
	if n := h.count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1 AND t.attempt_count <> 1`, local); n != 0 {
		t.Errorf("%d results with attempt_count != 1 — a replayed event was applied twice", n)
	}
	if n := h.count(`SELECT count(*) FROM test_result_attempts a JOIN test_results t ON t.id = a.test_result_id JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1`, local); n != batches*per {
		t.Errorf("attempts: %d, want %d", n, batches*per)
	}
	if !api.alive() {
		t.Error("api died during replay")
	}
}

// §14.3 "Kill worker giữa job" and "queue không khả dụng": runs are
// ingested and finished with no worker alive (reporter never sees a 5xx),
// the worker is killed while draining, and the next worker finishes the
// job — every result resolved, one identity per fingerprint despite the
// re-run and the 4-way concurrency.
func TestKillWorkerMidJobThenRecover(t *testing.T) {
	h := newHarness(t)
	h.startAPI()

	// 36 runs × 3 requests stays under the per-token burst (§7.4: 120).
	const runs, tests = 36, 12
	for n := 1; n <= runs; n++ {
		local := localRun(n)
		if r := h.createRun(local); r.code != 201 {
			t.Fatalf("run %d create: %d %v %v", n, r.code, r.body, r.err)
		}
		var events []any
		for k := 1; k <= tests; k++ {
			events = append(events, event(k, "tests/shared.spec.ts", fmt.Sprintf("s%02d", k)))
		}
		if r := h.post("/v1/runs/"+local+"/events", map[string]any{"events": events}); r.code != 200 {
			t.Fatalf("run %d events with no worker: %d %v %v", n, r.code, r.body, r.err)
		}
		if r := h.post("/v1/runs/"+local+"/finish", map[string]any{}); r.code != 200 {
			t.Fatalf("run %d finish with no worker: %d %v %v", n, r.code, r.body, r.err)
		}
	}
	if s := h.jobsByStatus(); s["pending"] != runs {
		t.Fatalf("jobs after finish with no worker: %v, want %d pending", s, runs)
	}
	if n := h.count(`SELECT count(*) FROM test_results WHERE stable_test_id IS NOT NULL`); n != 0 {
		t.Fatalf("%d results resolved with no worker running", n)
	}

	// Kill the worker as soon as it has started draining.
	w := h.startWorker()
	h.waitFor("worker to complete a job", 20*time.Second, func() bool { return h.jobsByStatus()["done"] >= 1 })
	w.kill()
	mid := h.jobsByStatus()
	t.Logf("killed worker mid-drain: %v", mid)
	if mid["done"] == runs {
		t.Log("note: the worker drained everything before the kill landed; recovery path not exercised this run")
	}

	// The dead worker's leases are 2 min (ingest.Jobs). Advance the clock in
	// the DB instead of sleeping: Claim's `leased_until < now()` recovery
	// branch is what runs either way, unchanged.
	h.exec(`UPDATE jobs SET leased_until = now() - interval '1 minute' WHERE status = 'running'`)

	h.startWorker()
	h.waitFor("every job to finish", 30*time.Second, func() bool {
		s := h.jobsByStatus()
		return s["done"] == runs && s["pending"] == 0 && s["running"] == 0
	})
	if s := h.jobsByStatus(); s["dead"] != 0 || s["done"] != runs {
		t.Errorf("jobs: %v", s)
	}
	if n := h.count(`SELECT count(*) FROM test_results WHERE stable_test_id IS NULL`); n != 0 {
		t.Errorf("%d results still unresolved", n)
	}
	if n := h.count(`SELECT count(*) FROM test_identities WHERE status = 'active'`); n != tests {
		t.Errorf("active identities: %d, want %d — a re-run or a concurrent run created a duplicate", n, tests)
	}
	if n := h.count(`SELECT count(*) FROM test_identity_aliases WHERE valid_to IS NULL`); n != tests {
		t.Errorf("current aliases: %d, want %d", n, tests)
	}
	if n := h.count(`SELECT count(DISTINCT stable_test_id) FROM test_results`); n != tests {
		t.Errorf("distinct stable ids across runs: %d, want %d", n, tests)
	}
	if n := h.count(`SELECT count(*) FROM runs WHERE status IN ('queued', 'running')`); n != 0 {
		t.Errorf("%d runs still open", n)
	}
	t.Logf("jobs re-run after the kill: %d", h.count(`SELECT count(*) FROM jobs WHERE attempts > 1`))
	// Every re-run must come from the dead worker's lease, never from the
	// handler failing: the first run of this harness found 4-way concurrent
	// resolves of the same suite deadlocking in Postgres (40P01), hidden by
	// the queue's retry — each one a 5 s backoff on the §14.2 resolver p99.
	if n := h.count(`SELECT count(*) FROM jobs WHERE last_error IS NOT NULL`); n != 0 {
		t.Errorf("%d jobs failed at least once — the retry is masking a handler error", n)
	}
	rows, err := h.db.Q(h.ctx).Query(h.ctx, `SELECT coalesce(last_error, '<lease expiry, no error>'), count(*) FROM jobs WHERE attempts > 1 GROUP BY 1 ORDER BY 2 DESC`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var why string
		var n int
		_ = rows.Scan(&why, &n)
		t.Logf("  %3d × %s", n, why)
	}
}

// §14.3 "Restart kết nối DB": every backend of api and worker is
// terminated under traffic. The reporter's retry gets through, the worker
// keeps draining, nothing stays `running` forever, and no request is ever
// answered with a 4xx that blames the client for the outage.
func TestDBConnectionsDroppedUnderTraffic(t *testing.T) {
	h := newHarness(t)
	api, w := h.startAPI(), h.startWorker()

	dropConnections := func() int {
		return h.count(`SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()`)
	}
	// postWithRetry is the reporter's behaviour: retry 5xx/transport errors,
	// never a 4xx.
	postWithRetry := func(path string, body any) (resp, int) {
		var last resp
		for attempt := 1; attempt <= 10; attempt++ {
			last = h.post(path, body)
			if last.err == nil && last.code < 500 {
				if last.code >= 400 {
					t.Fatalf("%s answered %d %v during a DB blip — the client is blamed for the outage", path, last.code, last.body)
				}
				return last, attempt
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("%s did not recover in 10 attempts: %d %v %v", path, last.code, last.body, last.err)
		return last, 0
	}
	resolved := func(local string) bool {
		return h.count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1 AND t.stable_test_id IS NULL`, local) == 0 &&
			h.count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1`, local) > 0
	}

	for round := 1; round <= 3; round++ {
		local := localRun(round)
		// A pooled connection killed by the previous drop is only found dead
		// on its next use: that request is a 503 the reporter retries.
		if r, _ := postWithRetry("/v1/runs", map[string]any{
			"localRunId": local, "environmentKey": "ci",
			"trigger":   map[string]any{"ref": "refs/heads/main", "commitSha": "deadbeef"},
			"startedAt": time.Now().UTC().Format(time.RFC3339),
		}); r.code != 201 {
			t.Fatalf("round %d create: %d %v %v", round, r.code, r.body, r.err)
		}
		var events []any
		for k := 1; k <= 8; k++ {
			events = append(events, event(k, "tests/db.spec.ts", fmt.Sprintf("d%02d", k)))
		}
		if r, _ := postWithRetry("/v1/runs/"+local+"/events", map[string]any{"events": events}); r.code != 200 {
			t.Fatalf("round %d events: %d %v %v", round, r.code, r.body, r.err)
		}
		killed := dropConnections()
		_, attempts := postWithRetry("/v1/runs/"+local+"/finish", map[string]any{})
		t.Logf("round %d: terminated %d backends; finish succeeded on attempt %d", round, killed, attempts)
		h.waitFor(fmt.Sprintf("round %d run to be resolved by the surviving worker", round), 20*time.Second, func() bool { return resolved(local) })
		if round == 2 {
			// Once more while the worker is idle-polling, not mid-job.
			dropConnections()
			time.Sleep(300 * time.Millisecond)
		}
	}
	if !api.alive() || !w.alive() {
		t.Fatalf("processes after the blips: api alive=%v worker alive=%v", api.alive(), w.alive())
	}
	// Round 3 resolved inline (tier 1: the aliases exist by then); its
	// run.finished job may still be queued behind the worker's poll.
	h.waitFor("the queue to drain", 20*time.Second, func() bool {
		s := h.jobsByStatus()
		return s["pending"] == 0 && s["running"] == 0
	})
	if n := h.count(`SELECT count(*) FROM runs WHERE status = 'running'`); n != 0 {
		t.Errorf("%d runs stuck in running", n)
	}
	if s := h.jobsByStatus(); s["dead"] != 0 || s["running"] != 0 || s["pending"] != 0 {
		t.Errorf("jobs after the blips: %v", s)
	}
	if r := h.post("/v1/runs", map[string]any{}); r.code != 400 {
		t.Errorf("api after the blips answers a bad request with %d %v %v, want 400", r.code, r.body, r.err)
	}
}
