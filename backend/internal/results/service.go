package results

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

type service struct {
	db *pg.DB
}

// New returns the Service over migrations/0003_results.sql.
func New(db *pg.DB) Service { return &service{db: db} }

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

const runColumns = `id, project_id, environment_id, local_run_id, status, flaky_count, error_reason,
	trigger, commit_sha, branch, framework, reporter, started_at, finished_at, created_at`

func scanRun(row interface{ Scan(dest ...any) error }) (Run, error) {
	var r Run
	var errorReason, commit, branch *string
	var finished *time.Time
	err := row.Scan(&r.ID, &r.ProjectID, &r.EnvironmentID, &r.LocalRunID, &r.Status, &r.FlakyCount, &errorReason,
		&r.Trigger, &commit, &branch, &r.Framework, &r.Reporter, &r.StartedAt, &finished, &r.CreatedAt)
	if err != nil {
		return Run{}, err
	}
	r.ErrorReason = deref(errorReason)
	r.CommitSHA = deref(commit)
	r.Branch = deref(branch)
	// §8: every timestamp is UTC on the way out, whatever the session TZ.
	r.StartedAt, r.CreatedAt = r.StartedAt.UTC(), r.CreatedAt.UTC()
	if finished != nil {
		r.FinishedAt = finished.UTC()
	}
	return r, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// orEmptyObject keeps NOT NULL jsonb columns satisfied when the caller has
// nothing to say.
func orEmptyObject(j json.RawMessage) json.RawMessage {
	if len(j) == 0 {
		return json.RawMessage(`{}`)
	}
	return j
}

func (s *service) CreateRun(ctx context.Context, in NewRun) (Run, bool, error) {
	if !isUUID(in.ProjectID) || !isUUID(in.EnvironmentID) {
		return Run{}, false, fmt.Errorf("%w: project/environment id", ErrInvalid)
	}
	if !isUUID(strings.ToLower(in.LocalRunID)) {
		return Run{}, false, fmt.Errorf("%w: localRunId must be a UUID", ErrInvalid)
	}
	if in.StartedAt.IsZero() {
		return Run{}, false, fmt.Errorf("%w: startedAt", ErrInvalid)
	}
	local := strings.ToLower(in.LocalRunID)

	// Insert-or-nothing then read: the loser of a race sees the winner's row.
	var r Run
	row := s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO runs (project_id, environment_id, local_run_id, status, trigger, commit_sha, branch, framework, reporter, started_at)
		VALUES ($1, $2, $3, 'running', $4, $5, $6, $7, $8, $9)
		ON CONFLICT (project_id, local_run_id) DO NOTHING
		RETURNING `+runColumns,
		in.ProjectID, in.EnvironmentID, local, orEmptyObject(in.Trigger), nullable(in.CommitSHA), nullable(in.Branch),
		orEmptyObject(in.Framework), orEmptyObject(in.Reporter), in.StartedAt.UTC())
	r, err := scanRun(row)
	if err == nil {
		return r, true, nil
	}
	if !errors.Is(err, pg.ErrNoRows) {
		return Run{}, false, mapErr(err)
	}
	r, err = s.GetRun(ctx, in.ProjectID, local)
	return r, false, err
}

func (s *service) GetRun(ctx context.Context, projectID, localRunID string) (Run, error) {
	localRunID = strings.ToLower(localRunID)
	if !isUUID(projectID) || !isUUID(localRunID) {
		return Run{}, ErrNotFound
	}
	r, err := scanRun(s.db.Q(ctx).QueryRow(ctx,
		`SELECT `+runColumns+` FROM runs WHERE project_id = $1 AND local_run_id = $2`, projectID, localRunID))
	return r, mapErr(err)
}

func (s *service) AppendAttempts(ctx context.Context, run RunID, events []AttemptEvent) (Appended, error) {
	if !isUUID(string(run)) {
		return Appended{}, ErrNotFound
	}
	for i, e := range events {
		if err := validateEvent(e); err != nil {
			return Appended{}, fmt.Errorf("event %d (sequence %d): %w", i, e.Sequence, err)
		}
	}
	var out Appended
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		// Lock the run: batches for one run apply one at a time, so the
		// per-test aggregates below always see every attempt so far.
		var status RunStatus
		err := q.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, string(run)).Scan(&status)
		if err != nil {
			return mapErr(err)
		}
		if status != RunRunning {
			return ErrRunFinished
		}
		// Five statements per batch, not per event: at 500 events a batch
		// the per-event version was 2 500 round trips on one connection and
		// held the pool for seconds under 100 concurrent reporters (§14.2).
		accepted, err := s.claimSequences(ctx, run, events)
		if err != nil {
			return err
		}
		out.Duplicates = len(events) - len(accepted)
		if len(accepted) == 0 {
			return nil
		}
		ids, existed, err := s.upsertResults(ctx, run, accepted)
		if err != nil {
			return err
		}
		if err := s.insertAttempts(ctx, ids, accepted); err != nil {
			return err
		}
		if len(existed) > 0 {
			if err := s.recomputeAggregates(ctx, existed); err != nil {
				return err
			}
		}
		out.Accepted = len(accepted)
		return nil
	})
	if err != nil {
		return Appended{}, err
	}
	return out, nil
}

// claimSequences records the batch's sequences and returns the events whose
// sequence the run had not seen — the idempotency key of §7.1 — in arrival
// order. A sequence repeated inside one batch counts once.
func (s *service) claimSequences(ctx context.Context, run RunID, events []AttemptEvent) ([]AttemptEvent, error) {
	seqs := make([]int64, 0, len(events))
	seen := map[int64]bool{}
	for _, e := range events {
		if !seen[e.Sequence] {
			seen[e.Sequence] = true
			seqs = append(seqs, e.Sequence)
		}
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		INSERT INTO run_events (run_id, sequence)
		SELECT $1, unnest($2::bigint[])
		ON CONFLICT DO NOTHING
		RETURNING sequence`, string(run), seqs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fresh := map[int64]bool{}
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			return nil, err
		}
		fresh[seq] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var accepted []AttemptEvent
	for _, e := range events {
		if fresh[e.Sequence] {
			delete(fresh, e.Sequence) // the first occurrence in the batch wins
			accepted = append(accepted, e)
		}
	}
	return accepted, nil
}

