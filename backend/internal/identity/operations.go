package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// Manual operations (§9.4). §9.6 accepts precision ≥ 0.98, so roughly one
// auto-match in fifty is wrong on real data, and a wrong merge silently
// fuses the history of two tests. These are what make an imperfect matcher
// survivable. All three are audited; P1-min drives them from cmd/admin.

// Merge folds src into dst. Every alias of src — in force or historical —
// moves to dst, because unifying the whole history is the point when a
// genuine rename was missed. The moved alias ids and their prior sources
// are recorded in the audit row so UndoMerge can put back exactly those.
func (s *service) Merge(ctx context.Context, projectID, dst, src, actor string) error {
	if !isUUID(projectID) || !isUUID(dst) || !isUUID(src) {
		return ErrNotFound
	}
	if dst == src {
		return fmt.Errorf("%w: cannot merge an identity into itself", ErrInvalid)
	}
	if actor == "" {
		return fmt.Errorf("%w: actor required", ErrInvalid)
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		d, err := s.lockIdentity(ctx, projectID, dst)
		if err != nil {
			return fmt.Errorf("target: %w", err)
		}
		if d.Status != StatusActive {
			return fmt.Errorf("%w: merge target %s is %s", ErrConflict, dst, d.Status)
		}
		sr, err := s.lockIdentity(ctx, projectID, src)
		if err != nil {
			return fmt.Errorf("source: %w", err)
		}
		if sr.Status != StatusActive {
			return fmt.Errorf("%w: merge source %s is %s", ErrConflict, src, sr.Status)
		}

		rows, err := q.Query(ctx, `SELECT id, source FROM test_identity_aliases WHERE identity_id = $1 FOR UPDATE`, src)
		if err != nil {
			return err
		}
		moved := map[string]string{} // alias id → prior source
		for rows.Next() {
			var id, prior string
			if err := rows.Scan(&id, &prior); err != nil {
				rows.Close()
				return err
			}
			moved[id] = prior
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `
			UPDATE test_identity_aliases SET identity_id = $2, source = 'manual_merge', created_by = $3 WHERE identity_id = $1`, src, dst, actor); err != nil {
			return mapErr(err)
		}
		now := s.now().UTC()
		if _, err := q.Exec(ctx, `
			UPDATE test_identities SET status = 'merged', merged_into_id = $2, updated_at = $3 WHERE id = $1`, src, dst, now); err != nil {
			return mapErr(err)
		}
		if _, err := q.Exec(ctx, `
			UPDATE test_identities SET last_seen_at = greatest(last_seen_at, $2), updated_at = $3 WHERE id = $1`, dst, sr.LastSeenAt, now); err != nil {
			return mapErr(err)
		}
		details, _ := json.Marshal(map[string]any{"moved": moved})
		return s.audit(ctx, projectID, "merge", src, dst, actor, details)
	})
}

// UndoMerge is NOT SplitFrom (N43). It restores the aliases the latest
// Merge of src moved — with their prior sources — and reactivates src, so
// every run between the merge and its discovery is attributed correctly
// again. Measured on the prototype: SplitFrom(day 30) after a wrong merge
// on day 10 leaves 29/29 days wrong; this leaves 0/29.
func (s *service) UndoMerge(ctx context.Context, projectID, src, actor string) error {
	if !isUUID(projectID) || !isUUID(src) {
		return ErrNotFound
	}
	if actor == "" {
		return fmt.Errorf("%w: actor required", ErrInvalid)
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		sr, err := s.lockIdentity(ctx, projectID, src)
		if err != nil {
			return err
		}
		if sr.Status != StatusMerged {
			return fmt.Errorf("%w: %s is not merged into anything", ErrConflict, src)
		}
		var details []byte
		var dst string
		if err := q.QueryRow(ctx, `
			SELECT target_id, details FROM identity_audit
			 WHERE project_id = $1 AND action = 'merge' AND subject_id = $2
			 ORDER BY at DESC, id DESC LIMIT 1`, projectID, src).Scan(&dst, &details); err != nil {
			return fmt.Errorf("%w: no merge audit for %s", ErrConflict, src)
		}
		if dst != sr.MergedIntoID {
			return fmt.Errorf("%w: audit says merged into %s, row says %s", ErrConflict, dst, sr.MergedIntoID)
		}
		var rec struct {
			Moved map[string]string `json:"moved"`
		}
		_ = json.Unmarshal(details, &rec)
		// The survivor may itself have been merged since; its aliases then
		// live further down the chain, which this undo does not unwind.
		if _, err := s.lockIdentity(ctx, projectID, dst); err != nil {
			return fmt.Errorf("target: %w", err)
		}
		restored := 0
		for aliasID, prior := range rec.Moved {
			tag, err := q.Exec(ctx, `
				UPDATE test_identity_aliases SET identity_id = $2, source = $3, created_by = $4
				 WHERE id = $1 AND identity_id = $5`, aliasID, src, prior, actor, dst)
			if err != nil {
				return mapErr(err)
			}
			restored += int(tag.RowsAffected())
		}
		if restored != len(rec.Moved) {
			return fmt.Errorf("%w: %d of %d merged aliases have moved on since; undo by hand", ErrConflict, len(rec.Moved)-restored, len(rec.Moved))
		}
		now := s.now().UTC()
		if _, err := q.Exec(ctx, `
			UPDATE test_identities SET status = 'active', merged_into_id = NULL, updated_at = $2 WHERE id = $1`, src, now); err != nil {
			return mapErr(err)
		}
		out, _ := json.Marshal(map[string]any{"restored": restored, "from": dst})
		return s.audit(ctx, projectID, "undo_merge", src, dst, actor, out)
	})
}

