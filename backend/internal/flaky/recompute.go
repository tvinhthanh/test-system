package flaky

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

// RebuildAfter is how stale a signal may get before a sweep rebuilds it
// exactly from the window, dropping results that fell out of the 30 days.
// Between rebuilds a finish only adds its own results (§8.6: incremental).
const RebuildAfter = 24 * time.Hour

// RecomputeRun is §8.6 at run finish, incremental: each identity of the
// run takes the run's results as one more step of the fold — no history
// read for an identity that already has a signal. Identities with no
// signal yet (a new test, or one never seen in this environment) are built
// from their history, which is short by definition. Runs may finish out of
// their start order; the fold then differs from the exact windowed one by
// at most the flips at that boundary, and the sweep rebuilds it within
// RebuildAfter.
func (s *service) RecomputeRun(ctx context.Context, projectID, runID string) (int, error) {
	if !isUUID(projectID) || !isUUID(runID) {
		return 0, ErrNotFound
	}
	run, err := s.results.GetRunByID(ctx, results.RunID(runID))
	if err != nil {
		if errors.Is(err, results.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	if run.ProjectID != projectID {
		return 0, ErrNotFound
	}
	switch run.Status {
	case results.RunPassed, results.RunFailed, results.RunError:
	default:
		return 0, nil // a cancelled or still-open run is not evidence (same filter as History)
	}
	list, err := s.results.ListResults(ctx, run.ID)
	if err != nil {
		return 0, err
	}
	// The run's results per identity, in occurrence order — the same
	// order History puts them in — and the identities in id order (the
	// transaction lock order shared with the resolver, see ResolveDeferred).
	points := map[string][]results.HistoryPoint{}
	for _, tr := range list {
		if tr.StableTestID == "" {
			continue
		}
		points[tr.StableTestID] = append(points[tr.StableTestID], results.HistoryPoint{RunID: run.ID, StartedAt: run.StartedAt, FinalStatus: tr.FinalStatus, ObservedAt: tr.FailureObservedAt, Occurrence: tr.OccurrenceIndex})
	}
	ids := make([]string, 0, len(points))
	for id, ps := range points {
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].Occurrence < ps[j].Occurrence })
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return 0, nil
	}

	now := s.now().UTC()
	existing, err := s.foldStates(ctx, ids, run.EnvironmentID)
	if err != nil {
		return 0, err
	}
	var fresh []string
	for _, id := range ids {
		if _, ok := existing[id]; !ok {
			fresh = append(fresh, id)
		}
	}
	// New identities: exact from history (which already includes this run).
	histories := map[string][]results.HistoryPoint{}
	if len(fresh) > 0 {
		histories, err = s.results.HistoryMany(ctx, fresh, run.EnvironmentID, now.Add(-WindowDays*24*time.Hour))
		if err != nil {
			return 0, err
		}
	}
	states := make([]Stats, len(ids))
	rebuilt := make([]bool, len(ids))
	for i, id := range ids {
		if st, ok := existing[id]; ok {
			for _, p := range points[id] {
				st.Apply(p)
			}
			states[i] = st.Finalize(now)
			continue
		}
		states[i] = Compute(histories[id], now)
		rebuilt[i] = true
	}
	if _, err := s.upsert(ctx, projectID, run.EnvironmentID, ids, states, rebuilt, now); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// Recompute rebuilds one signal exactly from its window.
func (s *service) Recompute(ctx context.Context, projectID, identityID, environmentID string) (Signal, error) {
	if !isUUID(projectID) || !isUUID(identityID) || !isUUID(environmentID) {
		return Signal{}, ErrNotFound
	}
	sigs, err := s.rebuild(ctx, projectID, environmentID, []string{identityID})
	if err != nil {
		return Signal{}, err
	}
	return sigs[0], nil
}

