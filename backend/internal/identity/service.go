package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity/internal/match"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

type service struct {
	db  *pg.DB
	cfg match.Config
	now func() time.Time
}

// New returns the Service with the §9.3 defaults.
func New(db *pg.DB) Service {
	return &service{db: db, cfg: match.DefaultConfig(), now: time.Now}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

func toMatch(s Signals) match.Signals {
	return match.Signals{Framework: s.Framework, FilePath: s.FilePath, TitlePath: s.TitlePath, ParamSignature: s.ParamSignature}
}

func validSignals(s Signals) bool {
	return s.Framework != "" && s.FilePath != "" && len(s.TitlePath) > 0
}

// lastSeenThrottle bounds the write tier 1 makes on every exact match.
const lastSeenThrottle = 5 * time.Minute

// ---- Tier 1 ---------------------------------------------------------------

func (s *service) ResolveInline(ctx context.Context, projectID string, refs []Ref) (map[string]string, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	out := map[string]string{}
	if len(refs) == 0 {
		return out, nil
	}
	byFP := map[string][]string{}
	fps := make([]string, 0, len(refs))
	for _, r := range refs {
		if !validSignals(r.Signals) {
			continue
		}
		fp := toMatch(r.Signals).Fingerprint()
		if _, seen := byFP[fp]; !seen {
			fps = append(fps, fp)
		}
		byFP[fp] = append(byFP[fp], r.Key)
	}
	hits, err := s.exact(ctx, projectID, fps)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(hits))
	for fp, id := range hits {
		ids = append(ids, id)
		for _, key := range byFP[fp] {
			out[key] = id
		}
	}
	if len(ids) > 0 {
		if _, err := s.db.Q(ctx).Exec(ctx, `
			UPDATE test_identities SET last_seen_at = $2, updated_at = $2
			 WHERE id = ANY($1) AND last_seen_at < $2::timestamptz - $3::interval`,
			ids, s.now().UTC(), fmt.Sprintf("%d seconds", int(lastSeenThrottle.Seconds()))); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// exact is §9.3 step 1: aliases in force, one index probe per fingerprint.
func (s *service) exact(ctx context.Context, projectID string, fps []string) (map[string]string, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT fingerprint, identity_id FROM test_identity_aliases
		 WHERE project_id = $1 AND fingerprint = ANY($2) AND valid_to IS NULL`, projectID, fps)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := map[string]string{}
	for rows.Next() {
		var fp, id string
		if err := rows.Scan(&fp, &id); err != nil {
			return nil, err
		}
		hits[fp] = id
	}
	return hits, rows.Err()
}

// ---- Tier 2 ---------------------------------------------------------------

func (s *service) ResolveDeferred(ctx context.Context, projectID, runID string, refs []Ref, presentIDs []string) (map[string]string, []Outcome, error) {
	if !isUUID(projectID) {
		return nil, nil, ErrNotFound
	}
	if runID != "" && !isUUID(runID) {
		return nil, nil, fmt.Errorf("%w: run id", ErrInvalid)
	}
	present := make(map[string]bool, len(presentIDs))
	for _, id := range presentIDs {
		present[id] = true
	}
	out := map[string]string{}
	var outcomes []Outcome
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		// The candidate filter's `%` operator reads this GUC; §9.3 says 0.4,
		// pg_trgm's default is 0.3. SET LOCAL dies with the transaction.
		if _, err := q.Exec(ctx, `SET LOCAL pg_trgm.similarity_threshold = 0.4`); err != nil {
			return err
		}
		now := s.now().UTC()
		decided := map[string]string{} // fingerprint → identity, within this call
		// Fingerprint order, always. Two workers resolving two runs of the same
		// suite insert the same unique keys inside one transaction each; in
		// random order they wait on each other in a cycle (40P01 deadlock, seen
		// in test/failure), in the same order the second one simply queues
		// behind the first and takes the ON CONFLICT path in create/bind.
		ordered := make([]keyed, 0, len(refs))
		for _, r := range refs {
			if !validSignals(r.Signals) {
				continue
			}
			ms := toMatch(r.Signals)
			ordered = append(ordered, keyed{ref: r, ms: ms, fp: ms.Fingerprint()})
		}
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].fp != ordered[j].fp {
				return ordered[i].fp < ordered[j].fp
			}
			return ordered[i].ref.Key < ordered[j].ref.Key
		})
		// Step 1 for the whole batch in one probe. A fingerprint bound by a
		// concurrent resolve after this read reaches create, whose ON
		// CONFLICT hands back that owner as an exact match.
		fps := make([]string, len(ordered))
		for i, k := range ordered {
			fps[i] = k.fp
		}
		known, err := s.exact(ctx, projectID, fps)
		if err != nil {
			return err
		}
		for _, k := range ordered {
			r, ms, fp := k.ref, k.ms, k.fp
			if id, ok := decided[fp]; ok {
				out[r.Key] = id
				continue
			}
			if id, ok := known[fp]; ok {
				decided[fp], out[r.Key] = id, id
				outcomes = append(outcomes, Outcome{Key: r.Key, IdentityID: id, Decision: DecisionExact, Score: 1})
				continue
			}
			cands, err := s.candidates(ctx, projectID, ms, present)
			if err != nil {
				return err
			}
			v := s.cfg.Decide(ms, cands, now)
			oc := Outcome{Key: r.Key, Score: v.Score, Gap: v.Gap}
			var id string
			created := true
			switch v.Decision {
			case match.DecisionAutoMatch:
				id, created, err = s.autoMatch(ctx, projectID, v.MatchedID, fp, r.Signals, v, now)
				oc.Decision = DecisionAutoMatch
			case match.DecisionReview:
				id, created, err = s.create(ctx, projectID, fp, r.Signals, now)
				if err == nil && created {
					err = s.enqueueReview(ctx, projectID, runID, id, fp, v)
				}
				oc.Decision = DecisionReview
			default:
				id, created, err = s.create(ctx, projectID, fp, r.Signals, now)
				oc.Decision = DecisionNew
			}
			if !created { // fp was bound meanwhile: it is that owner's, exactly
				oc.Decision, oc.Score, oc.Gap = DecisionExact, 1, 0
			}
			if err != nil {
				return fmt.Errorf("%s: %w", r.Key, err)
			}
			oc.IdentityID = id
			decided[fp], out[r.Key] = id, id
			outcomes = append(outcomes, oc)
			// An identity created or matched here is now present in the run:
			// the next unknown fingerprint of the same batch must see it.
			present[id] = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return out, outcomes, nil
}

// keyed is a Ref with its match signals and fingerprint computed once.
type keyed struct {
	ref Ref
	ms  match.Signals
	fp  string
}

// candidates is §9.3 step 2 in SQL: same project, active, and at least one
// of same file / same leaf / trigram ≥ 0.4, ranked by a rough score and
// capped. The GIN index from N47 carries the `%` clause.
func (s *service) candidates(ctx context.Context, projectID string, in match.Signals, present map[string]bool) ([]match.Candidate, error) {
	n := in.Normalize()
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, framework, canonical_file_path, canonical_title_path, param_signature, last_seen_at
		  FROM test_identities
		 WHERE project_id = $1 AND status = 'active'
		   AND (canonical_file_path = $2 OR canonical_leaf_title = $3 OR canonical_leaf_title OPERATOR(public.%) $3)
		 ORDER BY (canonical_file_path = $2)::int + (canonical_leaf_title = $3)::int + public.similarity(canonical_leaf_title, $3) DESC
		 LIMIT $4`, projectID, n.FilePath, n.Leaf(), s.cfg.MaxCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []match.Candidate
	for rows.Next() {
		var c match.Candidate
		if err := rows.Scan(&c.ID, &c.Signals.Framework, &c.Signals.FilePath, &c.Signals.TitlePath, &c.Signals.ParamSignature, &c.LastSeenAt); err != nil {
			return nil, err
		}
		c.PresentInRun = present[c.ID]
		out = append(out, c)
	}
	return out, rows.Err()
}

// create makes a new identity owning fp, and its exact alias, in one
// statement. Two runs racing on the same unknown fingerprint (§9.7) both
// land here; the partial unique indexes let exactly one through and the
// other reads the winner's row (created = false).
func (s *service) create(ctx context.Context, projectID, fp string, sig Signals, now time.Time) (id string, created bool, err error) {
	ms := toMatch(sig).Normalize()
	var newID, bound *string
	err = s.db.Q(ctx).QueryRow(ctx, `
		WITH ins AS (
			INSERT INTO test_identities (project_id, framework, canonical_file_path, canonical_title_path, canonical_leaf_title,
			                             param_signature, template_key, current_fingerprint, first_seen_at, last_seen_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
			ON CONFLICT (project_id, current_fingerprint) WHERE status = 'active' DO NOTHING
			RETURNING id
		), al AS (
			INSERT INTO test_identity_aliases (identity_id, project_id, fingerprint, source, match_score, signals, created_by, valid_from)
			SELECT id, $1::uuid, $8::text, $10::text, NULL, $11::jsonb, 'system', $9::timestamptz FROM ins
			ON CONFLICT (project_id, fingerprint) WHERE valid_to IS NULL DO NOTHING
			RETURNING identity_id
		)
		SELECT (SELECT id::text FROM ins), (SELECT identity_id::text FROM al)`,
		projectID, ms.Framework, ms.FilePath, ms.TitlePath, ms.Leaf(), ms.ParamSignature, toMatch(sig).TemplateKey(), fp, now,
		string(SourceExact), signalsJSON(sig),
	).Scan(&newID, &bound)
	if err != nil {
		return "", false, mapErr(err)
	}
	if newID != nil && bound != nil {
		return *bound, true, nil
	}
	// Lost the race (or the alias already existed): read who owns fp now.
	hits, err := s.exact(ctx, projectID, []string{fp})
	if err != nil {
		return "", false, err
	}
	if owner, ok := hits[fp]; ok {
		return owner, false, nil
	}
	return "", false, fmt.Errorf("identity: fingerprint %s has an active identity but no alias", fp)
}

// signalsJSON is what an alias keeps of the signals it was bound with.
func signalsJSON(sig Signals) []byte {
	if !validSignals(sig) {
		return nil
	}
	b, _ := json.Marshal(sig)
	return b
}

// bind opens an alias fp → identity from `from` and reports whether it did.
// A conflict on the partial unique index means someone bound it first
// (bound = false); the caller re-reads.
func (s *service) bind(ctx context.Context, projectID, identityID, fp string, src AliasSource, score float64, sig Signals, actor string, from time.Time) (bound bool, err error) {
	sigJSON := signalsJSON(sig)
	var scoreArg *float64
	if src == SourceAutoMatch {
		scoreArg = &score
	}
	tag, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO test_identity_aliases (identity_id, project_id, fingerprint, source, match_score, signals, created_by, valid_from)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (project_id, fingerprint) WHERE valid_to IS NULL DO NOTHING`,
		identityID, projectID, fp, string(src), scoreArg, sigJSON, actor, from)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

// autoMatch aliases fp to the matched identity and moves the identity's
// canonical signals to the new name: the identity follows the rename.
// When fp turns out to be bound already — a concurrent resolve of the same
// suite committed it after the batch's exact probe — it is that owner's,
// exactly (bound = false), and nothing is renamed.
func (s *service) autoMatch(ctx context.Context, projectID, matched, fp string, sig Signals, v match.Verdict, now time.Time) (id string, bound bool, err error) {
	if bound, err = s.bind(ctx, projectID, matched, fp, SourceAutoMatch, v.Score, sig, "system", now); err != nil {
		return "", false, err
	}
	if !bound {
		hits, err := s.exact(ctx, projectID, []string{fp})
		if err != nil {
			return "", false, err
		}
		owner, ok := hits[fp]
		if !ok {
			return "", false, fmt.Errorf("identity: fingerprint %s was bound but has no alias in force", fp)
		}
		return owner, false, nil
	}
	ms := toMatch(sig).Normalize()
	_, err = s.db.Q(ctx).Exec(ctx, `
		UPDATE test_identities SET
			framework = $2, canonical_file_path = $3, canonical_title_path = $4, canonical_leaf_title = $5,
			param_signature = $6, template_key = $7, current_fingerprint = $8, last_seen_at = $9, updated_at = $9
		WHERE id = $1`,
		matched, ms.Framework, ms.FilePath, ms.TitlePath, ms.Leaf(), ms.ParamSignature, toMatch(sig).TemplateKey(), fp, now)
	return matched, true, mapErr(err)
}

func (s *service) enqueueReview(ctx context.Context, projectID, runID, identityID, fp string, v match.Verdict) error {
	breakdown, _ := json.Marshal(v.Breakdown)
	var runner *string
	var runnerScore *float64
	if v.RunnerUpID != "" {
		runner, runnerScore = &v.RunnerUpID, &v.RunnerUp
	}
	_, err := s.db.Q(ctx).Exec(ctx, `
		INSERT INTO review_queue_items (project_id, identity_id, candidate_id, fingerprint, score, runner_up_id, runner_up_score, gap, breakdown, run_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		projectID, identityID, v.MatchedID, fp, v.Score, runner, runnerScore, v.Gap, breakdown, nullable(runID))
	return mapErr(err)
}

// ---- Reads ----------------------------------------------------------------

func (s *service) ResolveAt(ctx context.Context, projectID, fingerprint string, at time.Time) (string, error) {
	if !isUUID(projectID) {
		return "", ErrNotFound
	}
	var id string
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT identity_id FROM test_identity_aliases
		 WHERE project_id = $1 AND fingerprint = $2 AND valid_from <= $3 AND (valid_to IS NULL OR valid_to > $3)
		 ORDER BY valid_from DESC LIMIT 1`, projectID, fingerprint, at.UTC()).Scan(&id)
	if err != nil {
		return "", mapErr(err)
	}
	return s.follow(ctx, id)
}

// follow walks merge chains so a retired identity resolves to its survivor.
func (s *service) follow(ctx context.Context, id string) (string, error) {
	for i := 0; i < 32; i++ {
		var status Status
		var into *string
		if err := s.db.Q(ctx).QueryRow(ctx, `SELECT status, merged_into_id FROM test_identities WHERE id = $1`, id).Scan(&status, &into); err != nil {
			return "", mapErr(err)
		}
		if status != StatusMerged || into == nil {
			return id, nil
		}
		id = *into
	}
	return "", fmt.Errorf("%w: merge chain too deep at %s", ErrConflict, id)
}

const identityColumns = `id, project_id, status, merged_into_id, framework, canonical_file_path, canonical_title_path,
	param_signature, template_key, current_fingerprint, first_seen_at, last_seen_at`

func scanIdentity(row interface{ Scan(dest ...any) error }) (Identity, error) {
	var i Identity
	var into *string
	err := row.Scan(&i.ID, &i.ProjectID, &i.Status, &into, &i.Framework, &i.CanonicalFilePath, &i.CanonicalTitlePath,
		&i.ParamSignature, &i.TemplateKey, &i.CurrentFingerprint, &i.FirstSeenAt, &i.LastSeenAt)
	if err != nil {
		return Identity{}, err
	}
	if into != nil {
		i.MergedIntoID = *into
	}
	i.FirstSeenAt, i.LastSeenAt = i.FirstSeenAt.UTC(), i.LastSeenAt.UTC()
	return i, nil
}

func (s *service) Get(ctx context.Context, id string) (Identity, error) {
	if !isUUID(id) {
		return Identity{}, ErrNotFound
	}
	i, err := scanIdentity(s.db.Q(ctx).QueryRow(ctx, `SELECT `+identityColumns+` FROM test_identities WHERE id = $1`, id))
	return i, mapErr(err)
}

func (s *service) Aliases(ctx context.Context, id string) ([]Alias, error) {
	if !isUUID(id) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, identity_id, project_id, fingerprint, source, coalesce(match_score, 0), coalesce(created_by, ''), valid_from, valid_to
		  FROM test_identity_aliases WHERE identity_id = $1 ORDER BY valid_from, created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alias
	for rows.Next() {
		var a Alias
		var to *time.Time
		if err := rows.Scan(&a.ID, &a.IdentityID, &a.ProjectID, &a.Fingerprint, &a.Source, &a.MatchScore, &a.CreatedBy, &a.ValidFrom, &to); err != nil {
			return nil, err
		}
		a.ValidFrom = a.ValidFrom.UTC()
		if to != nil {
			a.ValidTo = to.UTC()
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pg.ErrNoRows):
		return ErrNotFound
	case pg.IsUniqueViolation(err):
		return fmt.Errorf("%w: %v", ErrConflict, err)
	case pg.IsForeignKeyViolation(err):
		return ErrNotFound
	case pg.IsCheckViolation(err):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !purgeUUID.MatchString(projectID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, q := range []string{
			"DELETE FROM review_queue_items WHERE project_id = $1",
			"DELETE FROM test_identity_aliases WHERE project_id = $1",
			"DELETE FROM identity_audit WHERE project_id = $1",
			"UPDATE test_identities SET merged_into_id = NULL, status = 'retired' WHERE project_id = $1 AND merged_into_id IS NOT NULL",
			"DELETE FROM test_identities WHERE project_id = $1",
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, projectID); err != nil {
				return fmt.Errorf("identity: purge project: %w", err)
			}
		}
		return nil
	})
}

var purgeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (s *service) PurgeRun(ctx context.Context, runID string) error {
	if !purgeUUID.MatchString(runID) {
		return ErrNotFound
	}
	_, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM review_queue_items WHERE run_id = $1`, runID)
	return err
}