type resultKey struct {
	client     string
	occurrence int
}

// upsertResults creates or touches one TestResult per (clientTestId,
// occurrenceIndex) in the batch and returns their ids, plus the ids that
// already existed. A new result is written complete — aggregates computed
// from the batch's own attempts — so the common first-batch path never
// updates a freshly inserted row (a non-HOT update touching every index,
// the single most expensive statement under load in test/load). A result
// that existed is touched here and recomputed from all its attempts after
// the new ones are in. identity_raw is refreshed with the newest event's
// signals (§7.6 #4); stable_test_id is set once and never overwritten.
func (s *service) upsertResults(ctx context.Context, run RunID, events []AttemptEvent) (map[resultKey]TestResultID, []string, error) {
	// ON CONFLICT DO UPDATE may touch a row once per statement: collapse
	// the batch to one row per key.
	order := make([]resultKey, 0, len(events))
	byKey := map[resultKey][]AttemptEvent{}
	for _, e := range events {
		k := resultKey{e.ClientTestID, e.OccurrenceIndex}
		if _, ok := byKey[k]; !ok {
			order = append(order, k)
		}
		byKey[k] = append(byKey[k], e)
	}
	n := len(order)
	clients, occurrences, names, suites, raws, finals, stables := make([]string, n), make([]int32, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]*string, n)
	counts, totals, firstFailed, anyFailed := make([]int32, n), make([]int32, n), make([]*time.Time, n), make([]bool, n)
	for i, k := range order {
		batch := byKey[k]
		last := batch[len(batch)-1] // newest signals win
		clients[i], occurrences[i], names[i], suites[i], raws[i] = last.ClientTestID, int32(last.OccurrenceIndex), last.DisplayName, last.SuiteName, string(last.IdentityRaw)
		seen := map[int]bool{}
		var top AttemptEvent
		for _, e := range batch {
			if seen[e.Attempt] {
				continue // the first occurrence of an attempt in the batch wins, as in insertAttempts
			}
			seen[e.Attempt] = true
			if e.StableTestID != "" && stables[i] == nil {
				stables[i] = nullable(e.StableTestID)
			}
			counts[i]++
			totals[i] += int32(e.DurationMs)
			if e.Status == AttemptFailed || e.Status == AttemptTimedOut || e.Status == AttemptInterrupted {
				anyFailed[i] = true
				if !e.StartedAt.IsZero() && (firstFailed[i] == nil || e.StartedAt.Before(*firstFailed[i])) {
					t := e.StartedAt
					firstFailed[i] = &t
				}
			}
			if counts[i] == 1 || e.Attempt > top.Attempt {
				top = e
			}
		}
		finals[i] = string(FinalStatusOf(top.Outcome, top.Status))
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		INSERT INTO test_results (run_id, client_test_id, occurrence_index, display_name, suite_name, identity_raw, final_status, stable_test_id,
		                          attempt_count, duration_ms, first_failed_at, failure_observed_at)
		SELECT $1, c, o, d, s, r::jsonb, f, st::uuid, cnt, tot, ff, CASE WHEN af THEN now() END
		  FROM unnest($2::text[], $3::int[], $4::text[], $5::text[], $6::text[], $7::text[], $8::text[],
		              $9::int[], $10::int[], $11::timestamptz[], $12::bool[]) AS x(c, o, d, s, r, f, st, cnt, tot, ff, af)
		ON CONFLICT (run_id, client_test_id, occurrence_index) DO UPDATE SET
			identity_raw = EXCLUDED.identity_raw,
			stable_test_id = coalesce(test_results.stable_test_id, EXCLUDED.stable_test_id),
			updated_at = now()
		RETURNING id, client_test_id, occurrence_index, (xmax <> 0) AS existed`,
		string(run), clients, occurrences, names, suites, raws, finals, stables, counts, totals, firstFailed, anyFailed)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	defer rows.Close()
	ids := make(map[resultKey]TestResultID, n)
	var existed []string
	for rows.Next() {
		var id TestResultID
		var k resultKey
		var old bool
		if err := rows.Scan(&id, &k.client, &k.occurrence, &old); err != nil {
			return nil, nil, err
		}
		ids[k] = id
		if old {
			existed = append(existed, string(id))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, mapErr(err)
	}
	if len(ids) != n {
		return nil, nil, fmt.Errorf("results: upsert returned %d ids for %d keys", len(ids), n)
	}
	return ids, existed, nil
}

// insertAttempts stores every accepted event as an attempt of its result.
// A (result, attempt) the run already has is left as it was.
func (s *service) insertAttempts(ctx context.Context, ids map[resultKey]TestResultID, events []AttemptEvent) error {
	n := len(events)
	trIDs, attempts, statuses, outcomes := make([]string, n), make([]int32, n), make([]string, n), make([]string, n)
	errs, workers, starts, finishes, durations := make([]*string, n), make([]*string, n), make([]*time.Time, n), make([]*time.Time, n), make([]int32, n)
	for i, e := range events {
		trIDs[i] = string(ids[resultKey{e.ClientTestID, e.OccurrenceIndex}])
		attempts[i], statuses[i], outcomes[i], durations[i] = int32(e.Attempt), string(e.Status), string(e.Outcome), int32(e.DurationMs)
		if len(e.Error) > 0 {
			errs[i] = nullable(string(e.Error))
		}
		workers[i], starts[i], finishes[i] = nullable(e.WorkerID), nullableTime(e.StartedAt), nullableTime(e.FinishedAt)
	}
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO test_result_attempts (test_result_id, attempt, status, framework_outcome, error, worker_id, started_at, finished_at, duration_ms)
		SELECT t::uuid, a, s, o, e::jsonb, w, st, fi, d
		  FROM unnest($1::text[], $2::int[], $3::text[], $4::text[], $5::text[], $6::text[], $7::timestamptz[], $8::timestamptz[], $9::int[]) AS x(t, a, s, o, e, w, st, fi, d)
		ON CONFLICT (test_result_id, attempt) DO NOTHING`,
		trIDs, attempts, statuses, outcomes, errs, workers, starts, finishes, durations)
	return mapErr(err)
}

