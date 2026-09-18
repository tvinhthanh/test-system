package query

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

type service struct{ db *pg.DB }

// New returns the read model over the pool. It never opens a transaction.
func New(db *pg.DB) Service { return &service{db: db} }

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

// ---- Runs -----------------------------------------------------------------

// runSelect is the Run DTO from one runs row plus its environment and
// counts. Used by Runs, Run and IdentityRuns with different WHERE clauses.
const runSelect = `
	SELECT r.id, coalesce(r.branch, ''), coalesce(r.commit_sha, ''), r.status, coalesce(e.key, ''), r.started_at, r.finished_at,
	       (SELECT count(*) FROM test_results t WHERE t.run_id = r.id),
	       (SELECT count(*) FROM test_results t WHERE t.run_id = r.id AND t.final_status = 'passed'),
	       (SELECT count(*) FROM test_results t WHERE t.run_id = r.id AND t.final_status IN ('failed', 'timed_out', 'interrupted')),
	       (SELECT count(*) FROM test_results t WHERE t.run_id = r.id AND t.final_status = 'flaky_pass')
	  FROM runs r LEFT JOIN environments e ON e.id = r.environment_id`

func scanRun(row interface{ Scan(dest ...any) error }) (Run, error) {
	var r Run
	var finished *time.Time
	if err := row.Scan(&r.ID, &r.Branch, &r.CommitSHA, &r.Status, &r.EnvironmentName, &r.StartedAt, &finished,
		&r.TotalCount, &r.PassedCount, &r.FailedCount, &r.FlakyCount); err != nil {
		return Run{}, err
	}
	r.StartedAt = r.StartedAt.UTC()
	r.FinishedAt = utc(finished)
	if finished != nil {
		ms := finished.Sub(r.StartedAt).Milliseconds()
		if ms < 0 {
			ms = 0
		}
		r.DurationMs = &ms
	}
	return r, nil
}

var validRunStatus = map[string]bool{"queued": true, "running": true, "passed": true, "failed": true, "error": true, "cancelled": true}

// Runs — sort startedAt DESC, id DESC; keyset cursor on (started_at, id);
// index runs_environment_started for the environment case, otherwise the
// project's runs in time order.
func (s *service) Runs(ctx context.Context, projectID string, f RunFilter) (Page[Run], error) {
	if !isUUID(projectID) {
		return Page[Run]{}, ErrNotFound
	}
	if len(f.Q) > 200 {
		return Page[Run]{}, fmt.Errorf("%w: q longer than 200 characters", ErrInvalid)
	}
	if f.Status != "" && !validRunStatus[f.Status] {
		return Page[Run]{}, fmt.Errorf("%w: status %q", ErrInvalid, f.Status)
	}
	limit := clamp(f.Limit, 20)
	fh := filterHash("runs", f.Q, f.Status, f.Environment, "-startedAt")
	var afterTime *time.Time
	var afterID *string
	if f.Cursor != "" {
		c, err := decodeCursor(f.Cursor, "runs", projectID, fh)
		if err != nil {
			return Page[Run]{}, err
		}
		t, err := time.Parse(time.RFC3339Nano, c.Time)
		if err != nil {
			return Page[Run]{}, ErrBadCursor
		}
		afterTime, afterID = &t, &c.ID
	}
	var q *string
	if f.Q != "" {
		like := "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(f.Q, `\`, `\\`), "%", `\%`), "_", `\_`) + "%"
		q = &like
	}
	rows, err := s.db.Q(ctx).Query(ctx, runSelect+`
		 WHERE r.project_id = $1
		   AND ($2::text IS NULL OR r.status = $2)
		   AND ($3::text IS NULL OR e.key = $3)
		   AND ($4::text IS NULL OR r.branch ILIKE $4 OR r.commit_sha ILIKE $4 OR r.local_run_id::text ILIKE $4)
		   AND ($5::timestamptz IS NULL OR (r.started_at, r.id) < ($5, $6::uuid))
		 ORDER BY r.started_at DESC, r.id DESC
		 LIMIT $7`, projectID, nullable(f.Status), nullable(f.Environment), q, afterTime, afterID, limit+1)
	if err != nil {
		return Page[Run]{}, err
	}
	defer rows.Close()
	var items []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return Page[Run]{}, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return Page[Run]{}, err
	}
	page := Page[Run]{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeCursor(cursor{Kind: "runs", Project: projectID, Filter: fh, Time: last.StartedAt.Format(time.RFC3339Nano), ID: last.ID})
	}
	if page.Items == nil {
		page.Items = []Run{}
	}
	return page, nil
}

