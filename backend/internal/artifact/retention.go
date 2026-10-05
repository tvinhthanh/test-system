package artifact

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A project's retention (§10.4, §11.3.2 "Sửa retention"): the plan's
// number per kind, or a shorter one a maintainer chose. Overrides live in
// artifact_retention and are read inside the INSERT that records an
// artifact, so ingest pays no extra round trip.

// expiresSQL is the expires_at expression for an INSERT: from + the plan's
// days, or the project's override when there is one — never longer than
// the plan, even if the plan shrank after the override was set. The row's
// project_id is $1.
func expiresSQL(from, planDays, kind string) string {
	return from + `::timestamptz + make_interval(days => LEAST(` + planDays + `::int, coalesce(
		(SELECT days FROM artifact_retention WHERE project_id = $1 AND kind = ` + kind + `), ` + planDays + `::int)))`
}

func (s *service) Retention(ctx context.Context, projectID, plan string) ([]Retention, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT kind, days, updated_by, updated_at FROM artifact_retention WHERE project_id = $1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[Kind]Retention{}
	for rows.Next() {
		var r Retention
		if err := rows.Scan(&r.Kind, &r.Days, &r.UpdatedBy, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.UpdatedAt = r.UpdatedAt.UTC()
		set[r.Kind] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	days := planDays(plan)
	out := make([]Retention, 0, len(Kinds))
	for _, k := range Kinds {
		r := Retention{Kind: k, PlanDays: days[k], Days: days[k]}
		if o, ok := set[k]; ok {
			r.Override, r.UpdatedBy, r.UpdatedAt = true, o.UpdatedBy, o.UpdatedAt
			r.Days = min(o.Days, days[k]) // what expiresSQL applies
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *service) SetRetention(ctx context.Context, projectID, plan string, days map[Kind]int, actor string) (map[Kind][2]int, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	if strings.TrimSpace(actor) == "" {
		return nil, fmt.Errorf("%w: actor", ErrInvalid)
	}
	limit := planDays(plan)
	for k, d := range days {
		switch {
		case !isKind(k):
			return nil, fmt.Errorf("%w: kind %q", ErrInvalid, k)
		case d < 0:
			return nil, fmt.Errorf("%w: %s retention must be at least 1 day, or 0 for the plan's", ErrInvalid, k)
		case d > limit[k]:
			return nil, &AbovePlanError{Kind: k, PlanDays: limit[k], Got: d}
		}
	}
	changed := map[Kind][2]int{}
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		before, err := s.Retention(ctx, projectID, plan)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, r := range before {
			d, ok := days[r.Kind]
			if !ok {
				continue
			}
			after := d
			if d == 0 {
				after = r.PlanDays
				_, err = s.db.Q(ctx).Exec(ctx, `DELETE FROM artifact_retention WHERE project_id = $1 AND kind = $2`, projectID, string(r.Kind))
			} else {
				_, err = s.db.Q(ctx).Exec(ctx, `
					INSERT INTO artifact_retention (project_id, kind, days, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (project_id, kind) DO UPDATE SET days = EXCLUDED.days, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at
					 WHERE artifact_retention.days <> EXCLUDED.days`,
					projectID, string(r.Kind), d, strings.TrimSpace(actor), now)
			}
			if err != nil {
				return err
			}
			if after != r.Days {
				changed[r.Kind] = [2]int{r.Days, after}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// isKind is every kind of §10.4, manual_evidence included (ValidKind is
// the narrower set a reporter may declare).
func isKind(k Kind) bool {
	for _, x := range Kinds {
		if x == k {
			return true
		}
	}
	return false
}