// recomputeAggregates derives each pre-existing result from all of its
// attempts in one pass: the last attempt (highest number, whatever order it arrived
// in) decides finalStatus — the CASE is FinalStatusOf in SQL — the rest
// are sums and firsts.
func (s *service) recomputeAggregates(ctx context.Context, list []string) error {
	_, err := s.db.Q(ctx).Exec(ctx, `
		UPDATE test_results tr SET
			final_status        = CASE
			                        WHEN a.framework_outcome = 'flaky' THEN 'flaky_pass'
			                        WHEN a.framework_outcome = 'skipped' OR (a.framework_outcome = 'expected' AND a.status = 'skipped') THEN 'skipped'
			                        WHEN a.framework_outcome = 'expected' THEN 'passed'
			                        WHEN a.status IN ('timed_out', 'interrupted') THEN a.status
			                        ELSE 'failed'
			                      END,
			attempt_count       = a.cnt,
			duration_ms         = a.total,
			first_failed_at     = a.first_failed,
			failure_observed_at = COALESCE(tr.failure_observed_at, CASE WHEN a.any_failed THEN now() END),
			updated_at          = now()
		FROM (
			SELECT test_result_id,
			       count(*) AS cnt,
			       coalesce(sum(duration_ms), 0)::integer AS total,
			       min(started_at) FILTER (WHERE status IN ('failed', 'timed_out', 'interrupted')) AS first_failed,
			       bool_or(status IN ('failed', 'timed_out', 'interrupted')) AS any_failed,
			       (array_agg(status ORDER BY attempt DESC))[1] AS status,
			       (array_agg(framework_outcome ORDER BY attempt DESC))[1] AS framework_outcome
			  FROM test_result_attempts WHERE test_result_id = ANY($1::uuid[])
			 GROUP BY test_result_id
		) a
		WHERE tr.id = a.test_result_id`, list)
	return err
}