// Sweep rebuilds, exactly from the window, up to limit signals not rebuilt
// for olderThan — the other half of "incremental": results older than the
// window leave the counts here. Returns how many it rebuilt.
func (s *service) Sweep(ctx context.Context, olderThan time.Duration, limit int) (int, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT project_id, environment_id, identity_id FROM flaky_signals
		 WHERE rebuilt_at < $1 ORDER BY rebuilt_at LIMIT $2`, s.now().UTC().Add(-olderThan), limit)
	if err != nil {
		return 0, err
	}
	type scope struct{ project, env string }
	groups := map[scope][]string{}
	var order []scope
	for rows.Next() {
		var sc scope
		var id string
		if err := rows.Scan(&sc.project, &sc.env, &id); err != nil {
			rows.Close()
			return 0, err
		}
		if _, ok := groups[sc]; !ok {
			order = append(order, sc)
		}
		groups[sc] = append(groups[sc], id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, sc := range order {
		ids := groups[sc]
		sort.Strings(ids)
		if _, err := s.rebuild(ctx, sc.project, sc.env, ids); err != nil {
			return n, fmt.Errorf("rebuild %d signals of project %s: %w", len(ids), sc.project, err)
		}
		n += len(ids)
	}
	return n, nil
}

// rebuild is the exact path: one history read for the ids, Compute, one
// upsert marking them rebuilt. ids must be sorted.
func (s *service) rebuild(ctx context.Context, projectID, environmentID string, ids []string) ([]Signal, error) {
	now := s.now().UTC()
	histories, err := s.results.HistoryMany(ctx, ids, environmentID, now.Add(-WindowDays*24*time.Hour))
	if err != nil {
		return nil, err
	}
	states := make([]Stats, len(ids))
	rebuilt := make([]bool, len(ids))
	for i, id := range ids {
		states[i] = Compute(histories[id], now)
		rebuilt[i] = true
	}
	return s.upsert(ctx, projectID, environmentID, ids, states, rebuilt, now)
}

// foldStates loads the fold state of the identities that have a signal in
// the environment, keyed by identity id.
func (s *service) foldStates(ctx context.Context, ids []string, environmentID string) (map[string]Stats, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT identity_id, total_runs, failed_runs, flaky_pass_runs, flip_count, last_failed_at, last_outcome
		  FROM flaky_signals WHERE environment_id = $1 AND identity_id = ANY($2::uuid[])`, environmentID, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Stats{}
	for rows.Next() {
		var id string
		var st Stats
		var lastFailed *time.Time
		if err := rows.Scan(&id, &st.TotalRuns, &st.FailedRuns, &st.FlakyPassRuns, &st.FlipCount, &lastFailed, &st.LastOutcome); err != nil {
			return nil, err
		}
		if lastFailed != nil {
			st.LastFailedAt = lastFailed.UTC()
		}
		out[id] = st
	}
	return out, rows.Err()
}

// upsert writes finalized states for ids in one statement. rebuilt marks
// the rows whose counts came from the whole window (rebuilt_at moves to
// now); a delta carries the epoch, which greatest() turns into "keep" on
// the existing row — and which, should a delta ever land on a row that
// does not exist, makes the sweep rebuild it on its next pass.
func (s *service) upsert(ctx context.Context, projectID, environmentID string, ids []string, states []Stats, rebuilt []bool, now time.Time) ([]Signal, error) {
	n := len(ids)
	totals, faileds, flakies, flips, lastOutcomes := make([]int32, n), make([]int32, n), make([]int32, n), make([]int32, n), make([]int16, n)
	scores, verdicts, lastFaileds := make([]*float64, n), make([]string, n), make([]*time.Time, n)
	for i, st := range states {
		totals[i], faileds[i], flakies[i], flips[i], lastOutcomes[i] = int32(st.TotalRuns), int32(st.FailedRuns), int32(st.FlakyPassRuns), int32(st.FlipCount), int16(st.LastOutcome)
		verdicts[i] = string(st.Verdict)
		if st.HasScore {
			score := st.Score
			scores[i] = &score
		}
		if !st.LastFailedAt.IsZero() {
			t := st.LastFailedAt
			lastFaileds[i] = &t
		}
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		INSERT INTO flaky_signals (project_id, identity_id, environment_id, window_days, total_runs, failed_runs, flaky_pass_runs, flip_count, flaky_score, verdict, last_failed_at, last_outcome, computed_at, rebuilt_at)
		SELECT $1, i::uuid, $2, $3, t, f, fp, fl, sc, v, lf, lo, $4, CASE WHEN rb THEN $4::timestamptz ELSE 'epoch'::timestamptz END
		  FROM unnest($5::text[], $6::int[], $7::int[], $8::int[], $9::int[], $10::float8[], $11::text[], $12::timestamptz[], $13::smallint[], $14::bool[]) AS x(i, t, f, fp, fl, sc, v, lf, lo, rb)
		ON CONFLICT (identity_id, environment_id) DO UPDATE SET
			total_runs = EXCLUDED.total_runs, failed_runs = EXCLUDED.failed_runs, flaky_pass_runs = EXCLUDED.flaky_pass_runs,
			flip_count = EXCLUDED.flip_count, flaky_score = EXCLUDED.flaky_score, verdict = EXCLUDED.verdict,
			last_failed_at = EXCLUDED.last_failed_at, last_outcome = EXCLUDED.last_outcome, computed_at = EXCLUDED.computed_at,
			rebuilt_at = greatest(flaky_signals.rebuilt_at, EXCLUDED.rebuilt_at)
		RETURNING `+columns,
		projectID, environmentID, WindowDays, now, ids, totals, faileds, flakies, flips, scores, verdicts, lastFaileds, lastOutcomes, rebuilt)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	byID := make(map[string]Signal, n)
	for rows.Next() {
		sig, err := scan(rows)
		if err != nil {
			return nil, err
		}
		byID[sig.IdentityID] = sig
	}
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}
	out := make([]Signal, n)
	for i, id := range ids {
		sig, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("flaky: upsert returned no row for identity %s", id)
		}
		out[i] = sig
	}
	return out, nil
}