func (s *service) Run(ctx context.Context, projectID, runID string) (Run, error) {
	if !isUUID(projectID) || !isUUID(runID) {
		return Run{}, ErrNotFound
	}
	r, err := scanRun(s.db.Q(ctx).QueryRow(ctx, runSelect+` WHERE r.project_id = $1 AND r.id = $2`, projectID, runID))
	return r, mapErr(err)
}

// ---- Results --------------------------------------------------------------

// Results — failures first, then suite and name, id as tie-breaker; keyset
// on (rank, display_name, id).
func (s *service) Results(ctx context.Context, projectID, runID string, limit int, cur string) (Page[Result], error) {
	if !isUUID(projectID) || !isUUID(runID) {
		return Page[Result]{}, ErrNotFound
	}
	limit = clamp(limit, 50)
	fh := filterHash("results", runID)
	var afterRank *int
	var afterName, afterID *string
	if cur != "" {
		c, err := decodeCursor(cur, "results", projectID, fh)
		if err != nil {
			return Page[Result]{}, err
		}
		afterRank, afterName, afterID = &c.Rank, &c.Str, &c.ID
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT t.id, t.display_name, coalesce(t.identity_raw->>'filePath', ''), t.final_status, t.attempt_count, t.duration_ms, t.stable_test_id,
		       coalesce(t.failure_category, ''), t.failure_observed_at,
		       CASE WHEN t.final_status IN ('failed', 'timed_out', 'interrupted') THEN 0 ELSE 1 END AS rank
		  FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE r.project_id = $1 AND r.id = $2
		   AND ($3::int IS NULL OR (CASE WHEN t.final_status IN ('failed', 'timed_out', 'interrupted') THEN 0 ELSE 1 END, t.display_name, t.id) > ($3, $4::text, $5::uuid))
		 ORDER BY rank, t.display_name, t.id
		 LIMIT $6`, projectID, runID, afterRank, afterName, afterID, limit+1)
	if err != nil {
		return Page[Result]{}, err
	}
	defer rows.Close()
	var items []Result
	var lastRank int
	for rows.Next() {
		var r Result
		var observed *time.Time
		if err := rows.Scan(&r.ID, &r.DisplayName, &r.FilePath, &r.FinalStatus, &r.AttemptCount, &r.DurationMs, &r.StableTestID, &r.FailureCategory, &observed, &lastRank); err != nil {
			return Page[Result]{}, err
		}
		r.FailureObservedAt = utc(observed)
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return Page[Result]{}, err
	}
	if len(items) == 0 {
		// An empty page is only an answer when the run is this project's; a
		// foreign or unknown run is 404, never "no results" (§11.3.3).
		var one int
		if err := s.db.Q(ctx).QueryRow(ctx, `SELECT 1 FROM runs WHERE project_id = $1 AND id = $2`, projectID, runID).Scan(&one); err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return Page[Result]{}, ErrNotFound
			}
			return Page[Result]{}, err
		}
	}
	page := Page[Result]{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		rank := 1
		if last.FinalStatus == "failed" || last.FinalStatus == "timed_out" || last.FinalStatus == "interrupted" {
			rank = 0
		}
		page.NextCursor = encodeCursor(cursor{Kind: "results", Project: projectID, Filter: fh, Rank: rank, Str: last.DisplayName, ID: last.ID})
	}
	if page.Items == nil {
		page.Items = []Result{}
	}
	return page, nil
}

func (s *service) ResultDetail(ctx context.Context, projectID, resultID string) (ResultDetail, error) {
	if !isUUID(projectID) || !isUUID(resultID) {
		return ResultDetail{}, ErrNotFound
	}
	var d ResultDetail
	var observed *time.Time
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT t.id, t.run_id, t.display_name, coalesce(t.identity_raw->>'filePath', ''), t.final_status, t.attempt_count, t.duration_ms, t.stable_test_id,
		       coalesce(t.failure_category, ''), t.failure_observed_at
		  FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE r.project_id = $1 AND t.id = $2`, projectID, resultID,
	).Scan(&d.Result.ID, &d.RunID, &d.Result.DisplayName, &d.Result.FilePath, &d.Result.FinalStatus, &d.Result.AttemptCount, &d.Result.DurationMs, &d.Result.StableTestID,
		&d.Result.FailureCategory, &observed)
	if err != nil {
		return ResultDetail{}, mapErr(err)
	}
	d.Result.FailureObservedAt = utc(observed)

	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, attempt, status, duration_ms, error->>'message', error->>'stack'
		  FROM test_result_attempts WHERE test_result_id = $1 ORDER BY attempt LIMIT 20`, resultID)
	if err != nil {
		return ResultDetail{}, err
	}
	d.Attempts = []Attempt{}
	attemptIDs := []string{}
	for rows.Next() {
		var a Attempt
		var id string
		var msg, stack *string
		if err := rows.Scan(&id, &a.Attempt, &a.Status, &a.DurationMs, &msg, &stack); err != nil {
			rows.Close()
			return ResultDetail{}, err
		}
		if msg != nil || stack != nil {
			a.Error = &AttemptError{Message: msg, Stack: stack}
		}
		a.Artifacts = []ArtifactRef{}
		d.Attempts = append(d.Attempts, a)
		attemptIDs = append(attemptIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return ResultDetail{}, err
	}
	// Verified artifacts of those attempts, in one query; the project
	// condition is on the artifact row too (§11.3.3), not inherited.
	arts, err := s.db.Q(ctx).Query(ctx, `
		SELECT test_result_attempt_id, id, name, kind, content_type, coalesce(actual_bytes, declared_bytes), expires_at
		  FROM artifacts
		 WHERE project_id = $1 AND test_result_attempt_id = ANY($2::uuid[]) AND status = 'verified'
		 ORDER BY created_at, id`, projectID, attemptIDs)
	if err != nil {
		return ResultDetail{}, err
	}
	defer arts.Close()
	index := map[string]int{}
	for i, id := range attemptIDs {
		index[id] = i
	}
	for arts.Next() {
		var attemptID string
		var ref ArtifactRef
		if err := arts.Scan(&attemptID, &ref.ID, &ref.Name, &ref.Kind, &ref.ContentType, &ref.SizeBytes, &ref.ExpiresAt); err != nil {
			return ResultDetail{}, err
		}
		ref.ExpiresAt = ref.ExpiresAt.UTC()
		if i, ok := index[attemptID]; ok && len(d.Attempts[i].Artifacts) < 20 {
			d.Attempts[i].Artifacts = append(d.Attempts[i].Artifacts, ref)
		}
	}
	if err := arts.Err(); err != nil {
		return ResultDetail{}, err
	}

	// actorName: the user's email for USER events, the actor type otherwise.
	rows, err = s.db.Q(ctx).Query(ctx, `
		SELECT e.id, e.type, CASE e.actor_type WHEN 'USER' THEN coalesce(u.email, 'user') WHEN 'AI' THEN 'ai' ELSE 'system' END, e.occurred_at
		  FROM triage_events e
		  LEFT JOIN users u ON e.actor_type = 'USER' AND e.actor_id ~ '^[0-9a-f-]{36}$' AND u.id::text = e.actor_id
		 WHERE e.test_result_id = $1 AND e.project_id = $2
		 ORDER BY e.occurred_at, e.created_at LIMIT 100`, resultID, projectID)
	if err != nil {
		return ResultDetail{}, err
	}
	defer rows.Close()
	d.Events = []TriageEvent{}
	for rows.Next() {
		var ev TriageEvent
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.ActorName, &ev.OccurredAt); err != nil {
			return ResultDetail{}, err
		}
		ev.OccurredAt = ev.OccurredAt.UTC()
		d.Events = append(d.Events, ev)
	}
	return d, rows.Err()
}

// ---- Identities -----------------------------------------------------------

// identitySelect joins each active identity with its worst flaky signal
// across environments and its last 30 outcomes.
const identitySelect = `
	SELECT i.id, i.canonical_leaf_title, i.canonical_file_path,
	       coalesce(f.verdict, 'insufficient_data'), f.flaky_score, coalesce(f.total_runs, 0),
	       i.status, i.merged_into_id,
	       coalesce((SELECT array_agg(t.final_status ORDER BY t.created_at DESC)
	                   FROM (SELECT final_status, created_at FROM test_results WHERE stable_test_id = i.id ORDER BY created_at DESC LIMIT 30) t), '{}'),
	       coalesce(-f.flaky_score, 1) AS neg
	  FROM test_identities i
	  LEFT JOIN LATERAL (
	       SELECT verdict, flaky_score, total_runs FROM flaky_signals s
	        WHERE s.identity_id = i.id ORDER BY s.flaky_score DESC NULLS LAST, s.total_runs DESC LIMIT 1) f ON true`

func scanIdentity(row interface{ Scan(dest ...any) error }) (Identity, float64, error) {
	var it Identity
	var neg float64
	if err := row.Scan(&it.ID, &it.CanonicalTitle, &it.CanonicalFilePath, &it.Verdict, &it.FlakyScore, &it.TotalRuns, &it.Status, &it.MergedIntoID, &it.RecentStatuses, &neg); err != nil {
		return Identity{}, 0, err
	}
	if it.RecentStatuses == nil {
		it.RecentStatuses = []string{}
	}
	return it, neg, nil
}

func (s *service) Identities(ctx context.Context, projectID, view string, limit int, cur string) (Page[Identity], error) {
	if !isUUID(projectID) {
		return Page[Identity]{}, ErrNotFound
	}
	if view != "" && view != "flaky" && view != "all" {
		return Page[Identity]{}, fmt.Errorf("%w: view %q", ErrInvalid, view)
	}
	limit = clamp(limit, 20)
	fh := filterHash("identities", view)
	var afterNeg *float64
	var afterID *string
	if cur != "" {
		c, err := decodeCursor(cur, "identities", projectID, fh)
		if err != nil {
			return Page[Identity]{}, err
		}
		afterNeg, afterID = &c.Num, &c.ID
	}
	rows, err := s.db.Q(ctx).Query(ctx, identitySelect+`
		 WHERE i.project_id = $1 AND i.status = 'active'
		   AND ($2::bool = false OR coalesce(f.verdict, '') IN ('suspect', 'flaky', 'quarantine_suggested'))
		   AND ($3::float8 IS NULL OR (coalesce(-f.flaky_score, 1), i.id) > ($3, $4::uuid))
		 ORDER BY neg, i.id
		 LIMIT $5`, projectID, view == "flaky", afterNeg, afterID, limit+1)
	if err != nil {
		return Page[Identity]{}, err
	}
	defer rows.Close()
	var items []Identity
	var lastNeg float64
	for rows.Next() {
		it, neg, err := scanIdentity(rows)
		if err != nil {
			return Page[Identity]{}, err
		}
		items = append(items, it)
		if len(items) == limit {
			lastNeg = neg
		}
	}
	if err := rows.Err(); err != nil {
		return Page[Identity]{}, err
	}
	page := Page[Identity]{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		page.NextCursor = encodeCursor(cursor{Kind: "identities", Project: projectID, Filter: fh, Num: lastNeg, ID: page.Items[limit-1].ID})
	}
	if page.Items == nil {
		page.Items = []Identity{}
	}
	return page, nil
}

func (s *service) Identity(ctx context.Context, projectID, identityID string) (Identity, error) {
	if !isUUID(projectID) || !isUUID(identityID) {
		return Identity{}, ErrNotFound
	}
	it, _, err := scanIdentity(s.db.Q(ctx).QueryRow(ctx, identitySelect+` WHERE i.project_id = $1 AND i.id = $2`, projectID, identityID))
	return it, mapErr(err)
}

// IdentityRuns — runs in which this identity has a result, newest first;
// index test_results_stable_created (§8.9).
func (s *service) IdentityRuns(ctx context.Context, projectID, identityID string, limit int, cur string) (Page[Run], error) {
	if !isUUID(projectID) || !isUUID(identityID) {
		return Page[Run]{}, ErrNotFound
	}
	if _, err := s.Identity(ctx, projectID, identityID); err != nil {
		return Page[Run]{}, err
	}
	limit = clamp(limit, 20)
	fh := filterHash("identity-runs", identityID)
	var afterTime *time.Time
	var afterID *string
	if cur != "" {
		c, err := decodeCursor(cur, "identity-runs", projectID, fh)
		if err != nil {
			return Page[Run]{}, err
		}
		t, err := time.Parse(time.RFC3339Nano, c.Time)
		if err != nil {
			return Page[Run]{}, ErrBadCursor
		}
		afterTime, afterID = &t, &c.ID
	}
	rows, err := s.db.Q(ctx).Query(ctx, runSelect+`
		 WHERE r.project_id = $1
		   AND EXISTS (SELECT 1 FROM test_results t WHERE t.run_id = r.id AND t.stable_test_id = $2)
		   AND ($3::timestamptz IS NULL OR (r.started_at, r.id) < ($3, $4::uuid))
		 ORDER BY r.started_at DESC, r.id DESC
		 LIMIT $5`, projectID, identityID, afterTime, afterID, limit+1)
	if err != nil {
		return Page[Run]{}, err
	}
	defer rows.Close()
	var items []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return Page[Run]{}, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return Page[Run]{}, err
	}
	page := Page[Run]{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[limit-1]
		page.NextCursor = encodeCursor(cursor{Kind: "identity-runs", Project: projectID, Filter: fh, Time: last.StartedAt.Format(time.RFC3339Nano), ID: last.ID})
	}
	if page.Items == nil {
		page.Items = []Run{}
	}
	return page, nil
}

// ---- Metrics (admin CLI / future dashboard) --------------------------------

func (s *service) CategoryTrend(ctx context.Context, projectID string, since time.Time) ([]TrendBucket, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		-- Weeks start Monday 00:00 UTC whatever the session timezone: the bucket
		-- is part of the contract, not of the connection's TimeZone setting.
		SELECT date_trunc('week', coalesce(t.first_failed_at, t.failure_observed_at) AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS week, coalesce(t.failure_category, 'unknown'), count(*)
		  FROM test_results t JOIN runs r ON r.id = t.run_id
		 WHERE r.project_id = $1 AND t.failure_observed_at >= $2
		 GROUP BY 1, 2 ORDER BY 1, 2`, projectID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrendBucket
	for rows.Next() {
		var b TrendBucket
		if err := rows.Scan(&b.Week, &b.Category, &b.Count); err != nil {
			return nil, err
		}
		b.Week = b.Week.UTC()
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *service) Triage(ctx context.Context, projectID string, since time.Time) (TriageMetrics, error) {
	if !isUUID(projectID) {
		return TriageMetrics{}, ErrNotFound
	}
	var m TriageMetrics
	var ttrc, first *float64
	err := s.db.Q(ctx).QueryRow(ctx, `
		-- One pass over the window's failures and one over their triage
		-- events: per-failure subselects were 2 index probes × every failure
		-- (1.5 s at 100k failures, test/load); the grouped join is one scan.
		WITH failures AS (
			SELECT t.id, t.failure_observed_at
			  FROM test_results t JOIN runs r ON r.id = t.run_id
			 WHERE r.project_id = $1 AND t.failure_observed_at >= $2
		), firsts AS (
			SELECT e.test_result_id,
			       min(e.occurred_at) FILTER (WHERE e.type = 'HYPOTHESIS_ACCEPTED') AS accepted_at,
			       min(e.occurred_at) FILTER (WHERE e.type = 'FAILURE_SEEN') AS seen_at
			  FROM triage_events e JOIN failures f ON f.id = e.test_result_id
			 GROUP BY e.test_result_id
		)
		SELECT count(*),
		       count(*) FILTER (WHERE accepted_at IS NOT NULL),
		       count(*) FILTER (WHERE seen_at IS NOT NULL),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM accepted_at - failure_observed_at)) FILTER (WHERE accepted_at IS NOT NULL),
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY extract(epoch FROM seen_at - failure_observed_at)) FILTER (WHERE seen_at IS NOT NULL)
		  FROM failures f LEFT JOIN firsts x ON x.test_result_id = f.id`, projectID, since.UTC()).Scan(&m.Failures, &m.Triaged, &m.Seen, &ttrc, &first)
	if err != nil {
		return TriageMetrics{}, err
	}
	if m.Failures > 0 {
		m.TriageCoverage = float64(m.Triaged) / float64(m.Failures)
		m.UntriagedRate = 1 - m.TriageCoverage
	}
	if ttrc != nil {
		m.TTRCMedian, m.HasTTRC = time.Duration(*ttrc*float64(time.Second)), true
	}
	if first != nil {
		m.TimeToFirstView, m.HasFirst = time.Duration(*first*float64(time.Second)), true
	}
	return m, nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pg.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