func validateEvent(e AttemptEvent) error {
	switch {
	case e.Sequence < 0:
		return fmt.Errorf("%w: sequence", ErrInvalid)
	case e.ClientTestID == "" || len(e.ClientTestID) > 256:
		return fmt.Errorf("%w: clientTestId", ErrInvalid)
	case e.OccurrenceIndex < 0 || e.Attempt < 0:
		return fmt.Errorf("%w: occurrenceIndex/attempt", ErrInvalid)
	case e.DisplayName == "":
		return fmt.Errorf("%w: displayName", ErrInvalid)
	case len(e.IdentityRaw) == 0:
		return fmt.Errorf("%w: identity", ErrInvalid)
	case e.DurationMs < 0:
		return fmt.Errorf("%w: durationMs", ErrInvalid)
	case e.StableTestID != "" && !isUUID(e.StableTestID):
		return fmt.Errorf("%w: stableTestId", ErrInvalid)
	}
	switch e.Status {
	case AttemptPassed, AttemptFailed, AttemptSkipped, AttemptTimedOut, AttemptInterrupted:
	default:
		return fmt.Errorf("%w: status %q", ErrInvalid, e.Status)
	}
	switch e.Outcome {
	case OutcomeExpected, OutcomeUnexpected, OutcomeFlaky, OutcomeSkipped:
	default:
		return fmt.Errorf("%w: outcome %q", ErrInvalid, e.Outcome)
	}
	return nil
}

