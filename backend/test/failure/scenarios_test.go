package failure

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/test/harness"
)

// §14.3 "Kill API giữa batch ingest": the reporter retries the same
// (localRunId, sequence) batches → no event is doubled, none is lost.
func TestKillAPIMidBatchThenReplay(t *testing.T) {
	h := harness.New(t)
	api := h.StartAPI()

	const batches, per = 40, 25
	local := harness.LocalRun(1)
	if r := h.CreateRun(local); r.Code != 201 {
		t.Fatalf("create run: %d %v %v", r.Code, r.Body, r.Err)
	}
	batch := func(i int) map[string]any {
		var events []any
		for k := 1; k <= per; k++ {
			seq := (i-1)*per + k
			events = append(events, harness.Event(seq, "tests/api.spec.ts", fmt.Sprintf("t%04d", seq)))
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
				r := h.Post("/v1/runs/"+local+"/events", batch(i))
				mu.Lock()
				switch {
				case r.Err != nil || r.Code >= 500:
					if firstErr == nil {
						firstErr = fmt.Errorf("batch %d: code %d err %v", i, r.Code, r.Err)
					}
				case r.Code == 200:
					acked[i] = true
				default:
					t.Errorf("batch %d before the kill: %d %v", i, r.Code, r.Body)
				}
				mu.Unlock()
			}
		}()
	}
	time.Sleep(120 * time.Millisecond)
	api.Kill()
	wg.Wait()
	committed := h.Count(`SELECT count(*) FROM run_events e JOIN runs r ON r.id = e.run_id WHERE r.local_run_id = $1`, local)
	t.Logf("killed api: %d/%d batches acked, %d events committed, first failure: %v", len(acked), batches, committed, firstErr)
	if committed < len(acked)*per {
		t.Fatalf("acked %d batches but only %d events are committed — an ack without a commit", len(acked), committed)
	}

	// The reporter's spool replays everything it did not see acked; here we
	// replay all of it, the harsher case.
	api = h.StartAPI()
	accepted, duplicates := 0, 0
	for i := 1; i <= batches; i++ {
		r := h.Post("/v1/runs/"+local+"/events", batch(i))
		if r.Code != 200 {
			t.Fatalf("replay batch %d: %d %v %v", i, r.Code, r.Body, r.Err)
		}
		a, d := int(r.Body["accepted"].(float64)), int(r.Body["duplicates"].(float64))
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
	if r := h.Post("/v1/runs/"+local+"/finish", map[string]any{}); r.Code != 200 {
		t.Fatalf("finish: %d %v", r.Code, r.Body)
	}

	// Invariants: every sequence exactly once, every test exactly one attempt.
	if n := h.Count(`SELECT count(*) FROM run_events e JOIN runs r ON r.id = e.run_id WHERE r.local_run_id = $1`, local); n != batches*per {
		t.Errorf("run_events: %d, want %d", n, batches*per)
	}
	if n := h.Count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1`, local); n != batches*per {
		t.Errorf("test_results: %d, want %d", n, batches*per)
	}
	if n := h.Count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1 AND t.attempt_count <> 1`, local); n != 0 {
		t.Errorf("%d results with attempt_count != 1 — a replayed event was applied twice", n)
	}
	if n := h.Count(`SELECT count(*) FROM test_result_attempts a JOIN test_results t ON t.id = a.test_result_id JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1`, local); n != batches*per {
		t.Errorf("attempts: %d, want %d", n, batches*per)
	}
	if !api.Alive() {
		t.Error("api died during replay")
	}
}