// SplitFrom says "this test genuinely diverged from time t": the alias in
// force for fingerprint is closed at t and a new identity owns it from t on.
// History before t stays where it is — that is the point, and also why this
// cannot undo a wrong merge (N43).
func (s *service) SplitFrom(ctx context.Context, projectID, fingerprint string, at time.Time, actor string) (string, error) {
	if !isUUID(projectID) {
		return "", ErrNotFound
	}
	if actor == "" || fingerprint == "" || at.IsZero() {
		return "", fmt.Errorf("%w: fingerprint, time and actor required", ErrInvalid)
	}
	at = at.UTC()
	var newID string
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var aliasID, oldID string
		var validFrom time.Time
		var sigJSON []byte
		err := q.QueryRow(ctx, `
			SELECT id, identity_id, valid_from, signals FROM test_identity_aliases
			 WHERE project_id = $1 AND fingerprint = $2 AND valid_to IS NULL FOR UPDATE`, projectID, fingerprint).Scan(&aliasID, &oldID, &validFrom, &sigJSON)
		if err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return fmt.Errorf("%w: no alias in force for %s", ErrNotFound, fingerprint)
			}
			return err
		}
		if !at.After(validFrom) {
			return fmt.Errorf("%w: split point %s is not after the alias start %s", ErrInvalid, at.Format(time.RFC3339), validFrom.UTC().Format(time.RFC3339))
		}
		old, err := s.lockIdentity(ctx, projectID, oldID)
		if err != nil {
			return err
		}
		// Signals for the new identity: the alias's own if recorded, else
		// the old identity's canonical ones.
		var sig Signals
		if len(sigJSON) > 0 {
			_ = json.Unmarshal(sigJSON, &sig)
		}
		if !validSignals(sig) {
			sig = Signals{Framework: old.Framework, FilePath: old.CanonicalFilePath, TitlePath: old.CanonicalTitlePath, ParamSignature: old.ParamSignature}
		}

		if _, err := q.Exec(ctx, `UPDATE test_identity_aliases SET valid_to = $2 WHERE id = $1`, aliasID, at); err != nil {
			return mapErr(err)
		}
		// The old identity's current fingerprint must not stay on the split
		// one (partial unique index). Move it to another alias in force, or
		// retire the identity when none is left: it keeps its history and
		// stops being a candidate.
		var other *string
		if err := q.QueryRow(ctx, `
			SELECT fingerprint FROM test_identity_aliases WHERE identity_id = $1 AND valid_to IS NULL
			 ORDER BY valid_from DESC LIMIT 1`, oldID).Scan(&other); err != nil && !errors.Is(err, pg.ErrNoRows) {
			return err
		}
		now := s.now().UTC()
		if other != nil {
			_, err = q.Exec(ctx, `UPDATE test_identities SET current_fingerprint = $2, updated_at = $3 WHERE id = $1`, oldID, *other, now)
		} else {
			_, err = q.Exec(ctx, `UPDATE test_identities SET status = 'retired', updated_at = $2 WHERE id = $1`, oldID, now)
		}
		if err != nil {
			return mapErr(err)
		}

		ms := toMatch(sig).Normalize()
		if err := q.QueryRow(ctx, `
			INSERT INTO test_identities (project_id, framework, canonical_file_path, canonical_title_path, canonical_leaf_title,
			                             param_signature, template_key, current_fingerprint, first_seen_at, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
			RETURNING id`,
			projectID, ms.Framework, ms.FilePath, ms.TitlePath, ms.Leaf(), ms.ParamSignature, toMatch(sig).TemplateKey(), fingerprint, at,
		).Scan(&newID); err != nil {
			return mapErr(err)
		}
		if err := s.bind(ctx, projectID, newID, fingerprint, SourceManualSplit, 0, sig, actor, at); err != nil {
			return err
		}
		details, _ := json.Marshal(map[string]any{"fingerprint": fingerprint, "from": at, "closedAlias": aliasID})
		return s.audit(ctx, projectID, "split", oldID, newID, actor, details)
	})
	if err != nil {
		return "", err
	}
	return newID, nil
}

