package testmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// --------------------------------------------------------------- releases

const releaseColumns = `id, project_id, version, branch, status, decided_by, decided_at, decision_note, last_gate, evaluated_at, created_at, updated_at`

func scanRelease(row interface{ Scan(dest ...any) error }) (Release, error) {
	var r Release
	var by *string
	var decidedAt, evaluatedAt *time.Time
	var gate []byte
	if err := row.Scan(&r.ID, &r.ProjectID, &r.Version, &r.Branch, &r.Status, &by, &decidedAt, &r.DecisionNote, &gate, &evaluatedAt, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return Release{}, err
	}
	r.DecidedBy = deref(by)
	if decidedAt != nil {
		r.DecidedAt = decidedAt.UTC()
	}
	if evaluatedAt != nil {
		r.EvaluatedAt = evaluatedAt.UTC()
	}
	if len(gate) > 0 {
		var g GateResult
		if json.Unmarshal(gate, &g) == nil {
			r.Gate = &g
		}
	}
	return r, nil
}

func (s *service) CreateRelease(ctx context.Context, projectID string, in NewRelease) (Release, error) {
	if !isUUID(projectID) {
		return Release{}, ErrNotFound
	}
	in.Version = strings.TrimSpace(in.Version)
	in.Branch = strings.TrimSpace(in.Branch)
	if in.Version == "" || len(in.Version) > 100 {
		return Release{}, fmt.Errorf("%w: version", ErrInvalid)
	}
	if len(in.Branch) > 200 {
		return Release{}, fmt.Errorf("%w: branch", ErrInvalid)
	}
	r, err := scanRelease(s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO releases (project_id, version, branch) VALUES ($1, $2, $3) RETURNING `+releaseColumns, projectID, in.Version, in.Branch))
	return r, mapErr(err)
}

func (s *service) ListReleases(ctx context.Context, projectID string) ([]Release, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+releaseColumns+` FROM releases WHERE project_id = $1 ORDER BY created_at DESC LIMIT 200`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Release{}
	for rows.Next() {
		r, err := scanRelease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *service) GetRelease(ctx context.Context, projectID, id string) (Release, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Release{}, ErrNotFound
	}
	r, err := scanRelease(s.db.Q(ctx).QueryRow(ctx, `SELECT `+releaseColumns+` FROM releases WHERE id = $1 AND project_id = $2`, id, projectID))
	return r, mapErr(err)
}

func (s *service) Decide(ctx context.Context, projectID, id string, d Decision, actor string) (Release, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Release{}, ErrNotFound
	}
	switch d.Status {
	case ReleaseBlocked, ReleaseApproved, ReleaseShipped:
	default:
		return Release{}, fmt.Errorf("%w: status %q (blocked|approved|shipped)", ErrInvalid, d.Status)
	}
	if actor == "" {
		return Release{}, fmt.Errorf("%w: actor required", ErrInvalid)
	}
	if len(d.Note) > 4000 {
		return Release{}, fmt.Errorf("%w: note too long", ErrInvalid)
	}
	var out Release
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		cur, err := scanRelease(s.db.Q(ctx).QueryRow(ctx, `SELECT `+releaseColumns+` FROM releases WHERE id = $1 AND project_id = $2 FOR UPDATE`, id, projectID))
		if err != nil {
			return mapErr(err)
		}
		if cur.Status == ReleaseShipped {
			return fmt.Errorf("%w: release already shipped", ErrConflict)
		}
		// §6.8.4 #1: the gate may say blocked and a named person may still
		// approve — that is the override, and the name goes on the row.
		out, err = scanRelease(s.db.Q(ctx).QueryRow(ctx, `
			UPDATE releases SET status = $3, decided_by = $4, decided_at = $5, decision_note = $6, updated_at = now()
			 WHERE id = $1 AND project_id = $2 RETURNING `+releaseColumns,
			id, projectID, string(d.Status), actor, s.now().UTC(), strings.TrimSpace(d.Note)))
		return mapErr(err)
	})
	if err != nil {
		return Release{}, err
	}
	return out, nil
}

func (s *service) ProjectOfRelease(ctx context.Context, releaseID string) (string, error) {
	if !isUUID(releaseID) {
		return "", ErrNotFound
	}
	var project string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT project_id FROM releases WHERE id = $1`, releaseID).Scan(&project)
	return project, mapErr(err)
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !purgeUUID.MatchString(projectID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, q := range []string{
			"DELETE FROM specialized_results WHERE project_id = $1",
			"DELETE FROM manual_executions WHERE project_id = $1",
			"DELETE FROM test_plans WHERE project_id = $1",
			"DELETE FROM releases WHERE project_id = $1",
			"DELETE FROM requirement_coverage WHERE project_id = $1",
			"DELETE FROM test_case_automation_links WHERE project_id = $1",
			"DELETE FROM test_cases WHERE project_id = $1",
			"DELETE FROM requirements WHERE project_id = $1",
			"DELETE FROM test_suites WHERE project_id = $1",
			"DELETE FROM ttrc_baselines WHERE project_id = $1",
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, projectID); err != nil {
				return fmt.Errorf("testmgmt: purge project: %w", err)
			}
		}
		return nil
	})
}

var purgeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
