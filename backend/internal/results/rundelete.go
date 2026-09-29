package results

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// ---- Deleting a run (§11.3.2 "Xóa dữ liệu run/artifact") -------------------
//
// Soft, like a project (migration 0026): from DeleteRun on every read of
// the run or its results skips it — the dashboard, flaky history, trends,
// ROI, the release gate, the meter — until RestoreRun, or until the worker
// calls PurgeRun after the restore window. The stable ids that come back
// are the tests whose flaky signals must be rebuilt without (or with) it.
// A run still receiving events cannot be deleted: finish it first.

// ErrRunOpen: the run is still open; it can be deleted once finished.
var ErrRunOpen = errors.New("results: run still open")

func (s *service) stableIDsOf(ctx context.Context, run RunID) ([]string, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT DISTINCT stable_test_id FROM test_results WHERE run_id = $1 AND stable_test_id IS NOT NULL ORDER BY 1`, string(run))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

const deletedRunColumns = runColumns + `, deleted_at, deleted_by, purge_after`

func scanDeletedRun(row interface{ Scan(dest ...any) error }) (DeletedRun, error) {
	var d DeletedRun
	var errorReason, commit, branch *string
	var finished *time.Time
	err := row.Scan(&d.Run.ID, &d.Run.ProjectID, &d.Run.EnvironmentID, &d.Run.LocalRunID, &d.Run.Status, &d.Run.FlakyCount, &errorReason,
		&d.Run.Trigger, &commit, &branch, &d.Run.Framework, &d.Run.Reporter, &d.Run.StartedAt, &finished, &d.Run.CreatedAt,
		&d.DeletedAt, &d.DeletedBy, &d.PurgeAfter)
	if err != nil {
		return DeletedRun{}, err
	}
	d.Run.ErrorReason, d.Run.CommitSHA, d.Run.Branch = deref(errorReason), deref(commit), deref(branch)
	d.Run.StartedAt, d.Run.CreatedAt = d.Run.StartedAt.UTC(), d.Run.CreatedAt.UTC()
	if finished != nil {
		d.Run.FinishedAt = finished.UTC()
	}
	d.DeletedAt, d.PurgeAfter = d.DeletedAt.UTC(), d.PurgeAfter.UTC()
	return d, nil
}

func (s *service) DeleteRun(ctx context.Context, projectID string, id RunID, actor string, keep time.Duration) (DeletedRun, error) {
	if !isUUID(projectID) || !isUUID(string(id)) {
		return DeletedRun{}, ErrNotFound
	}
	if strings.TrimSpace(actor) == "" || keep < time.Hour {
		return DeletedRun{}, fmt.Errorf("%w: actor and a restore window of at least an hour", ErrInvalid)
	}
	var out DeletedRun
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		var finished *time.Time
		err := s.db.Q(ctx).QueryRow(ctx, `SELECT finished_at FROM runs WHERE id = $1 AND project_id = $2 AND deleted_at IS NULL FOR UPDATE`, string(id), projectID).Scan(&finished)
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if finished == nil {
			return ErrRunOpen
		}
		now := time.Now().UTC()
		out, err = scanDeletedRun(s.db.Q(ctx).QueryRow(ctx, `
			UPDATE runs SET deleted_at = $2, deleted_by = $3, purge_after = $4 WHERE id = $1
			RETURNING `+deletedRunColumns, string(id), now, strings.TrimSpace(actor), now.Add(keep)))
		if err != nil {
			return err
		}
		out.StableTestIDs, err = s.stableIDsOf(ctx, id)
		return err
	})
	return out, err
}

func (s *service) RestoreRun(ctx context.Context, projectID string, id RunID) (DeletedRun, error) {
	if !isUUID(projectID) || !isUUID(string(id)) {
		return DeletedRun{}, ErrNotFound
	}
	var out DeletedRun
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		var err error
		out, err = scanDeletedRun(s.db.Q(ctx).QueryRow(ctx, `
			SELECT `+deletedRunColumns+` FROM runs WHERE id = $1 AND project_id = $2 AND deleted_at IS NOT NULL FOR UPDATE`, string(id), projectID))
		if errors.Is(err, pg.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE runs SET deleted_at = NULL, deleted_by = NULL, purge_after = NULL WHERE id = $1`, string(id)); err != nil {
			return err
		}
		out.StableTestIDs, err = s.stableIDsOf(ctx, id)
		return err
	})
	return out, err
}

func (s *service) DeletedRuns(ctx context.Context, projectID string) ([]DeletedRun, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	return s.listDeletedRuns(ctx, `project_id = $1`, projectID, 200)
}

func (s *service) RunsToPurge(ctx context.Context, now time.Time, limit int) ([]DeletedRun, error) {
	return s.listDeletedRuns(ctx, `purge_after <= $1`, now.UTC(), limit)
}

func (s *service) listDeletedRuns(ctx context.Context, where string, arg any, limit int) ([]DeletedRun, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+deletedRunColumns+` FROM runs
		 WHERE deleted_at IS NOT NULL AND `+where+`
		 ORDER BY deleted_at DESC LIMIT `+fmt.Sprint(limit), arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeletedRun{}
	for rows.Next() {
		d, err := scanDeletedRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *service) PurgeRun(ctx context.Context, id RunID) error {
	if !isUUID(string(id)) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		var deleted bool
		err := s.db.Q(ctx).QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM runs WHERE id = $1 FOR UPDATE`, string(id)).Scan(&deleted)
		if errors.Is(err, pg.ErrNoRows) {
			return nil // purged already
		}
		if err != nil {
			return err
		}
		if !deleted {
			return fmt.Errorf("%w: run %s is not deleted", ErrInvalid, id)
		}
		for _, q := range []string{
			`DELETE FROM test_result_attempts WHERE test_result_id IN (SELECT id FROM test_results WHERE run_id = $1)`,
			`DELETE FROM test_results WHERE run_id = $1`,
			`DELETE FROM run_events WHERE run_id = $1`,
			`DELETE FROM runs WHERE id = $1`,
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, string(id)); err != nil {
				return fmt.Errorf("results: purge run: %w", err)
			}
		}
		return nil
	})
}