// lockIdentity reads an identity FOR UPDATE, scoped to the project so a
// caller cannot operate on another tenant's row by id.
func (s *service) lockIdentity(ctx context.Context, projectID, id string) (Identity, error) {
	i, err := scanIdentity(s.db.Q(ctx).QueryRow(ctx,
		`SELECT `+identityColumns+` FROM test_identities WHERE id = $1 AND project_id = $2 FOR UPDATE`, id, projectID))
	return i, mapErr(err)
}

func (s *service) audit(ctx context.Context, projectID, action, subject, target, actor string, details []byte) error {
	if details == nil {
		details = []byte(`{}`)
	}
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO identity_audit (project_id, action, subject_id, target_id, actor, details, at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, projectID, action, subject, nullable(target), actor, details, s.now().UTC())
	return mapErr(err)
}

// ---- Review Queue ----------------------------------------------------------

func (s *service) PendingReviews(ctx context.Context) (int, error) {
	var n int
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM review_queue_items WHERE status = 'pending'`).Scan(&n)
	return n, err
}

func (s *service) ListReview(ctx context.Context, projectID string, limit int) ([]ReviewItem, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, project_id, identity_id, candidate_id, fingerprint, score, runner_up_id, runner_up_score, gap, breakdown,
		       run_id, status, coalesce(decided_by, ''), decided_at, created_at
		  FROM review_queue_items WHERE project_id = $1 AND status = 'pending' ORDER BY created_at LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReviewItem
	for rows.Next() {
		var it ReviewItem
		var runner, run *string
		var runnerScore *float64
		var breakdown []byte
		var decidedAt *time.Time
		if err := rows.Scan(&it.ID, &it.ProjectID, &it.IdentityID, &it.CandidateID, &it.Fingerprint, &it.Score, &runner, &runnerScore, &it.Gap, &breakdown,
			&run, &it.Status, &it.DecidedBy, &decidedAt, &it.CreatedAt); err != nil {
			return nil, err
		}
		if runner != nil {
			it.RunnerUpID = *runner
		}
		if runnerScore != nil {
			it.RunnerUpScore = *runnerScore
		}
		if run != nil {
			it.RunID = *run
		}
		if decidedAt != nil {
			it.DecidedAt = decidedAt.UTC()
		}
		it.CreatedAt = it.CreatedAt.UTC()
		_ = json.Unmarshal(breakdown, &it.Breakdown)
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s *service) DecideReview(ctx context.Context, itemID string, decision ReviewStatus, actor string) error {
	if !isUUID(itemID) {
		return ErrNotFound
	}
	if decision != ReviewMerged && decision != ReviewKept {
		return fmt.Errorf("%w: decision must be merged or kept", ErrInvalid)
	}
	if actor == "" {
		return fmt.Errorf("%w: actor required", ErrInvalid)
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var projectID, identityID, candidateID string
		var status ReviewStatus
		if err := q.QueryRow(ctx, `
			SELECT project_id, identity_id, candidate_id, status FROM review_queue_items WHERE id = $1 FOR UPDATE`, itemID,
		).Scan(&projectID, &identityID, &candidateID, &status); err != nil {
			return mapErr(err)
		}
		if status != ReviewPending {
			return fmt.Errorf("%w: item already %s", ErrConflict, status)
		}
		if decision == ReviewMerged {
			// The reviewer confirmed the rename: the identity created at
			// resolve time folds into the candidate it was compared against.
			if err := s.Merge(ctx, projectID, candidateID, identityID, actor); err != nil {
				return err
			}
		}
		now := s.now().UTC()
		if _, err := q.Exec(ctx, `
			UPDATE review_queue_items SET status = $2, decided_by = $3, decided_at = $4 WHERE id = $1`, itemID, string(decision), actor, now); err != nil {
			return mapErr(err)
		}
		details, _ := json.Marshal(map[string]any{"item": itemID, "decision": decision, "candidate": candidateID})
		return s.audit(ctx, projectID, "review", identityID, candidateID, actor, details)
	})
}