func (s *service) FinishRun(ctx context.Context, run RunID, finishedAt time.Time) (Run, error) {
	if !isUUID(string(run)) {
		return Run{}, ErrNotFound
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	var out Run
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var status RunStatus
		if err := q.QueryRow(ctx, `SELECT status FROM runs WHERE id = $1 FOR UPDATE`, string(run)).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status != RunRunning {
			// Already closed: report the run as it is (§7.2 #3).
			r, err := scanRun(q.QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1`, string(run)))
			out = r
			return mapErr(err)
		}
		r, err := scanRun(q.QueryRow(ctx, `
			UPDATE runs SET
				status = CASE WHEN EXISTS (
					SELECT 1 FROM test_results WHERE run_id = runs.id AND final_status IN ('failed', 'timed_out', 'interrupted')
				) THEN 'failed' ELSE 'passed' END,
				flaky_count = (SELECT count(*) FROM test_results WHERE run_id = runs.id AND final_status = 'flaky_pass'),
				finished_at = $2,
				updated_at  = now()
			WHERE id = $1
			RETURNING `+runColumns, string(run), finishedAt.UTC()))
		out = r
		return mapErr(err)
	})
	return out, err
}

func (s *service) ListResults(ctx context.Context, run RunID) ([]TestResult, error) {
	if !isUUID(string(run)) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, run_id, stable_test_id, client_test_id, occurrence_index, display_name, suite_name, identity_raw,
		       final_status, attempt_count, first_failed_at, failure_observed_at, duration_ms, created_at
		  FROM test_results WHERE run_id = $1 ORDER BY created_at, client_test_id, occurrence_index`, string(run))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TestResult
	for rows.Next() {
		var t TestResult
		var stable *string
		var firstFailed, observed *time.Time
		if err := rows.Scan(&t.ID, &t.RunID, &stable, &t.ClientTestID, &t.OccurrenceIndex, &t.DisplayName, &t.SuiteName, &t.IdentityRaw,
			&t.FinalStatus, &t.AttemptCount, &firstFailed, &observed, &t.DurationMs, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.StableTestID = deref(stable)
		t.CreatedAt = t.CreatedAt.UTC()
		if firstFailed != nil {
			t.FirstFailedAt = firstFailed.UTC()
		}
		if observed != nil {
			t.FailureObservedAt = observed.UTC()
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pg.ErrNoRows):
		return ErrNotFound
	case pg.IsForeignKeyViolation(err):
		return ErrNotFound
	case pg.IsCheckViolation(err):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}

func (s *service) SetStableIDs(ctx context.Context, run RunID, byClient map[string]string) (int, error) {
	if !isUUID(string(run)) {
		return 0, ErrNotFound
	}
	if len(byClient) == 0 {
		return 0, nil
	}
	clients := make([]string, 0, len(byClient))
	ids := make([]string, 0, len(byClient))
	for c, id := range byClient {
		if !isUUID(id) {
			return 0, fmt.Errorf("%w: stable id for %s", ErrInvalid, c)
		}
		clients = append(clients, c)
		ids = append(ids, id)
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `
		UPDATE test_results tr SET stable_test_id = m.sid::uuid, updated_at = now()
		  FROM unnest($2::text[], $3::text[]) AS m(cid, sid)
		 WHERE tr.run_id = $1 AND tr.client_test_id = m.cid AND tr.stable_test_id IS NULL`, string(run), clients, ids)
	if err != nil {
		return 0, mapErr(err)
	}
	return int(tag.RowsAffected()), nil
}

func (s *service) AutoClose(ctx context.Context, startedBefore time.Time) ([]Run, error) {
	now := time.Now().UTC()
	rows, err := s.db.Q(ctx).Query(ctx, `
		UPDATE runs SET status = 'error', error_reason = 'RUN_TIMED_OUT', finished_at = $2, updated_at = $2
		 WHERE status = 'running' AND started_at < $1
		RETURNING `+runColumns, startedBefore.UTC(), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *service) FindAttempt(ctx context.Context, run RunID, clientTestID string, occurrenceIndex, attempt int) (string, error) {
	if !isUUID(string(run)) {
		return "", ErrNotFound
	}
	var id string
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT a.id FROM test_result_attempts a
		  JOIN test_results t ON t.id = a.test_result_id
		 WHERE t.run_id = $1 AND t.client_test_id = $2 AND t.occurrence_index = $3 AND a.attempt = $4`,
		string(run), clientTestID, occurrenceIndex, attempt).Scan(&id)
	return id, mapErr(err)
}

func (s *service) GetRunByID(ctx context.Context, id RunID) (Run, error) {
	if !isUUID(string(id)) {
		return Run{}, ErrNotFound
	}
	r, err := scanRun(s.db.Q(ctx).QueryRow(ctx, `SELECT `+runColumns+` FROM runs WHERE id = $1`, string(id)))
	return r, mapErr(err)
}

func (s *service) History(ctx context.Context, stableTestID, environmentID string, since time.Time) ([]HistoryPoint, error) {
	if !isUUID(stableTestID) || !isUUID(environmentID) {
		return nil, ErrNotFound
	}
	// Both tables are this module's (§6.9.5), so the join is legal.
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT r.id, r.started_at, t.final_status, t.failure_observed_at
		  FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE t.stable_test_id = $1 AND r.environment_id = $2 AND r.started_at >= $3
		   AND r.status IN ('passed', 'failed', 'error')
		 ORDER BY r.started_at, t.occurrence_index`, stableTestID, environmentID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryPoint
	for rows.Next() {
		var p HistoryPoint
		var observed *time.Time
		if err := rows.Scan(&p.RunID, &p.StartedAt, &p.FinalStatus, &observed); err != nil {
			return nil, err
		}
		p.StartedAt = p.StartedAt.UTC()
		if observed != nil {
			p.ObservedAt = observed.UTC()
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// HistoryMany is History for every identity of a finished run at once:
// one query instead of one per identity, ordered so the caller can slice
// it per identity. Same rows, same filter, same order within an identity.
func (s *service) HistoryMany(ctx context.Context, stableTestIDs []string, environmentID string, since time.Time) (map[string][]HistoryPoint, error) {
	if !isUUID(environmentID) {
		return nil, ErrNotFound
	}
	for _, id := range stableTestIDs {
		if !isUUID(id) {
			return nil, ErrNotFound
		}
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT t.stable_test_id, r.id, r.started_at, t.final_status, t.failure_observed_at
		  FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE t.stable_test_id = ANY($1::uuid[]) AND r.environment_id = $2 AND r.started_at >= $3
		   AND r.status IN ('passed', 'failed', 'error')
		 ORDER BY t.stable_test_id, r.started_at, t.occurrence_index`, stableTestIDs, environmentID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]HistoryPoint, len(stableTestIDs))
	for rows.Next() {
		var id string
		var p HistoryPoint
		var observed *time.Time
		if err := rows.Scan(&id, &p.RunID, &p.StartedAt, &p.FinalStatus, &observed); err != nil {
			return nil, err
		}
		p.StartedAt = p.StartedAt.UTC()
		if observed != nil {
			p.ObservedAt = observed.UTC()
		}
		out[id] = append(out[id], p)
	}
	return out, rows.Err()
}

func (s *service) GetResult(ctx context.Context, projectID string, id TestResultID) (TestResult, error) {
	if !isUUID(projectID) || !isUUID(string(id)) {
		return TestResult{}, ErrNotFound
	}
	// runs and test_results are both this module's (§6.9.5); the tenant
	// condition rides on runs.project_id.
	var t TestResult
	var stable, category *string
	var firstFailed, observed *time.Time
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT t.id, t.run_id, r.project_id, t.stable_test_id, t.client_test_id, t.occurrence_index, t.display_name, t.suite_name, t.identity_raw,
		       t.final_status, t.failure_category, t.attempt_count, t.first_failed_at, t.failure_observed_at, t.duration_ms, t.created_at
		  FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE t.id = $1 AND r.project_id = $2`, string(id), projectID,
	).Scan(&t.ID, &t.RunID, &t.ProjectID, &stable, &t.ClientTestID, &t.OccurrenceIndex, &t.DisplayName, &t.SuiteName, &t.IdentityRaw,
		&t.FinalStatus, &category, &t.AttemptCount, &firstFailed, &observed, &t.DurationMs, &t.CreatedAt)
	if err != nil {
		return TestResult{}, mapErr(err)
	}
	t.StableTestID, t.FailureCategory = deref(stable), deref(category)
	t.CreatedAt = t.CreatedAt.UTC()
	if firstFailed != nil {
		t.FirstFailedAt = firstFailed.UTC()
	}
	if observed != nil {
		t.FailureObservedAt = observed.UTC()
	}
	return t, nil
}

func (s *service) SetFailureCategory(ctx context.Context, id TestResultID, category string) error {
	if !isUUID(string(id)) {
		return ErrNotFound
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `UPDATE test_results SET failure_category = $2, updated_at = now() WHERE id = $1`, string(id), category)
	if err != nil {
		return mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