// §14.3 "Kill worker giữa job" and "queue không khả dụng": runs are
// ingested and finished with no worker alive (reporter never sees a 5xx),
// the worker is killed while draining, and the next worker finishes the
// job — every result resolved, one identity per fingerprint despite the
// re-run and the 4-way concurrency.
func TestKillWorkerMidJobThenRecover(t *testing.T) {
	h := harness.New(t)
	h.StartAPI()

	// 36 runs × 3 requests stays under the per-token burst (§7.4: 120).
	const runs, tests = 36, 12
	for n := 1; n <= runs; n++ {
		local := harness.LocalRun(n)
		if r := h.CreateRun(local); r.Code != 201 {
			t.Fatalf("run %d create: %d %v %v", n, r.Code, r.Body, r.Err)
		}
		var events []any
		for k := 1; k <= tests; k++ {
			events = append(events, harness.Event(k, "tests/shared.spec.ts", fmt.Sprintf("s%02d", k)))
		}
		if r := h.Post("/v1/runs/"+local+"/events", map[string]any{"events": events}); r.Code != 200 {
			t.Fatalf("run %d events with no worker: %d %v %v", n, r.Code, r.Body, r.Err)
		}
		if r := h.Post("/v1/runs/"+local+"/finish", map[string]any{}); r.Code != 200 {
			t.Fatalf("run %d finish with no worker: %d %v %v", n, r.Code, r.Body, r.Err)
		}
	}
	if s := h.JobsByStatus(); s["pending"] != runs {
		t.Fatalf("jobs after finish with no worker: %v, want %d pending", s, runs)
	}
	if n := h.Count(`SELECT count(*) FROM test_results WHERE stable_test_id IS NOT NULL`); n != 0 {
		t.Fatalf("%d results resolved with no worker running", n)
	}

	// Kill the worker as soon as it has started draining.
	w := h.StartWorker()
	h.WaitFor("worker to complete a job", 20*time.Second, func() bool { return h.JobsByStatus()["done"] >= 1 })
	w.Kill()
	mid := h.JobsByStatus()
	t.Logf("killed worker mid-drain: %v", mid)
	if mid["done"] == runs {
		t.Log("note: the worker drained everything before the kill landed; recovery path not exercised this run")
	}

	// The dead worker's leases are 2 min (ingest.Jobs). Advance the clock in
	// the DB instead of sleeping: Claim's `leased_until < now()` recovery
	// branch is what runs either way, unchanged.
	h.Exec(`UPDATE jobs SET leased_until = now() - interval '1 minute' WHERE status = 'running'`)

	h.StartWorker()
	h.WaitFor("every job to finish", 30*time.Second, func() bool {
		s := h.JobsByStatus()
		return s["done"] == runs && s["pending"] == 0 && s["running"] == 0
	})
	if s := h.JobsByStatus(); s["dead"] != 0 || s["done"] != runs {
		t.Errorf("jobs: %v", s)
	}
	if n := h.Count(`SELECT count(*) FROM test_results WHERE stable_test_id IS NULL`); n != 0 {
		t.Errorf("%d results still unresolved", n)
	}
	if n := h.Count(`SELECT count(*) FROM test_identities WHERE status = 'active'`); n != tests {
		t.Errorf("active identities: %d, want %d — a re-run or a concurrent run created a duplicate", n, tests)
	}
	if n := h.Count(`SELECT count(*) FROM test_identity_aliases WHERE valid_to IS NULL`); n != tests {
		t.Errorf("current aliases: %d, want %d", n, tests)
	}
	if n := h.Count(`SELECT count(DISTINCT stable_test_id) FROM test_results`); n != tests {
		t.Errorf("distinct stable ids across runs: %d, want %d", n, tests)
	}
	if n := h.Count(`SELECT count(*) FROM runs WHERE status IN ('queued', 'running')`); n != 0 {
		t.Errorf("%d runs still open", n)
	}
	t.Logf("jobs re-run after the kill: %d", h.Count(`SELECT count(*) FROM jobs WHERE attempts > 1`))
	// Every re-run must come from the dead worker's lease, never from the
	// handler failing: the first run of this harness found 4-way concurrent
	// resolves of the same suite deadlocking in Postgres (40P01), hidden by
	// the queue's retry — each one a 5 s backoff on the §14.2 resolver p99.
	if n := h.Count(`SELECT count(*) FROM jobs WHERE last_error IS NOT NULL`); n != 0 {
		t.Errorf("%d jobs failed at least once — the retry is masking a handler error", n)
	}
	rows, err := h.DB.Q(h.Ctx).Query(h.Ctx, `SELECT coalesce(last_error, '<lease expiry, no error>'), count(*) FROM jobs WHERE attempts > 1 GROUP BY 1 ORDER BY 2 DESC`)
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
	h := harness.New(t)
	api, w := h.StartAPI(), h.StartWorker()

	dropConnections := func() int {
		return h.Count(`SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()`)
	}
	// postWithRetry is the reporter's behaviour: retry 5xx/transport errors,
	// never a 4xx.
	postWithRetry := func(path string, body any) (harness.Resp, int) {
		var last harness.Resp
		for attempt := 1; attempt <= 10; attempt++ {
			last = h.Post(path, body)
			if last.Err == nil && last.Code < 500 {
				if last.Code >= 400 {
					t.Fatalf("%s answered %d %v during a DB blip — the client is blamed for the outage", path, last.Code, last.Body)
				}
				return last, attempt
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatalf("%s did not recover in 10 attempts: %d %v %v", path, last.Code, last.Body, last.Err)
		return last, 0
	}
	resolved := func(local string) bool {
		return h.Count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1 AND t.stable_test_id IS NULL`, local) == 0 &&
			h.Count(`SELECT count(*) FROM test_results t JOIN runs r ON r.id = t.run_id WHERE r.local_run_id = $1`, local) > 0
	}

	for round := 1; round <= 3; round++ {
		local := harness.LocalRun(round)
		// A pooled connection killed by the previous drop is only found dead
		// on its next use: that request is a 503 the reporter retries.
		if r, _ := postWithRetry("/v1/runs", map[string]any{
			"localRunId": local, "environmentKey": "ci",
			"trigger":   map[string]any{"ref": "refs/heads/main", "commitSha": "deadbeef"},
			"startedAt": time.Now().UTC().Format(time.RFC3339),
		}); r.Code != 201 {
			t.Fatalf("round %d create: %d %v %v", round, r.Code, r.Body, r.Err)
		}
		var events []any
		for k := 1; k <= 8; k++ {
			events = append(events, harness.Event(k, "tests/db.spec.ts", fmt.Sprintf("d%02d", k)))
		}
		if r, _ := postWithRetry("/v1/runs/"+local+"/events", map[string]any{"events": events}); r.Code != 200 {
			t.Fatalf("round %d events: %d %v %v", round, r.Code, r.Body, r.Err)
		}
		killed := dropConnections()
		_, attempts := postWithRetry("/v1/runs/"+local+"/finish", map[string]any{})
		t.Logf("round %d: terminated %d backends; finish succeeded on attempt %d", round, killed, attempts)
		h.WaitFor(fmt.Sprintf("round %d run to be resolved by the surviving worker", round), 20*time.Second, func() bool { return resolved(local) })
		if round == 2 {
			// Once more while the worker is idle-polling, not mid-job.
			dropConnections()
			time.Sleep(300 * time.Millisecond)
		}
	}
	if !api.Alive() || !w.Alive() {
		t.Fatalf("processes after the blips: api alive=%v worker alive=%v", api.Alive(), w.Alive())
	}
	// Round 3 resolved inline (tier 1: the aliases exist by then); its
	// run.finished job may still be queued behind the worker's poll.
	h.WaitFor("the queue to drain", 20*time.Second, func() bool {
		s := h.JobsByStatus()
		return s["pending"] == 0 && s["running"] == 0
	})
	if n := h.Count(`SELECT count(*) FROM runs WHERE status = 'running'`); n != 0 {
		t.Errorf("%d runs stuck in running", n)
	}
	if s := h.JobsByStatus(); s["dead"] != 0 || s["running"] != 0 || s["pending"] != 0 {
		t.Errorf("jobs after the blips: %v", s)
	}
	if r := h.Post("/v1/runs", map[string]any{}); r.Code != 400 {
		t.Errorf("api after the blips answers a bad request with %d %v %v, want 400", r.Code, r.Body, r.Err)
	}
}
